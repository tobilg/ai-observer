package handlers

import (
	"net/http"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/version"
)

// ServerInfo describes the running server for GET /api/status. It holds no
// secrets: AuthEnabled says whether a token is required, never which.
type ServerInfo struct {
	// Mode is "serve", "watch" or "serve+watch"
	Mode        string
	StartedAt   time.Time
	Host        string
	APIPort     int
	OTLPPort    int
	OTLPEnabled bool
	// OTLPGRPCPort is the configured OTLP/gRPC port, 0 when it is disabled;
	// OTLPGRPCEnabled whether it listens
	OTLPGRPCPort    int
	OTLPGRPCEnabled bool
	// WatchedTools are the tools whose session files are watched, none when
	// not watching
	WatchedTools  []string
	DatabasePath  string
	DBMemoryLimit string
	DBThreads     int
	RetentionDays int
	Redaction     string
	AuthEnabled   bool
	MCPEnabled    bool
	StoreAllSpans bool
	// MaxDBSize is AI_OBSERVER_MAX_DB_SIZE in bytes (0: no limit)
	MaxDBSize int64
	// AdminAPI reports whether /api/admin/* is served; BackupDir is where it
	// writes backups and exports
	AdminAPI  bool
	BackupDir string
	// TLSEnabled reports whether the servers speak TLS
	TLSEnabled bool
	// LogLevel and LogFormat are the logging settings in effect
	LogLevel, LogFormat string
	// CORSOrigin is AI_OBSERVER_FRONTEND_URL, AllowedHosts
	// AI_OBSERVER_ALLOWED_HOSTS
	CORSOrigin   string
	AllowedHosts []string
	// SkipMigrationBackup is AI_OBSERVER_SKIP_MIGRATION_BACKUP
	SkipMigrationBackup bool
}

// SetServerInfo sets what GET /api/status reports about the server: info is
// called per request, so it can report state that changes, such as the
// mode once the file watcher has started
func (h *Handlers) SetServerInfo(info func() ServerInfo) {
	h.serverInfo = info
}

// GetStatus handles GET /api/status: version, mode, database, telemetry
// counts (the cached /api/stats) and time range, settings, pricing files and
// the import state of the tools' session files
func (h *Handlers) GetStatus(w http.ResponseWriter, r *http.Request) {
	var info ServerInfo
	if h.serverInfo != nil {
		info = h.serverInfo()
	}
	if info.Mode == "" {
		info.Mode = "serve"
	}
	ctx := r.Context()
	stats, err := h.stats.get(ctx, h.store.GetStats)
	if err != nil {
		writeQueryError(w, r, "failed to load stats", err)
		return
	}
	oldest, newest, err := h.store.TelemetryBounds(ctx)
	if err != nil {
		writeQueryError(w, r, "failed to load telemetry time range", err)
		return
	}
	imports, err := h.store.ImportSummaries(ctx)
	if err != nil {
		writeQueryError(w, r, "failed to load import state", err)
		return
	}
	size, err := h.store.DatabaseSize(ctx)
	if err != nil {
		writeQueryError(w, r, "failed to load database size", err)
		return
	}

	resp := api.StatusResponse{
		Version:      version.Version,
		GitCommit:    version.GitCommit,
		BuildDate:    version.BuildDate,
		Mode:         info.Mode,
		WatchedTools: info.WatchedTools,
		StartedAt:    info.StartedAt,
		Server: api.StatusServer{
			Host: info.Host, APIPort: info.APIPort, OTLPPort: info.OTLPPort, OTLPEnabled: info.OTLPEnabled,
			OTLPGRPCPort: info.OTLPGRPCPort, OTLPGRPCEnabled: info.OTLPGRPCEnabled,
		},
		Database: api.StatusDatabase{
			Path:         info.DatabasePath,
			SizeBytes:    diskspace.FileSize(info.DatabasePath),
			WALSizeBytes: diskspace.FileSize(info.DatabasePath + ".wal"),
			MemoryLimit:  info.DBMemoryLimit,
			Threads:      info.DBThreads,

			UsedBytes:          size.UsedBytes(),
			FreeBytes:          size.FreeBytes(),
			CompactRecommended: size.CompactionWorthwhile(),
			GrowthBytesPerDay:  storage.GrowthPerDay(size.UsedBytes(), oldest, newest),
		},
		Telemetry: api.StatusTelemetry{
			TraceCount:   stats.TraceCount,
			SpanCount:    stats.SpanCount,
			LogCount:     stats.LogCount,
			MetricCount:  stats.MetricCount,
			ServiceCount: stats.ServiceCount,
			Services:     stats.Services,
			Oldest:       timeOrNil(oldest),
			Newest:       timeOrNil(newest),
		},
		Settings: api.StatusSettings{
			RetentionDays: info.RetentionDays,
			Redaction:     info.Redaction,
			AuthEnabled:   info.AuthEnabled,
			MCPEnabled:    info.MCPEnabled,
			StoreAllSpans: info.StoreAllSpans,

			MaxDBSizeBytes:  info.MaxDBSize,
			AdminAPIEnabled: info.AdminAPI,

			TLSEnabled:          info.TLSEnabled,
			LogLevel:            info.LogLevel,
			LogFormat:           info.LogFormat,
			CORSOrigin:          info.CORSOrigin,
			AllowedHosts:        info.AllowedHosts,
			SkipMigrationBackup: info.SkipMigrationBackup,
		},
		Pricing: []api.StatusPricing{},
		Imports: []api.StatusImport{},
	}
	if info.AdminAPI {
		resp.Settings.BackupDir = info.BackupDir
	}
	if resp.Telemetry.Services == nil {
		resp.Telemetry.Services = []string{}
	}
	for _, f := range pricing.Files() {
		resp.Pricing = append(resp.Pricing, api.StatusPricing{
			File: f.File, Provider: string(f.Provider), LastUpdated: f.LastUpdated, Models: f.Models,
			Override: f.Override, OverrideLastUpdated: f.OverrideLastUpdated,
		})
	}
	for _, sum := range imports {
		resp.Imports = append(resp.Imports, api.StatusImport{
			Tool: sum.Source, Files: sum.Files, Records: sum.Records,
			LastImport: timeOrNil(sum.LastImport), PartialFiles: sum.PartialFiles,
		})
	}
	api.WriteJSON(w, http.StatusOK, resp)
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
