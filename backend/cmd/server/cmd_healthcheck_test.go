package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/config"
)

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			t.Errorf("asked %s, want /ready", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	var out bytes.Buffer
	if err := runHealthcheck(context.Background(), []string{"--url", srv.URL + "/ready"}, &out); err != nil || out.String() != "ready\n" {
		t.Fatalf("ready server: %v, %q", err, out.String())
	}
	status = http.StatusServiceUnavailable
	if err := runHealthcheck(context.Background(), []string{"--url", srv.URL + "/ready"}, &out); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("unavailable server: %v, want an error naming 503", err)
	}
	srv.Close()
	if err := runHealthcheck(context.Background(), []string{"--url", srv.URL + "/ready", "--timeout", "1s"}, &out); err == nil {
		t.Fatal("stopped server: no error")
	}
}

func TestReadyURL(t *testing.T) {
	if got := readyURL(&config.Config{APIPort: 8080}); got != "http://127.0.0.1:8080/ready" {
		t.Errorf("plain: %s", got)
	}
	if got := readyURL(&config.Config{APIPort: 9443, TLSCert: "c.pem", TLSKey: "k.pem"}); got != "https://127.0.0.1:9443/ready" {
		t.Errorf("TLS: %s", got)
	}
}
