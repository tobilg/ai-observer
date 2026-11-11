// Package restore loads the telemetry and dashboards of an export
// (ai-observer export) back into a database: the restore command
package restore

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/prompt"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Options configures a restore
type Options struct {
	// Path is an export directory or the ZIP archive of one
	Path string
	// Replace deletes the stored records of the export's services in its
	// time range before restoring, instead of refusing to store them twice
	Replace bool
	// PathMap rewrites the prefixes of the export's session file paths, for
	// a restore on another machine or with another home directory
	PathMap     []storage.PathMapping
	DryRun      bool
	SkipConfirm bool

	// Out receives the preview, prompts and results and In the
	// confirmation; nil means stdout and stdin
	Out io.Writer
	In  io.Reader
}

func (o Options) out() io.Writer {
	if o.Out == nil {
		return os.Stdout
	}
	return o.Out
}

func (o Options) in() io.Reader {
	if o.In == nil {
		return os.Stdin
	}
	return o.In
}

// ErrConflict is returned when the database holds records of the export's
// services in its range and Replace is not set
var ErrConflict = errors.New("the database already holds records of the export's services in its time range")

// exportFiles are the files of an export restore reads; others, such as the
// views database, are ignored
var exportFiles = []string{
	exporter.TracesFile, exporter.LogsFile, exporter.MetricsFile, exporter.BaselinesFile,
	exporter.ImportStateFile, exporter.DashboardsFile, exporter.ManifestFile,
}

// Run restores the export at opts.Path into store: it shows what the export
// holds, refuses (ErrConflict) or with Replace deletes the stored records it
// would duplicate, asks for confirmation and stores the export's records in
// one transaction.
func Run(ctx context.Context, store Store, opts Options) error {
	out := opts.out()
	dir, cleanup, err := openExport(opts.Path)
	if err != nil {
		return err
	}
	defer cleanup()

	files, manifest, hasManifest, err := readExport(dir)
	if err != nil {
		return err
	}
	files.PathMap = opts.PathMap
	if hasManifest && manifest.Format > exporter.ManifestFormat {
		return fmt.Errorf("the export was written by a newer AI Observer (%s, export format %d): update AI Observer to restore it",
			manifest.Version, manifest.Format)
	}

	inv, err := store.InspectRestore(ctx, files)
	if err != nil {
		return err
	}
	if inv.Empty() && len(files.Dashboards) == 0 {
		fmt.Fprintln(out, "The export holds no telemetry and no dashboards: nothing to restore.")
		return nil
	}
	conflicts, err := store.CountRestoreConflicts(ctx, inv)
	if err != nil {
		return err
	}
	hasConflicts := conflicts.LogCount+conflicts.MetricCount+conflicts.SpanCount > 0

	printPreview(out, opts, store, inv, files, manifest, hasManifest, conflicts)
	if hasConflicts && !opts.Replace {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Logs and metric points have no identity, so restoring them would store them twice.")
		fmt.Fprintln(out, "Run again with --replace to delete these records first, or restore into another")
		fmt.Fprintln(out, "database (AI_OBSERVER_DATABASE_PATH=<new file> ai-observer restore ...).")
		return ErrConflict
	}
	if opts.DryRun {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Dry run - nothing restored.")
		return nil
	}
	if !opts.SkipConfirm && !confirm(opts, hasConflicts) {
		fmt.Fprintln(out, "Aborted.")
		return nil
	}

	start := time.Now()
	result, err := store.Restore(ctx, files, inv, opts.Replace)
	if err != nil {
		return fmt.Errorf("restore failed, nothing was changed: %w", err)
	}
	// Return the blocks of replaced records for reuse (best effort)
	if err := store.Checkpoint(ctx); err != nil {
		fmt.Fprintf(out, "Warning: %v\n", err)
	}
	printResult(out, result, inv, time.Since(start))
	return nil
}

// openExport returns the directory of the export at p: p itself, or a
// temporary directory with the export files of the ZIP archive p, which
// cleanup removes
func openExport(p string) (dir string, cleanup func(), err error) {
	noop := func() {}
	fi, err := os.Stat(p)
	if err != nil {
		return "", noop, fmt.Errorf("reading export: %w", err)
	}
	if fi.IsDir() {
		return p, noop, nil
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".zip":
	case ".duckdb":
		return "", noop, fmt.Errorf("%s is a database, not an export: to restore a backup, stop AI Observer and "+
			"copy it over the database file (or point AI_OBSERVER_DATABASE_PATH at it)", p)
	default:
		return "", noop, fmt.Errorf("%s is neither an export directory nor a ZIP archive of one", p)
	}

	tmp, err := os.MkdirTemp("", "ai-observer-restore-*")
	if err != nil {
		return "", noop, fmt.Errorf("creating temporary directory: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmp) }
	if err := extractExport(p, tmp); err != nil {
		cleanup()
		return "", noop, err
	}
	return tmp, cleanup, nil
}

// extractExport extracts the export files of a ZIP archive into dir. Only
// the known file names at the archive's top level are extracted, so an
// archive cannot write elsewhere.
func extractExport(archive, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("opening %s: %w", archive, err)
	}
	defer r.Close()
	known := map[string]bool{}
	for _, name := range exportFiles {
		known[name] = true
	}
	for _, f := range r.File {
		name := path.Clean(f.Name)
		if !known[name] || f.FileInfo().IsDir() {
			continue
		}
		if err := extractFile(f, filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("extracting %s: %w", name, err)
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// readExport finds the files of the export in dir
func readExport(dir string) (files storage.RestoreFiles, m exporter.Manifest, hasManifest bool, err error) {
	present := func(name string) string {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
		return ""
	}
	files.Traces = present(exporter.TracesFile)
	files.Logs = present(exporter.LogsFile)
	files.Metrics = present(exporter.MetricsFile)
	files.Baselines = present(exporter.BaselinesFile)
	files.ImportState = present(exporter.ImportStateFile)
	if files.Traces == "" && files.Logs == "" && files.Metrics == "" {
		return files, m, false, fmt.Errorf("no export in %s: expected %s, %s and %s (written by ai-observer export)",
			dir, exporter.TracesFile, exporter.LogsFile, exporter.MetricsFile)
	}
	if files.Dashboards, _, err = exporter.ReadDashboards(dir); err != nil {
		return files, m, false, err
	}
	m, hasManifest, err = exporter.ReadManifest(dir)
	return files, m, hasManifest, err
}

func printPreview(out io.Writer, opts Options, store Store, inv *storage.RestoreInventory, files storage.RestoreFiles,
	m exporter.Manifest, hasManifest bool, conflicts *storage.DeleteSummary) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Restore Preview")
	fmt.Fprintln(out, "===============")
	fmt.Fprintf(out, "Export: %s\n", opts.Path)
	if hasManifest {
		fmt.Fprintf(out, "Written: %s by AI Observer %s (schema version %d), source %s\n",
			m.ExportedAt.Local().Format("2006-01-02 15:04"), m.Version, m.SchemaVersion, m.Source)
		if m.SchemaVersion < storage.LatestSchemaVersion() {
			fmt.Fprintf(out, "Note: the records are restored as exported; the data migrations after schema version %d are not applied to them.\n",
				m.SchemaVersion)
		}
	} else {
		fmt.Fprintln(out, "Written: by an AI Observer version without manifests (no dashboards or metric baselines)")
	}
	if !inv.Empty() {
		fmt.Fprintf(out, "Time range: %s to %s\n", formatTime(inv.From), formatTime(inv.To))
		fmt.Fprintf(out, "Services: %s\n", strings.Join(inv.Services, ", "))
	}
	for table, cols := range inv.Computed {
		fmt.Fprintf(out, "Computed for %s (missing in the export): %s\n", table, strings.Join(cols, ", "))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Records to restore:")
	fmt.Fprintf(out, "  Spans:            %d\n", inv.Rows["otel_traces"])
	fmt.Fprintf(out, "  Logs:             %d\n", inv.Rows["otel_logs"])
	fmt.Fprintf(out, "  Metric points:    %d\n", inv.Rows["otel_metrics"])
	fmt.Fprintf(out, "  Metric baselines: %d\n", inv.Rows["metric_baselines"])
	fmt.Fprintf(out, "  Dashboards:       %d\n", len(files.Dashboards))
	missing := 0
	if inv.FileState {
		fmt.Fprintf(out, "  Session files:    %d (import state)", len(inv.SessionFiles))
		if missing = missingFiles(inv.SessionFiles); missing > 0 {
			fmt.Fprintf(out, ", %d not on this machine", missing)
		}
		fmt.Fprintln(out)
	}
	if r := store.Redactor(); r.Enabled() {
		fmt.Fprintf(out, "Text is redacted again before it is stored (AI_OBSERVER_REDACT=%s).\n", r.Mode())
	}
	if missing > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Session files that are not at the export's paths are imported as new files: if they are")
		fmt.Fprintln(out, "the same sessions at another place (another machine or home directory), restore with")
		fmt.Fprintln(out, "--map-path OLD=NEW so that import and watch recognize them and store them once.")
	}

	if conflicts.LogCount+conflicts.MetricCount+conflicts.SpanCount > 0 {
		fmt.Fprintln(out)
		if opts.Replace {
			fmt.Fprintln(out, "Stored records of these services in the time range, deleted first (--replace):")
		} else {
			fmt.Fprintln(out, "The database already holds records of these services in the time range:")
		}
		fmt.Fprintf(out, "  Spans:            %d\n", conflicts.SpanCount)
		fmt.Fprintf(out, "  Logs:             %d\n", conflicts.LogCount)
		fmt.Fprintf(out, "  Metric points:    %d\n", conflicts.MetricCount)
	}
}

// missingFiles counts the paths that do not exist on this machine
func missingFiles(paths []string) int {
	missing := 0
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			missing++
		}
	}
	return missing
}

func printResult(out io.Writer, r *storage.RestoreResult, inv *storage.RestoreInventory, took time.Duration) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Restore complete (%s):\n", took.Round(time.Millisecond))
	if r.Deleted.LogCount+r.Deleted.MetricCount+r.Deleted.SpanCount > 0 {
		fmt.Fprintf(out, "  Replaced:         %d spans, %d logs, %d metric points\n",
			r.Deleted.SpanCount, r.Deleted.LogCount, r.Deleted.MetricCount)
	}
	fmt.Fprintf(out, "  Spans:            %d", r.Spans)
	if r.SpansSkipped > 0 {
		fmt.Fprintf(out, " (%d stored already, skipped)", r.SpansSkipped)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Logs:             %d\n", r.Logs)
	fmt.Fprintf(out, "  Metric points:    %d\n", r.Metrics)
	fmt.Fprintf(out, "  Metric baselines: %d\n", r.Baselines)
	fmt.Fprintf(out, "  Dashboards:       %d", r.Dashboards)
	if r.DashboardsSkipped > 0 {
		fmt.Fprintf(out, " (%d exist already, kept)", r.DashboardsSkipped)
	}
	fmt.Fprintln(out)
	if inv.FileState {
		fmt.Fprintf(out, "  Session files:    %d", r.SessionFiles)
		if r.SessionFilesKept > 0 {
			fmt.Fprintf(out, " (%d with the same state kept)", r.SessionFilesKept)
		}
		fmt.Fprintln(out)
		if r.SessionFilesReread > 0 {
			fmt.Fprintf(out, "  %d session files the database had imported with another state are read again by the\n", r.SessionFilesReread)
			fmt.Fprintln(out, "  next import or watch, which replaces their records.")
		}
	}
	if r.Redacted {
		fmt.Fprintln(out, "  Text was redacted again (AI_OBSERVER_REDACT).")
	}
	if !inv.FileState {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "The export was written by an earlier AI Observer version: its records carry no session file")
		fmt.Fprintln(out, "path, so importing the same session files again stores them twice. Use")
		fmt.Fprintln(out, "`ai-observer import <tool> --purge` to rebuild them from the files instead.")
	}
}

func confirm(opts Options, replacing bool) bool {
	out := opts.out()
	fmt.Fprintln(out)
	if replacing {
		fmt.Fprintln(out, "The replaced records are deleted; this cannot be undone.")
	}
	fmt.Fprint(out, "Continue? [y/N] ")
	return prompt.Confirmed(opts.in())
}

func formatTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05")
}
