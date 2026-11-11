package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/maintenance"
	"github.com/tobilg/ai-observer/internal/storage"
)

// lockedError explains that another process, such as a running server,
// has the database open, and how the server's admin API does the same: the
// curl command of POST endpoint with body. It returns err unchanged when
// the database is not in use.
func lockedError(err error, cfg *config.Config, endpoint string, body map[string]any, note string) error {
	if !storage.IsLocked(err) && !errors.Is(err, maintenance.ErrLocked) {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "the database %s is in use by another process, such as a running AI Observer server or watch.\n", cfg.DatabasePath)
	b.WriteString("Stop it first, or let the server do it through its admin API: start it with\n")
	b.WriteString("AI_OBSERVER_ADMIN_API=true and run\n\n")
	fmt.Fprintf(&b, "  %s\n", adminCurl(cfg, endpoint, body))
	if note != "" {
		fmt.Fprintf(&b, "\n%s", note)
	}
	return errors.New(strings.TrimRight(b.String(), "\n"))
}

// adminCurl returns the curl command that posts body to the admin API
// endpoint (such as "delete") of the server configured by cfg. The token is
// referenced, never printed.
func adminCurl(cfg *config.Config, endpoint string, body map[string]any) string {
	scheme := "http"
	if cfg.TLSCert != "" {
		scheme = "https"
	}
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("%s://%s/api/admin/%s", scheme, net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(cfg.APIPort)), endpoint)
	parts := []string{"curl", "-X", "POST", url}
	if cfg.AuthToken != "" {
		parts = append(parts, "-H", `"Authorization: Bearer $AI_OBSERVER_AUTH_TOKEN"`)
	}
	if len(body) > 0 {
		data, _ := json.Marshal(body)
		parts = append(parts, "-H", "'Content-Type: application/json'", "-d", "'"+strings.ReplaceAll(string(data), "'", `'\''`)+"'")
	}
	return strings.Join(parts, " ")
}
