package ws

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHub_Run(t *testing.T) {
	hub := NewHub()

	// Run should not panic; start it in a goroutine
	go hub.Run()

	// Give the goroutine a moment to start
	time.Sleep(50 * time.Millisecond)

	// Verify broadcast channel works without blocking
	done := make(chan struct{})
	go func() {
		hub.Broadcast([]byte("test"))
		close(done)
	}()

	select {
	case <-done:
		// Broadcast accepted, hub is running
	case <-time.After(time.Second):
		t.Fatal("hub.Broadcast blocked; hub.Run may not be running")
	}
}

func TestNewMessage(t *testing.T) {
	data := map[string]string{"key": "value"}
	raw, err := NewMessage("test_type", data)
	if err != nil {
		t.Fatalf("NewMessage returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if msg.Type != "test_type" {
		t.Errorf("expected type %q, got %q", "test_type", msg.Type)
	}

	var payload map[string]string
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}

	if payload["key"] != "value" {
		t.Errorf("expected data key=%q, got %q", "value", payload["key"])
	}
}

func TestNewMessage_NilData(t *testing.T) {
	raw, err := NewMessage("ping", nil)
	if err != nil {
		t.Fatalf("NewMessage with nil data returned error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if msg.Type != "ping" {
		t.Errorf("expected type %q, got %q", "ping", msg.Type)
	}
}
