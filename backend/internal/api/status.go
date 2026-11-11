package api

import "time"

// StatusResponse describes the running server for a status page (GET
// /api/status). It names settings, never their secret values: whether a
// token is required, not the token.
type StatusResponse struct {
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
	// Mode is what the server runs: "serve" (OTLP and API), "watch"
	// (session files and API, no OTLP) or "serve+watch" (OTLP, session files
	// and API: serve --watch)
	Mode string `json:"mode"`
	// WatchedTools lists the tools whose session files are watched
	// (claude-code, codex, gemini) in watch and serve+watch mode
	WatchedTools []string        `json:"watchedTools,omitempty"`
	StartedAt    time.Time       `json:"startedAt"`
	Server       StatusServer    `json:"server"`
	Database     StatusDatabase  `json:"database"`
	Telemetry    StatusTelemetry `json:"telemetry"`
	Settings     StatusSettings  `json:"settings"`
	Pricing      []StatusPricing `json:"pricing"`
	Imports      []StatusImport  `json:"imports"`
}

// StatusServer describes where the server listens
type StatusServer struct {
	Host    string `json:"host"`
	APIPort int    `json:"apiPort"`
	// OTLPPort is the configured OTLP port; OTLPEnabled is false in watch
	// mode, which does not listen on it
	OTLPPort    int  `json:"otlpPort"`
	OTLPEnabled bool `json:"otlpEnabled"`
	// OTLPGRPCPort is the configured OTLP/gRPC port (AI_OBSERVER_OTLP_GRPC_PORT),
	// 0 when it is turned off; OTLPGRPCEnabled reports whether it listens
	// (false in watch mode)
	OTLPGRPCPort    int  `json:"otlpGrpcPort"`
	OTLPGRPCEnabled bool `json:"otlpGrpcEnabled"`
}

// StatusDatabase describes the DuckDB database
type StatusDatabase struct {
	Path string `json:"path"`
	// SizeBytes is the size of the database file, WALSizeBytes that of its
	// write-ahead log (0 when there is none)
	SizeBytes    int64 `json:"sizeBytes"`
	WALSizeBytes int64 `json:"walSizeBytes"`
	// MemoryLimit and Threads are AI_OBSERVER_DB_MEMORY_LIMIT and
	// AI_OBSERVER_DB_THREADS, empty and 0 for DuckDB's defaults
	MemoryLimit string `json:"memoryLimit,omitempty"`
	Threads     int    `json:"threads,omitempty"`
	// UsedBytes and FreeBytes are the used and free blocks inside the file
	// (pragma_database_size). Deleted data frees blocks, which new data
	// reuses; the file only shrinks when compacted (ai-observer compact,
	// with the server stopped), which CompactRecommended suggests when a
	// large share of it is free.
	UsedBytes          int64 `json:"usedBytes"`
	FreeBytes          int64 `json:"freeBytes"`
	CompactRecommended bool  `json:"compactRecommended"`
	// GrowthBytesPerDay is the used space divided by the days between the
	// oldest and newest record, 0 while they are less than a day apart
	GrowthBytesPerDay int64 `json:"growthBytesPerDay,omitempty"`
}

// StatusTelemetry counts the stored telemetry (the cached /api/stats) and
// gives the time of its oldest and newest record
type StatusTelemetry struct {
	TraceCount   int64      `json:"traceCount"`
	SpanCount    int64      `json:"spanCount"`
	LogCount     int64      `json:"logCount"`
	MetricCount  int64      `json:"metricCount"`
	ServiceCount int        `json:"serviceCount"`
	Services     []string   `json:"services"`
	Oldest       *time.Time `json:"oldest,omitempty"`
	Newest       *time.Time `json:"newest,omitempty"`
}

// StatusSettings are the settings that change what is stored and who may
// read it
type StatusSettings struct {
	// RetentionDays is AI_OBSERVER_RETENTION_DAYS, 0 to keep everything
	RetentionDays int `json:"retentionDays"`
	// Redaction is AI_OBSERVER_REDACT: secrets, all or off
	Redaction string `json:"redaction"`
	// AuthEnabled reports whether AI_OBSERVER_AUTH_TOKEN is set
	AuthEnabled   bool `json:"authEnabled"`
	MCPEnabled    bool `json:"mcpEnabled"`
	StoreAllSpans bool `json:"storeAllSpans"`
	// MaxDBSizeBytes is AI_OBSERVER_MAX_DB_SIZE, 0 for no limit: the oldest
	// days are deleted while the used blocks exceed it
	MaxDBSizeBytes int64 `json:"maxDbSizeBytes"`
	// AdminAPIEnabled is AI_OBSERVER_ADMIN_API; BackupDir is where the
	// admin API writes backups and exports (only set when it is enabled)
	AdminAPIEnabled bool   `json:"adminApiEnabled"`
	BackupDir       string `json:"backupDir,omitempty"`
	// TLSEnabled reports whether the servers speak TLS
	// (AI_OBSERVER_TLS_CERT and AI_OBSERVER_TLS_KEY); the files are not named
	TLSEnabled bool `json:"tlsEnabled"`
	// LogLevel is AI_OBSERVER_LOG_LEVEL in effect: DEBUG, INFO, WARN or ERROR
	LogLevel string `json:"logLevel"`
	// LogFormat is AI_OBSERVER_LOG_FORMAT in effect: text or json
	LogFormat string `json:"logFormat"`
	// CORSOrigin is the browser origin the API answers CORS requests of
	// (AI_OBSERVER_FRONTEND_URL)
	CORSOrigin string `json:"corsOrigin"`
	// AllowedHosts are the further host names the API accepts
	// (AI_OBSERVER_ALLOWED_HOSTS; "*" for any)
	AllowedHosts []string `json:"allowedHosts,omitempty"`
	// SkipMigrationBackup is AI_OBSERVER_SKIP_MIGRATION_BACKUP
	SkipMigrationBackup bool `json:"skipMigrationBackup"`
}

// StatusPricing describes a pricing file in use (pricing.Files)
type StatusPricing struct {
	// File is the name of the file, such as claude.json
	File     string `json:"file"`
	Provider string `json:"provider"`
	// LastUpdated is the lastUpdated of the built-in file
	LastUpdated string `json:"lastUpdated"`
	// Models counts the models with prices, overrides included
	Models int `json:"models"`
	// Override is the path of the file from AI_OBSERVER_PRICING_DIR merged
	// over the built-in one, with its lastUpdated
	Override            string `json:"override,omitempty"`
	OverrideLastUpdated string `json:"overrideLastUpdated,omitempty"`
}

// StatusImport sums up the imported and watched session files of a tool
type StatusImport struct {
	Tool         string     `json:"tool"`
	Files        int        `json:"files"`
	Records      int64      `json:"records"`
	LastImport   *time.Time `json:"lastImport,omitempty"`
	PartialFiles int        `json:"partialFiles"`
}
