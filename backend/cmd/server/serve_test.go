package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer mimics http.Server: serve blocks until Shutdown is called and then
// returns http.ErrServerClosed.
type fakeServer struct {
	stop         chan struct{}
	shutdownDone atomic.Bool
}

func newFakeServer() *fakeServer {
	return &fakeServer{stop: make(chan struct{})}
}

func (f *fakeServer) serve() error {
	<-f.stop
	return http.ErrServerClosed
}

func (f *fakeServer) Shutdown(ctx context.Context) error {
	close(f.stop)
	// Simulate draining requests and closing storage
	time.Sleep(20 * time.Millisecond)
	f.shutdownDone.Store(true)
	return nil
}

func TestServeUntilDone_SignalShutsDownGracefully(t *testing.T) {
	srv := newFakeServer()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- serveUntilDone(ctx, srv, srv.serve) }()

	cancel() // what SIGINT/SIGTERM does via signal.NotifyContext

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveUntilDone did not return after cancellation")
	}
	if !srv.shutdownDone.Load() {
		t.Error("serveUntilDone returned before Shutdown finished")
	}
}

func TestServeUntilDone_ServeErrorIsReturnedAfterShutdown(t *testing.T) {
	srv := newFakeServer()
	startErr := errors.New("listen tcp :8080: bind: address already in use")

	err := serveUntilDone(context.Background(), srv, func() error { return startErr })

	if !errors.Is(err, startErr) {
		t.Fatalf("expected serve error, got %v", err)
	}
	if !srv.shutdownDone.Load() {
		t.Error("expected Shutdown to release resources after serve failed")
	}
}
