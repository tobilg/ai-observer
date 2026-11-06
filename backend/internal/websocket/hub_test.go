package websocket

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// MockClient creates a test client without a real WebSocket connection
func newMockClient(hub *Hub) *Client {
	return &Client{
		hub:  hub,
		conn: nil,
		send: make(chan []byte, sendBufferSize),
	}
}

func TestNewHub(t *testing.T) {
	hub := NewHub()

	if hub == nil {
		t.Fatal("NewHub returned nil")
	}

	if hub.clients == nil {
		t.Error("clients map is nil")
	}

	if hub.broadcast == nil {
		t.Error("broadcast channel is nil")
	}

	if hub.register == nil {
		t.Error("register channel is nil")
	}

	if hub.unregister == nil {
		t.Error("unregister channel is nil")
	}
}

func TestHubClientRegistration(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Create and register a client
	client := newMockClient(hub)
	hub.register <- client

	waitFor(t, func() bool { return hub.ClientCount() == 1 })
}

func TestHubClientUnregistration(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Create and register a client
	client := newMockClient(hub)
	hub.register <- client
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	// Unregister the client
	hub.unregister <- client
	waitFor(t, func() bool { return hub.ClientCount() == 0 })
}

func TestHubDoubleUnregister(t *testing.T) {
	// Tests that unregistering a client twice doesn't panic
	hub := NewHub()
	go hub.Run()

	client := newMockClient(hub)
	hub.register <- client
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	// Unregister twice - should not panic due to sync.Once in client.Close()
	hub.unregister <- client
	hub.unregister <- client // only returns once Run handled the first one

	waitFor(t, func() bool { return hub.ClientCount() == 0 })
}

func TestHubBroadcast(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Register multiple clients
	client1 := newMockClient(hub)
	client2 := newMockClient(hub)
	client3 := newMockClient(hub)

	hub.register <- client1
	hub.register <- client2
	hub.register <- client3
	waitFor(t, func() bool { return hub.ClientCount() == 3 })

	// Broadcast a message
	testMsg := Message{
		Type:    "test",
		Payload: []string{"hello", "world"},
	}
	hub.Broadcast(testMsg)

	// Check that all clients received the message
	for i, client := range []*Client{client1, client2, client3} {
		select {
		case data := <-client.send:
			var received Message
			if err := json.Unmarshal(data, &received); err != nil {
				t.Errorf("client %d: failed to unmarshal message: %v", i+1, err)
				continue
			}
			if received.Type != testMsg.Type {
				t.Errorf("client %d: expected type %s, got %s", i+1, testMsg.Type, received.Type)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("client %d: did not receive message", i+1)
		}
	}
}

func TestHubBroadcastOrder(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client := newMockClient(hub)
	hub.register <- client
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	// Send multiple messages
	messages := []Message{
		{Type: "msg1", Payload: "first"},
		{Type: "msg2", Payload: "second"},
		{Type: "msg3", Payload: "third"},
	}

	for _, msg := range messages {
		hub.Broadcast(msg)
	}

	// Verify order
	for i, expected := range messages {
		select {
		case data := <-client.send:
			var received Message
			if err := json.Unmarshal(data, &received); err != nil {
				t.Errorf("message %d: failed to unmarshal: %v", i+1, err)
				continue
			}
			if received.Type != expected.Type {
				t.Errorf("message %d: expected type %s, got %s", i+1, expected.Type, received.Type)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("message %d: not received", i+1)
		}
	}
}

func TestHubConcurrentRegistration(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Concurrently register many clients
	numClients := 100
	var wg sync.WaitGroup
	clients := make([]*Client, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			client := newMockClient(hub)
			clients[idx] = client
			hub.register <- client
		}(i)
	}

	wg.Wait()
	waitFor(t, func() bool { return hub.ClientCount() == numClients })
}

func TestHubConcurrentBroadcast(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Register a few clients
	numClients := 5
	clients := make([]*Client, numClients)
	for i := 0; i < numClients; i++ {
		clients[i] = newMockClient(hub)
		hub.register <- clients[i]
	}
	waitFor(t, func() bool { return hub.ClientCount() == numClients })

	// Concurrently broadcast messages
	numMessages := 50
	var wg sync.WaitGroup

	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hub.Broadcast(Message{Type: "concurrent", Payload: idx})
		}(i)
	}

	wg.Wait()

	// Every client receives the messages (the buffer holds all 50)
	for i, client := range clients {
		waitFor(t, func() bool { return len(client.send) == numMessages })
		if got := len(client.send); got != numMessages {
			t.Errorf("client %d received %d messages, want %d", i, got, numMessages)
		}
	}
}

func TestClientClose(t *testing.T) {
	hub := NewHub()
	client := newMockClient(hub)

	// First close should succeed
	client.Close()

	// Second close should not panic (sync.Once ensures this)
	client.Close()

	// Third close should still not panic
	client.Close()
}

func TestHubBroadcastChannelFull(t *testing.T) {
	hub := NewHub()
	// Don't start hub.Run() - broadcast channel will fill up

	// Fill the broadcast channel
	for i := 0; i < 256; i++ {
		hub.Broadcast(Message{Type: "fill", Payload: i})
	}

	// This should not block - it should drop the message
	done := make(chan bool)
	go func() {
		hub.Broadcast(Message{Type: "overflow", Payload: "dropped"})
		done <- true
	}()

	select {
	case <-done:
		// Success - didn't block
	case <-time.After(2 * time.Second):
		t.Error("Broadcast blocked when channel was full")
	}
}

func TestHubDisconnectsSlowClients(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	slow := newMockClient(hub)
	hub.register <- slow
	waitFor(t, func() bool { return hub.ClientCount() == 1 })

	// The client never reads: once its buffer is full it is dropped. Broadcast
	// drops messages when the hub is behind, so keep sending until it is.
	waitFor(t, func() bool {
		hub.Broadcast(Message{Type: "fill"})
		return hub.ClientCount() == 0
	})

	// Its send channel is closed, which ends its writePump
	for range slow.send {
	}
}
