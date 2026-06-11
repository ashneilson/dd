package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/ashneilson/dd"
	ddapi "github.com/ashneilson/dd/api"
	"github.com/ashneilson/dd/helper"
	"github.com/sirupsen/logrus"
)

// Door position constants (0-100 scale)
const (
	// CLOSE represents a fully closed door position
	CLOSE = 0
	// OPEN represents a fully open door position
	OPEN = 100
)

// Logger setup
var logger = logrus.New()

// Flags
var (
	// Multi-hub mode: a JSON file describing one or more hubs.
	flagHubsPath       = flag.String("hubs", "", "path to JSON file describing one or more hubs ({\"hubs\":[{name,host,code,password,mqtt_prefix}]})")
	flagCredentialsDir = flag.String("credentialsDir", ".", "directory used to store/load per-hub credentials in multi-hub mode")

	// Legacy single-hub mode (used when -hubs is not provided).
	flagCredentialsPath = flag.String("credentials", "dd-credentials.json", "path to credentials file (single-hub legacy mode)")
	flagHost            = flag.String("host", "", "host to connect to (single-hub legacy mode)")
	flagMqttPrefix      = flag.String("mqttPrefix", "dd-door", "prefix for mqtt (single-hub legacy mode)")

	// Shared settings.
	flagMqtt         = flag.String("mqtt", "", "mqtt server")
	flagMqttPort     = flag.Int("mqttPort", 1883, "mqtt port")
	flagMqttUser     = flag.String("mqttUser", "", "mqtt user")
	flagMqttPassword = flag.String("mqttPassword", "", "mqtt password")
	flagRemoveEntity = flag.String("removeEntity", "", "entity to remove from haus")
	flagDebug        = flag.Bool("debug", false, "debug mode")
	flagMetricsPort  = flag.Int("metricsPort", 9090, "port to expose Prometheus metrics (0 to disable)")
	flagPollInterval = flag.Duration("pollInterval", 60*time.Second, "polling interval for actual door status (0 to disable)")
)

// hubConfig is the user-provided configuration for a single hub.
type hubConfig struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Code       string `json:"code"`
	Password   string `json:"password"`
	MQTTPrefix string `json:"mqtt_prefix"`

	// credsPath, when set, points at an explicit credentials file (legacy single-hub
	// mode). When empty, credentials are loaded/registered under credentialsDir keyed
	// by the hub's base station ID. Unexported so it is never read from the JSON file.
	credsPath string
}

// hubsFile is the on-disk representation passed via -hubs.
type hubsFile struct {
	Hubs []hubConfig `json:"hubs"`
}

// hubRuntime holds the live connection and metadata for a configured hub.
type hubRuntime struct {
	cfg       hubConfig
	conn      *dd.Conn
	basicInfo ddapi.BasicInfo
}

// subPrefixes is the set of MQTT prefixes the OnConnect handler (re)subscribes to on
// every (re)connect. It is populated once all hubs have been initialized.
var (
	subPrefixesMu sync.RWMutex
	subPrefixes   []string
)

func setSubPrefixes(p []string) {
	subPrefixesMu.Lock()
	defer subPrefixesMu.Unlock()
	subPrefixes = append([]string(nil), p...)
}

func getSubPrefixes() []string {
	subPrefixesMu.RLock()
	defer subPrefixesMu.RUnlock()
	return append([]string(nil), subPrefixes...)
}

func init() {
	logger.SetOutput(os.Stdout)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})
	logger.SetLevel(logrus.InfoLevel)
}

func main() {
	flag.Parse()

	if *flagDebug {
		logger.SetLevel(logrus.DebugLevel)
	}

	hubs, err := loadHubConfigs()
	if err != nil {
		logger.WithError(err).Fatal("failed to load hub configuration")
	}

	// Optional Prometheus metrics server
	if *flagMetricsPort > 0 {
		ddapi.StartMetricsServer(*flagMetricsPort)
	}

	// MQTT connection setup (a single client shared by every hub)
	mqttClient := connectToMQTT(*flagMqtt, *flagMqttUser, *flagMqttPassword, *flagMqttPort)
	mqttHandler := ddapi.NewMQTTHandler(mqttClient, logger)

	// Wait for MQTT to be available before proceeding to init state machine (bounded)
	maxWait := 60 * time.Second
	deadline := time.Now().Add(maxWait)
	for !mqttClient.IsConnected() {
		if time.Now().After(deadline) {
			logger.Error("MQTT did not connect within 60s. Check broker address, port, and credentials (username/password). Exiting.")
			os.Exit(1)
		}
		logger.Warn("MQTT not available yet; waiting before initializing state machine...")
		time.Sleep(5 * time.Second)
	}
	logger.Info("MQTT is connected; proceeding with initialization")

	if *flagRemoveEntity != "" {
		if err := mqttHandler.RemoveEntity(*flagRemoveEntity); err != nil {
			logger.WithField("*flagRemoveEntity", *flagRemoveEntity).WithError(err).Fatal("can't remove entity")
		}
		return
	}

	// Set up each hub: ensure credentials, connect, and fetch basic info. A failure for
	// one hub is logged and skipped so a single unreachable hub does not take down the
	// others.
	var runtimes []*hubRuntime
	var prefixes []string
	for _, hc := range hubs {
		rt, err := setupHub(hc)
		if err != nil {
			logger.WithError(err).WithFields(logrus.Fields{"hub": hc.Name, "host": hc.Host}).Error("failed to set up hub; skipping")
			continue
		}
		runtimes = append(runtimes, rt)
		prefixes = append(prefixes, rt.cfg.MQTTPrefix)
	}

	if len(runtimes) == 0 {
		logger.Fatal("no hubs could be initialized; exiting")
	}

	// Publish prefixes so reconnects resubscribe to every hub, then subscribe now.
	setSubPrefixes(prefixes)
	subscribeToMQTTCommandTopics(mqttHandler, prefixes)

	// Context for background goroutines
	ctx, cancel := context.WithCancel(context.Background())

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)

	// Wait for the termination signal
	go func() {
		<-stopCh
		logger.Info("Termination signal received")
		logger.Info("Shutting down gracefully")
		// Cancel the background status loops first
		cancel()
		// Use thread-safe helper to get all devices across all hubs
		allDevices := ddapi.GetAllDeviceFSMs()
		for deviceID, fsm := range allDevices {
			logger.Infof("Shutting down device: %s", deviceID)
			if err := fsm.Trigger(context.Background(), "go_offline"); err != nil {
				logger.WithField("deviceID", deviceID).WithError(err).Error("Failed to set device to offline")
			} else {
				logger.WithField("deviceID", deviceID).Info("Device successfully set to offline")
			}
		}
		mqttClient.Disconnect(250)
		os.Exit(0)
	}()

	// Run one independent status loop per hub.
	var wg sync.WaitGroup
	for _, rt := range runtimes {
		wg.Add(1)
		go func(rt *hubRuntime) {
			defer wg.Done()
			runHub(ctx, rt, mqttHandler)
		}(rt)
	}
	wg.Wait()
}

// loadHubConfigs returns the list of hubs to manage. In multi-hub mode it reads the
// JSON file given by -hubs; otherwise it builds a single hub from the legacy flags.
func loadHubConfigs() ([]hubConfig, error) {
	if *flagHubsPath != "" {
		data, err := os.ReadFile(*flagHubsPath)
		if err != nil {
			return nil, fmt.Errorf("read hubs file %s: %w", *flagHubsPath, err)
		}
		var hf hubsFile
		if err := json.Unmarshal(data, &hf); err != nil {
			return nil, fmt.Errorf("parse hubs file %s: %w", *flagHubsPath, err)
		}
		if len(hf.Hubs) == 0 {
			return nil, fmt.Errorf("no hubs defined in %s", *flagHubsPath)
		}

		seenPrefix := map[string]bool{}
		for i := range hf.Hubs {
			h := &hf.Hubs[i]
			if h.Host == "" {
				return nil, fmt.Errorf("hub %q is missing host", h.Name)
			}
			if h.MQTTPrefix == "" {
				return nil, fmt.Errorf("hub %q (%s) is missing mqtt_prefix", h.Name, h.Host)
			}
			// The prefix forms the first level of each device's topic (prefix/<id>/...),
			// and command routing assumes the device ID is the second segment. A '/' would
			// shift that, and the MQTT wildcards '+'/'#' would break subscriptions.
			if strings.ContainsAny(h.MQTTPrefix, "/+#") {
				return nil, fmt.Errorf("invalid mqtt_prefix %q for hub %q: must not contain '/', '+' or '#'", h.MQTTPrefix, h.Name)
			}
			if seenPrefix[h.MQTTPrefix] {
				return nil, fmt.Errorf("duplicate mqtt_prefix %q; each hub must use a unique prefix", h.MQTTPrefix)
			}
			seenPrefix[h.MQTTPrefix] = true
		}
		return hf.Hubs, nil
	}

	// Legacy single-hub mode driven by individual flags.
	if *flagHost == "" {
		return nil, fmt.Errorf("no -hubs file and no -host provided")
	}
	return []hubConfig{{
		Host:       *flagHost,
		MQTTPrefix: *flagMqttPrefix,
		credsPath:  *flagCredentialsPath,
	}}, nil
}

// setupHub ensures credentials exist for the hub, opens a connection, and fetches the
// device's basic info.
func setupHub(hc hubConfig) (*hubRuntime, error) {
	var creds *ddapi.RegisterResponse

	if hc.credsPath != "" {
		// Legacy mode: load the pre-existing credentials file directly.
		c, err := helper.LoadCreds(hc.credsPath)
		if err != nil {
			return nil, fmt.Errorf("load credentials %s: %w", hc.credsPath, err)
		}
		creds = c
	} else {
		c, path, err := helper.EnsureHubCredentials(*flagCredentialsDir, hc.Host, hc.Code, hc.Password)
		if err != nil {
			return nil, err
		}
		logger.WithFields(logrus.Fields{"hub": hc.Name, "host": hc.Host, "credentials": path}).Info("Hub credentials ready")
		creds = c
	}

	conn := &dd.Conn{Host: hc.Host, Debug: *flagDebug}
	if err := conn.Connect(creds.Credential); err != nil {
		return nil, fmt.Errorf("connect to %s: %w", hc.Host, err)
	}

	basicInfo, err := ddapi.FetchBasicInfo(conn)
	if err != nil {
		return nil, fmt.Errorf("fetch basic device info from %s: %w", hc.Host, err)
	}

	// Prefer the user-supplied hub name for the Home Assistant device registry entry.
	if hc.Name != "" {
		basicInfo.Name = hc.Name
	}

	logger.WithFields(logrus.Fields{
		"hub":  hc.Name,
		"host": hc.Host,
		"bsid": basicInfo.BaseStation,
	}).Info("Connected to hub")

	return &hubRuntime{cfg: hc, conn: conn, basicInfo: *basicInfo}, nil
}

// runHub drives the status loop for a single hub until its context is cancelled or the
// underlying connection fails. Each hub gets its own cancellable context derived from
// the parent so that one hub losing its connection does not affect the others.
func runHub(parentCtx context.Context, rt *hubRuntime, mqttHandler *ddapi.MQTTHandler) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	statusCh := make(chan ddapi.DoorStatus)
	go handleStatusUpdates(ctx, cancel, rt.conn, statusCh)

	for {
		select {
		case <-ctx.Done():
			logger.WithField("hub", rt.cfg.Name).Warn("Hub status loop ended")
			return
		case status := <-statusCh:
			for _, device := range status.Devices {
				processDevice(rt, mqttHandler, device)
			}
		}
	}
}

// processDevice configures (if needed) and updates the FSM/MQTT state for a single
// device belonging to the given hub.
func processDevice(rt *hubRuntime, mqttHandler *ddapi.MQTTHandler, device ddapi.DoorStatusDevice) {
	prefix := rt.cfg.MQTTPrefix

	logger.WithFields(logrus.Fields{
		"Position": device.Device.Position,
		"hub":      rt.cfg.Name,
		"deviceID": device.ID,
	}).Info("Announcing Position")

	// Ensure thread-safe access to DeviceFSMs using helper functions
	deviceFSM, exists := ddapi.GetDeviceFSM(device.ID)
	if exists && deviceFSM.HubID != rt.basicInfo.BaseStation {
		// Another hub already owns this device ID. Refuse to touch it rather than
		// driving the wrong hub's connection. (Chosen behaviour: fail loudly instead
		// of composite-keying device identity.)
		logger.WithFields(logrus.Fields{
			"deviceID": device.ID,
			"thisHub":  rt.basicInfo.BaseStation,
			"ownerHub": deviceFSM.HubID,
		}).Error("Device ID conflicts with another hub; skipping. Give each hub devices with unique IDs, or the conflicting door cannot be controlled.")
		return
	}
	if !exists {
		deviceFSM = ddapi.ConfigureDevice(mqttHandler, rt.conn, prefix, device, rt.basicInfo)
		if deviceFSM == nil {
			// Conflict (already owned by another hub) — ConfigureDevice logged the reason.
			return
		}
		// Subscriptions are handled in the MQTT OnConnect handler
		logger.Info("Waiting on status updates...")
		if err := deviceFSM.Trigger(context.Background(), "go_online"); err != nil {
			logger.WithError(err).Error("Failed to process 'go_online' event")
		}
	} else {
		logger.WithField("deviceID", device.ID).Info("Device already configured")
	}

	position := device.Device.Position

	// Always publish the latest position (retained).
	if err := mqttHandler.PublishPosition(prefix, device.ID, position); err != nil {
		logger.WithError(err).WithField("deviceID", device.ID).Error("Failed to publish position update")
	}

	// Update Prometheus metrics
	ddapi.UpdateDevicePositionMetric(device.ID, position)

	// Drive FSM transitions when the device has reached a fully open/closed position.
	// Redundant transitions (already in that state) and invalid ones (the door is moving
	// the other way) are skipped.
	currentState := deviceFSM.Current()
	switch position {
	case OPEN:
		if currentState != "open" && currentState != "closing" {
			if err := deviceFSM.Trigger(context.Background(), "go_opened"); err != nil {
				logger.WithError(err).WithField("deviceID", device.ID).WithField("currentState", currentState).Error("Failed to process 'go_opened' event")
			}
		}
	case CLOSE:
		if currentState != "closed" && currentState != "opening" {
			if err := deviceFSM.Trigger(context.Background(), "go_closed"); err != nil {
				logger.WithError(err).WithField("deviceID", device.ID).WithField("currentState", currentState).Error("Failed to process 'go_closed' event")
			}
		}
	}

	// Self-heal: on every poll, (re)publish the current logical state (retained) so Home
	// Assistant stays in sync even when no FSM transition occurred — e.g. the door was
	// already at rest, or HA became available after missing the original state message.
	state := ddapi.CoverStateForPublish(deviceFSM.Current(), position)
	if err := mqttHandler.PublishStatus(prefix, device.ID, state); err != nil {
		logger.WithError(err).WithField("deviceID", device.ID).Error("Failed to publish state update")
	}
}

// Connect to MQTT broker
func connectToMQTT(broker, user, password string, port int) mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", broker, port))
	// Use a stable client ID for a persistent session
	opts.SetClientID("dd_haus")

	// Networking and timeouts
	opts.SetConnectTimeout(5 * time.Second)
	opts.SetWriteTimeout(5 * time.Second)

	// Make MQTT connection resilient
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	// Enable persistent session and automatic resubscription
	opts.SetCleanSession(false)
	opts.SetResumeSubs(true)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		logger.Info("Connected to MQTT broker")
		// Subscribe (or resubscribe) to every configured hub on each (re)connect
		subscribeToMQTTCommandTopics(ddapi.NewMQTTHandler(c, logger), getSubPrefixes())
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		logger.WithError(err).Warn("MQTT connection lost; will retry")
	})

	if user != "" {
		opts.SetUsername(user)
	}

	if password != "" {
		opts.SetPassword(password)
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); !token.WaitTimeout(3 * time.Second) {
		logger.Warn("Initial MQTT connect timed out; auto-reconnect will continue in background")
	} else if err := token.Error(); err != nil {
		// Detect common authentication/authorization failures and fail fast
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "not authorized") || strings.Contains(errStr, "not authorised") || strings.Contains(errStr, "bad user name or password") || strings.Contains(errStr, "unauthor") {
			logger.WithError(err).Error("MQTT authentication failed. Check username/password and broker ACLs.")
			os.Exit(1)
		}
		logger.WithError(err).Warn("Initial MQTT connect failed; will keep retrying in background")
	}

	return client
}

// subscribeToMQTTCommandTopics subscribes to the command/set_position topics for every
// configured hub prefix.
func subscribeToMQTTCommandTopics(mqttHandler *ddapi.MQTTHandler, prefixes []string) {
	// If not connected, skip subscribing; OnConnect will invoke us again
	if !mqttHandler.Client.IsConnected() {
		logger.Warn("Skipping subscribe: MQTT not connected")
		return
	}
	for _, prefix := range prefixes {
		subscribeForPrefix(mqttHandler, prefix)
	}
}

// subscribeForPrefix subscribes to the command and set_position topics for one prefix.
func subscribeForPrefix(mqttHandler *ddapi.MQTTHandler, prefix string) {
	commandTopics := fmt.Sprintf(ddapi.CommandTopicTemplate, prefix, "+")
	setPositionTopics := fmt.Sprintf(ddapi.SetPositionTopicTemplate, prefix, "+")

	// Subscribe to command topic
	token := mqttHandler.Client.Subscribe(commandTopics, 0, func(client mqtt.Client, msg mqtt.Message) {
		payload := strings.ToUpper(string(msg.Payload()))
		logger.WithField("payload", payload).WithField("topic", msg.Topic()).Info("processing mqtt command")
		handleCommand(msg.Topic(), payload)
	})
	if !token.WaitTimeout(3 * time.Second) {
		logger.WithField("topic", commandTopics).Warn("Subscribe timed out; will retry on next reconnect")
		return
	}
	if err := token.Error(); err != nil {
		logger.WithError(err).WithField("topic", commandTopics).Warn("Subscribe failed; will retry on next reconnect")
		return
	}
	logger.WithField("commandTopics", commandTopics).Info("Subscribed to command topic")

	// Subscribe to set_position topic
	token = mqttHandler.Client.Subscribe(setPositionTopics, 0, func(client mqtt.Client, msg mqtt.Message) {
		payload := string(msg.Payload())
		logger.WithField("payload", payload).WithField("topic", msg.Topic()).Info("processing mqtt set_position")
		handleSetPosition(msg.Topic(), payload)
	})
	if !token.WaitTimeout(3 * time.Second) {
		logger.WithField("topic", setPositionTopics).Warn("Subscribe timed out; will retry on next reconnect")
		return
	}
	if err := token.Error(); err != nil {
		logger.WithError(err).WithField("topic", setPositionTopics).Warn("Subscribe failed; will retry on next reconnect")
		return
	}
	logger.WithField("setPositionTopics", setPositionTopics).Info("Subscribed to set_position topic")
}

// Handle incoming MQTT messages. The device FSM (looked up by device ID) carries its own
// connection, so commands are routed to the correct hub regardless of the topic prefix.
func handleCommand(topic string, command string) {
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		logger.WithField("topic", topic).Warn("Invalid topic format")
		return
	}

	deviceID := parts[1]
	// Use thread-safe helper to access DeviceFSMs
	deviceFSM, exists := ddapi.GetDeviceFSM(deviceID)

	if !exists {
		logger.WithField("device", deviceID).Error("Device does not exist")
		return
	}

	switch command {
	case "ONLINE":
		if err := deviceFSM.Trigger(context.Background(), "go_online"); err != nil {
			logger.WithError(err).Error("Failed to process 'go_online' event")
		}
	case "OFFLINE":
		if err := deviceFSM.Trigger(context.Background(), "go_offline"); err != nil {
			logger.WithError(err).Error("Failed to process 'go_offline' event")
		}
	case "GO_OPEN":
		if err := deviceFSM.Trigger(context.Background(), "go_open"); err != nil {
			logger.WithError(err).Error("Failed to process 'open' event")
		}
	case "GO_CLOSE":
		if err := deviceFSM.Trigger(context.Background(), "go_close"); err != nil {
			logger.WithError(err).Error("Failed to process 'close' event")
		}
	case "STOP":
		if err := deviceFSM.Trigger(context.Background(), "go_stop"); err != nil {
			logger.WithError(err).Error("Failed to process 'stop' event")
		}
	default:
		logger.WithFields(logrus.Fields{
			"deviceID": deviceID,
			"command":  command}).Warn("Unknown command for device")
	}
}

// Handle set_position MQTT messages
func handleSetPosition(topic string, positionStr string) {
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		logger.WithField("topic", topic).Warn("Invalid topic format for set_position")
		return
	}

	deviceID := parts[1]
	// Use thread-safe helper to access DeviceFSMs
	deviceFSM, exists := ddapi.GetDeviceFSM(deviceID)

	if !exists {
		logger.WithField("device", deviceID).Error("Device does not exist for set_position")
		return
	}

	// Parse position
	position, err := strconv.Atoi(positionStr)
	if err != nil {
		logger.WithFields(logrus.Fields{
			"deviceID": deviceID,
			"position": positionStr,
			"error":    err,
		}).Error("Invalid position value - must be 0-100")
		return
	}

	// Validate range
	if position < 0 || position > 100 {
		logger.WithFields(logrus.Fields{
			"deviceID": deviceID,
			"position": position,
		}).Warn("Position out of range - clamping to 0-100")
	}

	logger.WithFields(logrus.Fields{
		"deviceID": deviceID,
		"position": position,
	}).Info("Setting door position")

	// Get the appropriate command for this position
	cmd := ddapi.GetCommandForPosition(position)
	ddapi.RecordCommand(deviceID, fmt.Sprintf("%d", cmd))

	// Execute the command against this device's own hub connection
	if err := ddapi.SafeCommand(deviceFSM.Conn, deviceID, cmd); err != nil {
		logger.WithFields(logrus.Fields{
			"deviceID": deviceID,
			"position": position,
			"command":  cmd,
			"error":    err,
		}).Error("Failed to execute position command")
		return
	}

	logger.WithFields(logrus.Fields{
		"deviceID": deviceID,
		"position": position,
		"command":  cmd,
	}).Info("Position command executed successfully")
}

func handleStatusUpdates(ctx context.Context, cancel context.CancelFunc, conn *dd.Conn, statusCh chan ddapi.DoorStatus) {
	// send delivers a status update unless the context is cancelled first. We never
	// close statusCh, so concurrent senders (initial fetch, poller, message loop) can
	// never panic on a closed channel; runHub observes shutdown via ctx instead.
	send := func(s ddapi.DoorStatus) bool {
		select {
		case statusCh <- s:
			return true
		case <-ctx.Done():
			return false
		}
	}

	if status, err := ddapi.SafeFetchStatus(conn); err != nil {
		logger.WithError(err).Error("Failed to fetch initial status")
		// Continue even if initial fetch fails - messages loop may recover
	} else if !send(*status) {
		return
	}

	// Periodically poll the actual door status to self-heal state desynchronization
	if *flagPollInterval > 0 {
		go func() {
			ticker := time.NewTicker(*flagPollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					logger.Debug("Polling door state periodically")
					status, err := ddapi.SafeFetchStatus(conn)
					if err != nil {
						logger.WithError(err).Error("Failed to poll door status")
						continue
					}
					if !send(*status) {
						return
					}
				}
			}
		}()
	}

	if err := helper.LoopMessages(ctx, conn, statusCh); err != nil {
		logger.WithError(err).Error("Error reading messages - connection may be lost")
	}
	// Stop this hub's loops; runHub observes the cancellation and returns.
	cancel()
}
