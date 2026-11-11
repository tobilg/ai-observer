package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/deleter"
	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

// AdminDeleteChunk is the span of telemetry the admin API deletes per
// transaction, so a large deletion does not block ingestion (like retention)
const AdminDeleteChunk = 24 * time.Hour

// Admin handles the admin API (/api/admin/*, AI_OBSERVER_ADMIN_API=true):
// maintenance of the database while the server runs. Its operations run one
// at a time. Backups and exports are written to one directory, by file
// name: requests cannot name other paths.
type Admin struct {
	store     *storage.DuckDBStore
	backupDir string
	// busy is held while an operation runs
	busy sync.Mutex
	// now returns the current time, for default file names
	now func() time.Time
}

// NewAdmin returns the admin API handlers, writing backups and exports to
// backupDir
func NewAdmin(store *storage.DuckDBStore, backupDir string) *Admin {
	return &Admin{store: store, backupDir: backupDir, now: time.Now}
}

// AdminDisabled answers requests to the admin API while it is disabled
func AdminDisabled(w http.ResponseWriter, r *http.Request) {
	api.WriteError(w, http.StatusNotFound, "the admin API is disabled: start AI Observer with AI_OBSERVER_ADMIN_API=true")
}

// exclusive runs fn unless another operation runs, which is a 409
func (a *Admin) exclusive(w http.ResponseWriter, fn func()) {
	if !a.busy.TryLock() {
		api.WriteError(w, http.StatusConflict, "another maintenance operation is running: try again when it has finished")
		return
	}
	defer a.busy.Unlock()
	fn()
}

// Delete handles POST /api/admin/delete: the delete command's deletion
// (with its handling of cumulative metric baselines), a day per
// transaction, followed by a checkpoint so new data reuses the freed blocks
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	var req api.AdminDeleteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.From.IsZero() || req.To.IsZero() {
		api.WriteError(w, http.StatusBadRequest, "from and to are required (RFC 3339)")
		return
	}
	if req.From.After(req.To) {
		api.WriteError(w, http.StatusBadRequest, "from must not be after to")
		return
	}
	scope := deleter.ScopeAll
	if req.Scope != "" {
		var err error
		if scope, err = deleter.ParseScope(req.Scope); err != nil {
			api.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	service := ""
	if req.Service != "" {
		if service = tools.ResolveFilter(req.Service); service == "" {
			api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("unknown tool or service %q", req.Service))
			return
		}
	}
	opts := deleter.Options{Scope: scope, From: req.From, To: req.To, Service: service, Out: io.Discard}
	resp := api.AdminDeleteResponse{DryRun: req.DryRun, From: req.From, To: req.To, Service: service, Scope: string(scope)}

	a.exclusive(w, func() {
		ctx := r.Context()
		if req.DryRun {
			summary, err := deleter.Preview(ctx, a.store, opts)
			if err != nil {
				writeAdminError(w, r, "counting records failed", err)
				return
			}
			resp.Logs, resp.Metrics, resp.Spans, resp.Traces = summary.LogCount, summary.MetricCount, summary.SpanCount, summary.TraceCount
			api.WriteJSON(w, http.StatusOK, resp)
			return
		}
		summary, err := deleter.ExecuteChunked(ctx, a.store, opts, AdminDeleteChunk)
		if summary != nil && summary.LogCount+summary.MetricCount+summary.SpanCount > 0 {
			logger.Info("Deleted telemetry (admin API)", "from", req.From, "to", req.To, "service", service, "scope", scope,
				"logs", summary.LogCount, "metrics", summary.MetricCount, "spans", summary.SpanCount)
		}
		if err != nil {
			writeAdminError(w, r, "deletion failed (the days before the failure were deleted)", err)
			return
		}
		resp.Logs, resp.Metrics, resp.Spans = summary.LogCount, summary.MetricCount, summary.SpanCount
		if err := a.store.Checkpoint(ctx); err != nil {
			logger.Warn("Checkpoint after deletion failed", "error", err)
		}
		if size, err := a.store.DatabaseSize(ctx); err == nil {
			resp.Database = adminSize(size)
		}
		api.WriteJSON(w, http.StatusOK, resp)
	})
}

// Checkpoint handles POST /api/admin/checkpoint: writes the write-ahead log
// into the database file and frees the blocks of deleted data
func (a *Admin) Checkpoint(w http.ResponseWriter, r *http.Request) {
	a.exclusive(w, func() {
		start := time.Now()
		if err := a.store.Checkpoint(r.Context()); err != nil {
			writeAdminError(w, r, "checkpoint failed", err)
			return
		}
		size, err := a.store.DatabaseSize(r.Context())
		if err != nil {
			writeAdminError(w, r, "reading the database size failed", err)
			return
		}
		api.WriteJSON(w, http.StatusOK, api.AdminCheckpointResponse{
			Database: *adminSize(size), DurationMs: time.Since(start).Milliseconds(),
		})
	})
}

// Backup handles POST /api/admin/backup: a consistent copy of the database
// (storage.CopyDatabase) in the backup directory, taken while telemetry is
// stored
func (a *Admin) Backup(w http.ResponseWriter, r *http.Request) {
	var req api.AdminBackupRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	name := req.Name
	if name == "" {
		name = "ai-observer-backup-" + a.now().Format("20060102-150405") + ".duckdb"
	}
	target, ok := a.target(w, name)
	if !ok {
		return
	}
	a.exclusive(w, func() {
		result, err := a.store.CopyDatabase(r.Context(), target)
		if err != nil {
			writeAdminError(w, r, "backup failed", err)
			return
		}
		logger.Info("Backed up the database (admin API)", "path", result.Path, "bytes", result.Bytes, "duration", result.Duration)
		api.WriteJSON(w, http.StatusOK, api.AdminBackupResponse{
			Path: result.Path, SizeBytes: result.Bytes, Tables: result.Tables,
			SchemaVersion: result.SchemaVersion, DurationMs: result.Duration.Milliseconds(),
		})
	})
}

// Export handles POST /api/admin/export: the export command's files in a
// new directory of the backup directory
func (a *Admin) Export(w http.ResponseWriter, r *http.Request) {
	var req api.AdminExportRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	source := exporter.SourceAll
	if req.Source != "" {
		var err error
		if source, err = exporter.ParseSourceArg(req.Source); err != nil {
			api.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.From != nil && req.To != nil && req.From.After(*req.To) {
		api.WriteError(w, http.StatusBadRequest, "from must not be after to")
		return
	}
	name := req.Name
	if name == "" {
		name = fmt.Sprintf("ai-observer-export-%s-%s", source, a.now().Format("20060102-150405"))
	}
	dir, ok := a.target(w, name)
	if !ok {
		return
	}
	a.exclusive(w, func() {
		start := time.Now()
		opts := exporter.Options{Source: source, OutputDir: dir, FromDate: req.From, ToDate: req.To, CreateZip: req.Zip, Out: io.Discard}
		summary, err := exporter.NewExporter(a.store, false).Export(r.Context(), opts)
		if err != nil {
			os.RemoveAll(dir)
			writeAdminError(w, r, "export failed", err)
			return
		}
		files := make([]string, len(summary.OutputFiles))
		for i, f := range summary.OutputFiles {
			files[i] = filepath.Base(f)
		}
		logger.Info("Exported telemetry (admin API)", "path", dir, "bytes", summary.TotalSize)
		api.WriteJSON(w, http.StatusOK, api.AdminExportResponse{
			Path: dir, Files: files, Traces: summary.TracesCount, Logs: summary.LogsCount, Metrics: summary.MetricsCount,
			Baselines: summary.BaselinesCount, Dashboards: summary.DashboardsCount,
			SizeBytes: summary.TotalSize, DurationMs: time.Since(start).Milliseconds(),
		})
	})
}

// safeName is a file or directory name the admin API writes to: no path
// separators, no leading dot, no "..", at most 128 characters
var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// target returns the path of name in the backup directory, which it
// creates; a name that is not a plain file name is a 400, an existing
// path a 409
func (a *Admin) target(w http.ResponseWriter, name string) (string, bool) {
	if !safeName.MatchString(name) || strings.Contains(name, "..") {
		api.WriteError(w, http.StatusBadRequest,
			"invalid name: use a file name of letters, digits, '.', '_' and '-' (backups and exports are written to the backup directory, AI_OBSERVER_BACKUP_DIR)")
		return "", false
	}
	if a.backupDir == "" {
		api.WriteError(w, http.StatusInternalServerError, "no backup directory is configured")
		return "", false
	}
	dir, err := filepath.Abs(a.backupDir)
	if err != nil {
		api.WriteInternalError(w, "resolving the backup directory failed", err)
		return "", false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		api.WriteInternalError(w, "creating the backup directory failed", err)
		return "", false
	}
	target := filepath.Join(dir, name)
	if _, err := os.Lstat(target); err == nil {
		api.WriteError(w, http.StatusConflict, fmt.Sprintf("%s already exists in the backup directory", name))
		return "", false
	}
	return target, true
}

func adminSize(size storage.DatabaseSize) *api.AdminDatabaseSize {
	return &api.AdminDatabaseSize{
		SizeBytes: size.FileBytes, WALSizeBytes: size.WALBytes,
		UsedBytes: size.UsedBytes(), FreeBytes: size.FreeBytes(),
		CompactRecommended: size.CompactionWorthwhile(),
	}
}

// decodeOptionalBody decodes a JSON body into v; an empty body leaves v as
// it is
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds the %d byte limit", tooLarge.Limit))
		} else {
			api.WriteError(w, http.StatusBadRequest, "invalid request body")
		}
		return false
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return true
	}
	if err := json.Unmarshal(data, v); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// writeAdminError answers a failed operation: 507 without disk space, 503
// when the client or a shutdown cancelled it (nothing was kept)
func writeAdminError(w http.ResponseWriter, r *http.Request, message string, err error) {
	var space *diskspace.Error
	switch {
	case errors.As(err, &space):
		api.WriteError(w, http.StatusInsufficientStorage, message+": "+space.Error())
	case errors.Is(err, context.Canceled):
		logger.Warn("Admin operation cancelled", "path", r.URL.Path, "error", err)
		api.WriteError(w, http.StatusServiceUnavailable, message+": cancelled (the server is shutting down or the client went away)")
	default:
		api.WriteInternalError(w, message, err)
	}
}
