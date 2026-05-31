package api

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	// DeviceStateGauge tracks the current state of each device where open=1, closed=0, etc.
	DeviceStateGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dd_device_state",
		Help: "Current logical state of the device (0=closed, 1=open, 2=opening, 3=closing, -1=offline/unknown)",
	}, []string{"device_id"})

	// DevicePositionGauge tracks the precise numerical position (0-100)
	DevicePositionGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dd_device_position_percent",
		Help: "Current physical position of the device as a percentage (0-100)",
	}, []string{"device_id"})

	// CommandCounter tracks how many commands of what type were sent
	CommandCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dd_commands_sent_total",
		Help: "Total number of commands sent to devices via MQTT",
	}, []string{"device_id", "command"})
	
	// APIRequestCounter tracks outgoing API requests to the SmartDoor servers/devices
	APIRequestCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dd_api_requests_total",
		Help: "Total number of API requests made to SmartDoor endpoints",
	}, []string{"endpoint", "status"})
)

// StartMetricsServer starts the Prometheus HTTP server on the given port
func StartMetricsServer(port int) {
	http.Handle("/metrics", promhttp.Handler())
	addr := ":" + strconv.Itoa(port)
	logrus.Infof("Starting Prometheus metrics server on %s/metrics", addr)
	
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			logrus.WithError(err).Error("Prometheus metrics server failed")
		}
	}()
}

// UpdateDeviceStateMetric is a helper to update the prometheus gauge based on FSM state
func UpdateDeviceStateMetric(deviceID string, state string) {
	val := -1.0
	switch state {
	case "closed":
		val = 0
	case "open":
		val = 1
	case "opening":
		val = 2
	case "closing":
		val = 3
	default:
		val = -1
	}
	DeviceStateGauge.WithLabelValues(deviceID).Set(val)
}

// UpdateDevicePositionMetric updates the position gauge
func UpdateDevicePositionMetric(deviceID string, position int) {
	DevicePositionGauge.WithLabelValues(deviceID).Set(float64(position))
}

// RecordCommand records a command metric
func RecordCommand(deviceID string, command string) {
	CommandCounter.WithLabelValues(deviceID, command).Inc()
}
