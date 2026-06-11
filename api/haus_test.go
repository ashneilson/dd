package api

import (
	"context"
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/ashneilson/dd"
)

// mockMQTTClient implements a dummy mqtt.Client that reports it is not connected
// to prevent panics and allow graceful logging during FSM callback execution.
type mockMQTTClient struct {
	mqtt.Client
}

func (m *mockMQTTClient) IsConnected() bool {
	return false
}

func TestFSMTransitions(t *testing.T) {
	mockClient := &mockMQTTClient{}
	handler := NewMQTTHandler(mockClient, logger)
	conn := &dd.Conn{
		Host:  "localhost",
		Debug: false,
	}

	df := NewDeviceFSM("test-device", "dd-door", conn, handler)

	if df.Current() != "initial" {
		t.Fatalf("expected initial state, got: %s", df.Current())
	}

	// 1. Transition: initial -> online
	err := df.Trigger(context.Background(), "go_online")
	if err != nil {
		t.Fatalf("failed online transition: %v", err)
	}
	if df.Current() != "online" {
		t.Fatalf("expected online state, got: %s", df.Current())
	}

	// 2. Transition: online -> closing
	err = df.Trigger(context.Background(), "go_close")
	if err != nil {
		t.Fatalf("failed close transition: %v", err)
	}
	if df.Current() != "closing" {
		t.Fatalf("expected closing state, got: %s", df.Current())
	}

	// 3. Transition: closing -> closed
	err = df.Trigger(context.Background(), "go_closed")
	if err != nil {
		t.Fatalf("failed closed transition: %v", err)
	}
	if df.Current() != "closed" {
		t.Fatalf("expected closed state, got: %s", df.Current())
	}

	// 4. Transition: closed -> closing (relaxed transition test!)
	err = df.Trigger(context.Background(), "go_close")
	if err != nil {
		t.Fatalf("failed relaxed close transition from closed state: %v", err)
	}
	if df.Current() != "closing" {
		t.Fatalf("expected closing state, got: %s", df.Current())
	}

	// 5. Transition: closing -> closed
	err = df.Trigger(context.Background(), "go_closed")
	if err != nil {
		t.Fatalf("failed closed transition: %v", err)
	}

	// 6. Transition: closed -> opening
	err = df.Trigger(context.Background(), "go_open")
	if err != nil {
		t.Fatalf("failed open transition: %v", err)
	}
	if df.Current() != "opening" {
		t.Fatalf("expected opening state, got: %s", df.Current())
	}

	// 7. Transition: opening -> open
	err = df.Trigger(context.Background(), "go_opened")
	if err != nil {
		t.Fatalf("failed opened transition: %v", err)
	}
	if df.Current() != "open" {
		t.Fatalf("expected open state, got: %s", df.Current())
	}

	// 8. Transition: open -> opening (relaxed transition test!)
	err = df.Trigger(context.Background(), "go_open")
	if err != nil {
		t.Fatalf("failed relaxed open transition from open state: %v", err)
	}
	if df.Current() != "opening" {
		t.Fatalf("expected opening state, got: %s", df.Current())
	}
}

func TestCoverStateForPublish(t *testing.T) {
	tests := []struct {
		name     string
		fsmState string
		position int
		want     string
	}{
		{"fully closed", "closed", 0, "closed"},
		{"fully open", "open", 100, "open"},
		{"closed position wins over stale fsm", "opening", 0, "closed"},
		{"open position wins over stale fsm", "closing", 100, "open"},
		{"intermediate while opening", "opening", 50, "opening"},
		{"intermediate while closing", "closing", 30, "closing"},
		{"intermediate while stopping", "stopping", 40, "stopping"},
		{"intermediate at rest is open", "open", 20, "open"},
		{"intermediate when online is open", "online", 20, "open"},
		{"intermediate when stopped is open", "stopped", 68, "open"},
		{"position below range is closed", "online", -5, "closed"},
		{"position above range is open", "online", 150, "open"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CoverStateForPublish(tt.fsmState, tt.position); got != tt.want {
				t.Errorf("CoverStateForPublish(%q, %d) = %q, want %q", tt.fsmState, tt.position, got, tt.want)
			}
		})
	}
}
