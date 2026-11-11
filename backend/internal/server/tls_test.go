package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"
)

// selfSignedCert writes a certificate for localhost and 127.0.0.1 and its
// key to dir (generated per test; no key material is committed) and returns
// their paths and a pool trusting the certificate
func selfSignedCert(t *testing.T, dir string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certFile, keyFile, pool
}

func TestLoadTLSValidatesSettings(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, _ := selfSignedCert(t, dir)
	if cfg, err := loadTLS("", ""); cfg != nil || err != nil {
		t.Errorf("no TLS: %v, %v", cfg, err)
	}
	if cfg, err := loadTLS(certFile, keyFile); cfg == nil || err != nil || len(cfg.Certificates) != 1 {
		t.Errorf("valid pair: %v, %v", cfg, err)
	}
	if _, err := loadTLS(certFile, ""); err == nil || !strings.Contains(err.Error(), "both AI_OBSERVER_TLS_CERT") {
		t.Errorf("only a certificate: %v", err)
	}
	if _, err := loadTLS("", keyFile); err == nil {
		t.Error("only a key was accepted")
	}
	if _, err := loadTLS(keyFile, certFile); err == nil || !strings.Contains(err.Error(), "loading the TLS certificate") {
		t.Errorf("swapped files: %v", err)
	}
	if _, err := loadTLS(filepath.Join(dir, "missing.pem"), keyFile); err == nil {
		t.Error("a missing file was accepted")
	}

	// The server does not start with an incomplete setting
	cfg := getTestConfig(t)
	cfg.TLSCert = certFile
	if _, err := New(cfg); err == nil {
		t.Error("New accepted a certificate without key")
	}
}

func TestServersSpeakTLS(t *testing.T) {
	certFile, keyFile, pool := selfSignedCert(t, t.TempDir())
	cfg := getTestConfig(t)
	cfg.TLSCert, cfg.TLSKey = certFile, keyFile
	cfg.AuthToken = "s3cret"
	server, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	})

	tlsConfig := &tls.Config{RootCAs: pool}
	client := &http.Client{
		Transport:     &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       5 * time.Second,
	}
	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err = client.Get("https://localhost:18080/health")
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("API over TLS: %v", err)
	}
	resp.Body.Close()
	// The API speaks HTTP/1.1 only, also over TLS
	if resp.StatusCode != http.StatusOK || resp.ProtoMajor != 1 {
		t.Errorf("API health: %d over %s", resp.StatusCode, resp.Proto)
	}

	// OTLP/HTTP negotiates HTTP/2
	req, _ := http.NewRequest(http.MethodPost, "https://localhost:14318/v1/logs", strings.NewReader(`{"resourceLogs":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("OTLP over TLS: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ProtoMajor != 2 {
		t.Errorf("OTLP export: %d over %s, want 200 over HTTP/2", resp.StatusCode, resp.Proto)
	}

	// Plain HTTP is not served on the TLS ports
	if resp, err := http.Get("http://localhost:18080/health"); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Error("the API answered plain HTTP")
		}
	}

	// The token link sets a Secure cookie over TLS
	resp, err = client.Get("https://localhost:18080/?token=s3cret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cookies := resp.Cookies()
	if resp.StatusCode != http.StatusFound || len(cookies) != 1 || !cookies[0].Secure {
		t.Errorf("token link over TLS: %d, cookies %+v", resp.StatusCode, cookies)
	}

	// WebSocket over TLS (wss) on the API server
	dialer := gorillaws.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: 5 * time.Second}
	header := http.Header{"Authorization": {"Bearer s3cret"}}
	conn, wsResp, err := dialer.Dial("wss://localhost:18080/ws", header)
	if err != nil {
		status := 0
		if wsResp != nil {
			status = wsResp.StatusCode
		}
		t.Fatalf("wss: %v (status %d)", err, status)
	}
	conn.Close()
}
