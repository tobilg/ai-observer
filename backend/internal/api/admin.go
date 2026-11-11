package api

import "time"

// The admin API (/api/admin/*, AI_OBSERVER_ADMIN_API=true) maintains the
// database while the server runs: deletion, export, backup and checkpoint.

// AdminDeleteRequest is the body of POST /api/admin/delete: the telemetry
// in [from, to] (RFC 3339, both required) of a service filter (a tool name
// such as codex selects all of its services, a service name only itself;
// empty for all) and a scope (logs, metrics, traces or all, the default).
// DryRun only counts the records.
type AdminDeleteRequest struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Service string    `json:"service,omitempty"`
	Scope   string    `json:"scope,omitempty"`
	DryRun  bool      `json:"dryRun,omitempty"`
}

// AdminDeleteResponse counts the records deleted, or those a dry run would
// delete (traces only then). Database is the space of the database after
// the deletion and a checkpoint; the file keeps its size.
type AdminDeleteResponse struct {
	DryRun   bool               `json:"dryRun"`
	From     time.Time          `json:"from"`
	To       time.Time          `json:"to"`
	Service  string             `json:"service,omitempty"`
	Scope    string             `json:"scope"`
	Logs     int64              `json:"logs"`
	Metrics  int64              `json:"metrics"`
	Spans    int64              `json:"spans"`
	Traces   int64              `json:"traces,omitempty"`
	Database *AdminDatabaseSize `json:"database,omitempty"`
}

// AdminDatabaseSize is the space the database takes: the file and its
// write-ahead log, and the used and free blocks inside the file
type AdminDatabaseSize struct {
	SizeBytes    int64 `json:"sizeBytes"`
	WALSizeBytes int64 `json:"walSizeBytes"`
	UsedBytes    int64 `json:"usedBytes"`
	FreeBytes    int64 `json:"freeBytes"`
	// CompactRecommended is set when so much of the file is free that
	// ai-observer compact (with the server stopped) would shrink it
	// noticeably
	CompactRecommended bool `json:"compactRecommended"`
}

// AdminCheckpointResponse answers POST /api/admin/checkpoint
type AdminCheckpointResponse struct {
	Database   AdminDatabaseSize `json:"database"`
	DurationMs int64             `json:"durationMs"`
}

// AdminBackupRequest is the body of POST /api/admin/backup (optional): the
// file name of the backup in the backup directory (letters, digits, ".",
// "_" and "-"; default ai-observer-backup-<time>.duckdb)
type AdminBackupRequest struct {
	Name string `json:"name,omitempty"`
}

// AdminBackupResponse describes a backup: a DuckDB database file
type AdminBackupResponse struct {
	Path          string           `json:"path"`
	SizeBytes     int64            `json:"sizeBytes"`
	Tables        map[string]int64 `json:"tables"`
	SchemaVersion int              `json:"schemaVersion"`
	DurationMs    int64            `json:"durationMs"`
}

// AdminExportRequest is the body of POST /api/admin/export (optional): the
// directory name of the export in the backup directory (letters, digits,
// ".", "_" and "-"; default ai-observer-export-<source>-<time>), the source
// (a tool name or all, the default), an optional range and whether to
// write a ZIP archive
type AdminExportRequest struct {
	Name   string     `json:"name,omitempty"`
	Source string     `json:"source,omitempty"`
	From   *time.Time `json:"from,omitempty"`
	To     *time.Time `json:"to,omitempty"`
	Zip    bool       `json:"zip,omitempty"`
}

// AdminExportResponse describes an export: its directory, the files in it
// and the rows exported
type AdminExportResponse struct {
	Path       string   `json:"path"`
	Files      []string `json:"files"`
	Traces     int64    `json:"traces"`
	Logs       int64    `json:"logs"`
	Metrics    int64    `json:"metrics"`
	Baselines  int64    `json:"baselines"`
	Dashboards int      `json:"dashboards"`
	SizeBytes  int64    `json:"sizeBytes"`
	DurationMs int64    `json:"durationMs"`
}
