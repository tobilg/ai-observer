package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
	"github.com/tobilg/ai-observer/internal/version"
)

// The files of an export. Exports of earlier versions hold only the three
// telemetry files and the views database, or (format 1) no import state
// and no SourceFile column; restore reads them too.
const (
	TracesFile      = "traces.parquet"
	LogsFile        = "logs.parquet"
	MetricsFile     = "metrics.parquet"
	BaselinesFile   = "metric_baselines.parquet"
	ImportStateFile = "import_state.parquet"
	DashboardsFile  = "dashboards.json"
	ManifestFile    = "manifest.json"
)

// ManifestFormat is the version of the export layout manifest.json
// describes. Format 2 added the session file paths of the records
// (SourceFile) and the import state of those files (import_state.parquet).
const ManifestFormat = 2

// Manifest describes an export (manifest.json)
type Manifest struct {
	// Format is ManifestFormat of the version that wrote the export
	Format int `json:"format"`
	// Version is the AI Observer version and SchemaVersion the database
	// schema version (storage.LatestSchemaVersion) the export was written by
	Version       string    `json:"version"`
	SchemaVersion int       `json:"schemaVersion"`
	ExportedAt    time.Time `json:"exportedAt"`
	// Source is the exported tool, or "all"; Services are its service names
	// (empty for all)
	Source   string   `json:"source"`
	Services []string `json:"services,omitempty"`
	// From and To are the exported range, unset for all records
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`
	// Rows counts the rows of each Parquet file and the dashboards
	Rows map[string]int64 `json:"rows"`
}

// ReadManifest reads the manifest of the export in dir; ok is false for
// exports without one (written before AI Observer had restore)
func ReadManifest(dir string) (m Manifest, ok bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if os.IsNotExist(err) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, false, fmt.Errorf("reading %s: %w", ManifestFile, err)
	}
	return m, true, nil
}

// writeManifest writes the manifest of an export with summary's counts
func (e *Exporter) writeManifest(ctx context.Context, path string, opts Options, summary *Summary) error {
	schema, err := e.store.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	m := Manifest{
		Format:        ManifestFormat,
		Version:       version.Version,
		SchemaVersion: schema,
		ExportedAt:    time.Now().UTC(),
		Source:        string(opts.Source),
		From:          opts.FromDate,
		To:            opts.ToDate,
		Rows: map[string]int64{
			TracesFile:      summary.TracesCount,
			LogsFile:        summary.LogsCount,
			MetricsFile:     summary.MetricsCount,
			BaselinesFile:   summary.BaselinesCount,
			ImportStateFile: summary.ImportStateCount,
			DashboardsFile:  int64(summary.DashboardsCount),
		},
	}
	if filter := opts.ServiceFilter(); filter != "" {
		m.Services = tools.FilterServices(filter)
	}
	return writeJSONFile(path, m)
}

// exportDashboards writes all dashboards with their widgets, in the JSON of
// GET /api/dashboards/{id}, and returns their number
func (e *Exporter) exportDashboards(ctx context.Context, path string) (int, error) {
	dashboards, err := e.store.GetDashboards(ctx)
	if err != nil {
		return 0, fmt.Errorf("reading dashboards: %w", err)
	}
	all := make([]api.DashboardWithWidgets, 0, len(dashboards))
	for _, d := range dashboards {
		widgets, err := e.store.GetWidgetsForDashboard(ctx, d.ID)
		if err != nil {
			return 0, fmt.Errorf("reading widgets of dashboard %s: %w", d.ID, err)
		}
		if widgets == nil {
			widgets = []api.DashboardWidget{}
		}
		all = append(all, api.DashboardWithWidgets{Dashboard: d, Widgets: widgets})
	}
	return len(all), writeJSONFile(path, all)
}

// ReadDashboards reads the dashboards of the export in dir; ok is false for
// exports without them
func ReadDashboards(dir string) (dashboards []api.DashboardWithWidgets, ok bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, DashboardsFile))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(data, &dashboards); err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", DashboardsFile, err)
	}
	return dashboards, true, nil
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
