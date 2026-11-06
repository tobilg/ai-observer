package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// waitFor polls cond until it holds or the timeout expires
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met before timeout")
		}
		time.Sleep(time.Millisecond)
	}
}

// dialTestServer starts a server for hub and connects a WebSocket client to it
func dialTestServer(t *testing.T, hub *Hub, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, header)
	if conn != nil {
		t.Cleanup(func() { conn.Close() })
	}
	return conn, resp, err
}

func TestQueuedMessagesAreSentAsSeparateFrames(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	conn, _, err := dialTestServer(t, hub, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	// Broadcast back to back so the messages queue up in the client's send buffer
	const count = 5
	for i := 0; i < count; i++ {
		hub.Broadcast(NewLogsMessage([]int{i}))
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i := 0; i < count; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("reading message %d: %v", i, err)
		}
		var msg struct {
			Type    MessageType `json:"type"`
			Payload []int       `json:"payload"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("message %d is not a single JSON document: %v (%q)", i, err, data)
		}
		if msg.Type != MessageTypeLogs || len(msg.Payload) != 1 || msg.Payload[0] != i {
			t.Fatalf("message %d = %+v", i, msg)
		}
	}
}

func TestHubStopClosesConnections(t *testing.T) {
	hub := NewHub()
	done := make(chan struct{})
	go func() {
		hub.Run()
		close(done)
	}()

	conn, _, err := dialTestServer(t, hub, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	hub.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Stop")
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseNoStatusReceived) {
		t.Errorf("expected a close frame, got %v", err)
	}

	// Later handshakes are closed instead of waiting for Run forever
	conn2, _, err := dialTestServer(t, hub, nil)
	if err != nil {
		t.Fatalf("dial after stop: %v", err)
	}
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn2.ReadMessage(); err == nil {
		t.Error("expected the connection after Stop to be closed")
	}
	hub.Broadcast(NewLogsMessage([]int{1})) // must not block or panic
}

func TestHubOriginCheck(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()
	hub.SetOriginCheck(func(r *http.Request) bool { return r.Header.Get("Origin") == "http://allowed.test" })

	if _, _, err := dialTestServer(t, hub, http.Header{"Origin": {"http://other.test"}}); err == nil {
		t.Error("handshake from a rejected origin succeeded")
	}
	if _, _, err := dialTestServer(t, hub, http.Header{"Origin": {"http://allowed.test"}}); err != nil {
		t.Errorf("handshake from the allowed origin failed: %v", err)
	}
}
