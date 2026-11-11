package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/tobilg/ai-observer/internal/config"
)

// cmdHealthcheck exits 0 when the server on this machine is ready, 1
// otherwise: the health check of the container image, which has no shell
// or HTTP client
func cmdHealthcheck(args []string) {
	if err := runHealthcheck(context.Background(), args, os.Stdout); err != nil {
		exitOnError(err)
	}
}

// runHealthcheck asks the server's GET /ready (the API port: serve and
// watch serve it) whether it is ready: the database answers. The URL is
// built from the settings the server reads (AI_OBSERVER_API_PORT, https
// with AI_OBSERVER_TLS_CERT), always on loopback.
func runHealthcheck(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	var url string
	timeout := fs.Duration("timeout", 5*time.Second, "How long to wait for the answer")
	fs.Var(valueFlag{&url, "url"}, "url", "Readiness URL to ask (default: /ready on the API port of this machine)")
	fs.Usage = func() {
		fmt.Print(`Check whether the server on this machine is ready: its database answers

Exits with status 0 when GET /ready on the API port answers 200, and 1
otherwise. The container image runs it as its health check.

Usage: ai-observer healthcheck [options]

Options:
`)
		printFlags(fs)
	}
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	if url == "" {
		url = readyURL(config.Load())
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", url, err)
	}
	client := &http.Client{Transport: &http.Transport{
		// The certificate names the server's public host, not loopback
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("not ready: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("not ready: %s %s", resp.Status, body)
	}
	fmt.Fprintln(out, "ready")
	return nil
}

// readyURL is the URL of GET /ready of a server with cfg on this machine
func readyURL(cfg *config.Config) string {
	scheme := "http"
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.APIPort)) + "/ready"
}
