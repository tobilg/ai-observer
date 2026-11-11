package handlers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/version"
)

func TestGetStatus(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observer.duckdb")
	if err := os.WriteFile(dbPath, make([]byte, 1234), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	h.SetServerInfo(func() ServerInfo {
		return ServerInfo{
			Mode: "watch", StartedAt: started, Host: "127.0.0.1", APIPort: 8080, OTLPPort: 4318,
			DatabasePath: dbPath, DBMemoryLimit: "1GB", RetentionDays: 30, Redaction: "secrets",
			AuthEnabled: true, MCPEnabled: true,
			TLSEnabled: true, LogLevel: "DEBUG", LogFormat: "json", CORSOrigin: "https://observer.example.com",
			AllowedHosts: []string{"observer.example.com"}, SkipMigrationBackup: true,
		}
	})

	base := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	if err := h.db().InsertLogs(ctx, []api.LogRecord{
		{Timestamp: base, ServiceName: "claude-code"}, {Timestamp: base.Add(time.Hour), ServiceName: "gemini_cli"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.db().SetImportState(ctx, &storage.ImportState{Source: "codex", FilePath: "/s.jsonl", FileHash: "h", ImportedAt: base, RecordCount: 7, Partial: true}); err != nil {
		t.Fatal(err)
	}

	var status api.StatusResponse
	if code := getJSON(t, h.GetStatus, "/api/status", nil, &status); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if status.Version != version.Version || status.Mode != "watch" || !status.StartedAt.Equal(started) {
		t.Errorf("version %q, mode %q, started %v", status.Version, status.Mode, status.StartedAt)
	}
	if status.Server != (api.StatusServer{Host: "127.0.0.1", APIPort: 8080, OTLPPort: 4318}) {
		t.Errorf("server = %+v", status.Server)
	}
	if status.Database.Path != dbPath || status.Database.SizeBytes != 1234 || status.Database.WALSizeBytes != 0 || status.Database.MemoryLimit != "1GB" {
		t.Errorf("database = %+v", status.Database)
	}
	tel := status.Telemetry
	if tel.LogCount != 2 || tel.ServiceCount != 2 || tel.Oldest == nil || !tel.Oldest.Equal(base) || tel.Newest == nil || !tel.Newest.Equal(base.Add(time.Hour)) {
		t.Errorf("telemetry = %+v", tel)
	}
	if !reflect.DeepEqual(status.Settings, api.StatusSettings{RetentionDays: 30, Redaction: "secrets", AuthEnabled: true, MCPEnabled: true,
		TLSEnabled: true, LogLevel: "DEBUG", LogFormat: "json", CORSOrigin: "https://observer.example.com",
		AllowedHosts: []string{"observer.example.com"}, SkipMigrationBackup: true}) {
		t.Errorf("settings = %+v", status.Settings)
	}
	files := map[string]api.StatusPricing{}
	for _, p := range status.Pricing {
		files[p.File] = p
	}
	for _, name := range []string{"claude.json", "codex.json", "gemini.json", "github_copilot.json"} {
		if p := files[name]; p.LastUpdated == "" || p.Models == 0 || p.Provider == "" {
			t.Errorf("pricing %s = %+v", name, p)
		}
	}
	if len(status.Imports) != 1 || status.Imports[0] != (api.StatusImport{Tool: "codex", Files: 1, Records: 7, LastImport: status.Imports[0].LastImport, PartialFiles: 1}) ||
		status.Imports[0].LastImport == nil || !status.Imports[0].LastImport.Equal(base) {
		t.Errorf("imports = %+v", status.Imports)
	}
}

// Without server information (handlers used on their own) the status still
// answers, as a server
func TestGetStatus_WithoutServerInfo(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	var status api.StatusResponse
	if code := getJSON(t, h.GetStatus, "/api/status", nil, &status); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if status.Mode != "serve" || status.Imports == nil || status.Telemetry.Services == nil || status.Telemetry.Oldest != nil {
		t.Errorf("status = %+v", status)
	}
	if strings.Contains(status.Database.Path, "token") {
		t.Errorf("database path = %q", status.Database.Path)
	}
}
