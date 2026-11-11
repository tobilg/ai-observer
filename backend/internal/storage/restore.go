package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
)

// RestoreFiles names what an export holds: its Parquet files ("" when
// absent) and its dashboards, and how to read it
type RestoreFiles struct {
	Traces    string
	Logs      string
	Metrics   string
	Baselines string
	// ImportState is the import state of the session files the records
	// were read from (exports of format 2 and later)
	ImportState string

	Dashboards []api.DashboardWithWidgets

	// PathMap rewrites the session file paths of the export (records'
	// SourceFile and the import state), for a restore on another machine
	// or with another home directory
	PathMap []PathMapping
}

// PathMapping replaces the prefix From of session file paths with To. From
// matches whole path elements: /home/a matches /home/a/x, not /home/ab.
type PathMapping struct {
	From, To string
}

// ParsePathMapping reads a mapping written OLD=NEW
func ParsePathMapping(s string) (PathMapping, error) {
	from, to, ok := strings.Cut(s, "=")
	from = strings.TrimRight(from, `/\`)
	to = strings.TrimRight(to, `/\`)
	if !ok || from == "" || to == "" {
		return PathMapping{}, fmt.Errorf("invalid path mapping %q: use OLD=NEW, such as /Users/old=/Users/new", s)
	}
	return PathMapping{From: from, To: to}, nil
}

// mapPathExpr is the SQL expression of the session file path expr with the
// first matching mapping applied
func mapPathExpr(expr string, mappings []PathMapping) string {
	if len(mappings) == 0 {
		return expr
	}
	var b strings.Builder
	b.WriteString("CASE")
	for _, m := range mappings {
		from := sqlLiteral(m.From)
		fmt.Fprintf(&b, " WHEN %[1]s = %[2]s OR starts_with(%[1]s, %[3]s) OR starts_with(%[1]s, %[4]s) THEN %[5]s || substr(%[1]s, %[6]d)",
			expr, from, sqlLiteral(m.From+"/"), sqlLiteral(m.From+`\`), sqlLiteral(m.To), len([]rune(m.From))+1)
	}
	b.WriteString(" ELSE " + expr + " END")
	return b.String()
}

// restoreTables are the telemetry tables of an export, by file, with the
// columns ingestion redacts
func (f RestoreFiles) restoreTables() []restoreTable {
	return []restoreTable{
		{table: "otel_traces", file: f.Traces, attrs: "SpanAttributes",
			text:       []string{"StatusMessage"},
			attributes: []string{"SpanAttributes", "Events.Attributes", "Links.Attributes"},
			shared:     []string{"ResourceAttributes"}},
		{table: "otel_logs", file: f.Logs, attrs: "LogAttributes",
			text:       []string{"Body"},
			attributes: []string{"LogAttributes"},
			shared:     []string{"ResourceAttributes", "ScopeAttributes"}},
		{table: "otel_metrics", file: f.Metrics,
			attributes: []string{"Attributes"},
			shared:     []string{"ResourceAttributes"}},
	}
}

// restoreDerived are the columns of derived data a restore computes after
// the insert instead of taking them from the export: they depend on the
// stored (redacted) text and on the code of the running version. Exports of
// earlier versions lack them anyway.
var restoreDerived = map[string][]string{
	// fillMessageKeysTx
	"otel_logs": {"MessageKey"},
}

type restoreTable struct {
	table, file string
	// attrs is the attribute column session IDs are read from, "" for
	// tables without session ID columns
	attrs string
	// text, attributes and shared are the columns ingestion redacts: text,
	// attribute maps of which the values are redacted, and attribute maps
	// many records share (resource and scope attributes)
	text, attributes, shared []string
}

// RestoreInventory describes the records of an export
type RestoreInventory struct {
	// Rows counts the rows of each file by table (otel_traces, otel_logs,
	// otel_metrics, metric_baselines)
	Rows map[string]int64
	// From and To are the times of the oldest and newest record, zero when
	// the export holds none; Services are the services of its records
	From     time.Time
	To       time.Time
	Services []string
	// Computed lists, by table, the columns the export lacks that are
	// computed from the others (session IDs of exports from versions
	// before they were stored)
	Computed map[string][]string
	// SessionFiles are the session files (paths mapped) of the export's
	// import state; FileState reports whether the export has one (format
	// 2), without which its records carry no session file paths
	SessionFiles []string
	FileState    bool
}

// Empty reports whether the export holds no telemetry
func (inv *RestoreInventory) Empty() bool {
	return inv.Rows["otel_traces"]+inv.Rows["otel_logs"]+inv.Rows["otel_metrics"] == 0
}

// InspectRestore reads the size, time range and services of the records of
// an export, and checks that its files have the columns the tables need
func (s *DuckDBStore) InspectRestore(ctx context.Context, files RestoreFiles) (*RestoreInventory, error) {
	inv := &RestoreInventory{Rows: map[string]int64{}, Computed: map[string][]string{}}
	services := map[string]bool{}
	var from, to sql.NullTime
	for _, t := range files.restoreTables() {
		if t.file == "" {
			continue
		}
		if _, computed, err := s.restoreSelect(ctx, t, restoreOptions{}); err != nil {
			return nil, err
		} else if len(computed) > 0 {
			inv.Computed[t.table] = computed
		}
		var n int64
		var minTime, maxTime sql.NullTime
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*), MIN(Timestamp), MAX(Timestamp) FROM read_parquet(?)", t.file).
			Scan(&n, &minTime, &maxTime); err != nil {
			return nil, fmt.Errorf("reading %s: %w", t.file, err)
		}
		inv.Rows[t.table] = n
		if minTime.Valid && (!from.Valid || minTime.Time.Before(from.Time)) {
			from = minTime
		}
		if maxTime.Valid && (!to.Valid || maxTime.Time.After(to.Time)) {
			to = maxTime
		}
		names, err := s.distinctServices(ctx, t.file)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			services[name] = true
		}
	}
	if files.Baselines != "" {
		var n int64
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM read_parquet(?)", files.Baselines).Scan(&n); err != nil {
			return nil, fmt.Errorf("reading %s: %w", files.Baselines, err)
		}
		inv.Rows["metric_baselines"] = n
	}
	if files.ImportState != "" {
		paths, err := s.restoreSessionFiles(ctx, files)
		if err != nil {
			return nil, err
		}
		inv.FileState = true
		inv.SessionFiles = paths
		inv.Rows["import_state"] = int64(len(paths))
	}
	inv.From, inv.To = from.Time.UTC(), to.Time.UTC()
	for name := range services {
		inv.Services = append(inv.Services, name)
	}
	slices.Sort(inv.Services)
	return inv, nil
}

// restoreSessionFiles returns the distinct session file paths (mapped) of
// an export's import state
func (s *DuckDBStore) restoreSessionFiles(ctx context.Context, files RestoreFiles) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT "+mapPathExpr("file_path", files.PathMap)+
		" AS path FROM read_parquet(?) ORDER BY path", files.ImportState)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", files.ImportState, err)
	}
	defer rows.Close()
	paths := []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func (s *DuckDBStore) distinctServices(ctx context.Context, file string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT ServiceName FROM read_parquet(?) ORDER BY 1", file)
	if err != nil {
		return nil, fmt.Errorf("reading services of %s: %w", file, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// CountRestoreConflicts counts the stored records of services in [from, to]:
// restoring an export of that range would store its logs and metric points
// a second time (spans are skipped when stored)
func (s *DuckDBStore) CountRestoreConflicts(ctx context.Context, inv *RestoreInventory) (*DeleteSummary, error) {
	total := &DeleteSummary{}
	if inv.Empty() {
		return total, nil
	}
	for _, service := range inv.Services {
		counts, err := s.CountAllInRange(ctx, inv.From, inv.To, service)
		if err != nil {
			return nil, err
		}
		total.LogCount += counts.LogCount
		total.MetricCount += counts.MetricCount
		total.TraceCount += counts.TraceCount
		total.SpanCount += counts.SpanCount
	}
	return total, nil
}

// RestoreResult counts what a restore deleted and stored
type RestoreResult struct {
	// Deleted counts the records replaced (RestoreOptions.Replace)
	Deleted DeleteSummary
	// Logs, Metrics and Spans count the records stored; SpansSkipped the
	// spans that were stored already
	Logs         int64
	Metrics      int64
	Spans        int64
	SpansSkipped int64
	Baselines    int64
	// Dashboards counts the dashboards restored; DashboardsSkipped those
	// that exist (by ID) and were kept
	Dashboards        int
	DashboardsSkipped int
	// SessionFiles counts the import states of session files restored,
	// SessionFilesKept those the database held already, equal, and
	// SessionFilesReread those it held with another state: these files
	// are read again by the next import or watch (restoreImportState)
	SessionFiles       int64
	SessionFilesKept   int64
	SessionFilesReread int64
	// Redacted reports whether the restored text was redacted
	Redacted bool
}

// Restore stores the records of an export in one transaction. With
// replace, the stored records of the export's services in its time range
// are deleted first (as by the delete command, keeping the baselines of
// cumulative series); without it, the caller has checked there are none
// (CountRestoreConflicts). Records are stored as exported, with their
// session file paths (mapped by files.PathMap; none in exports before
// format 2) and the session ID columns computed for exports that lack
// them. Their text is redacted with the store's redactor, as ingestion
// does, after the session IDs are read. The import state of the session
// files is merged (restoreImportState). Spans that are stored already are
// skipped. Dashboards are restored unless one with the same ID exists; a
// restored default dashboard stays the default only when the database has
// none.
func (s *DuckDBStore) Restore(ctx context.Context, files RestoreFiles, inv *RestoreInventory, replace bool) (*RestoreResult, error) {
	result := &RestoreResult{}
	err := s.inWriteTx(ctx, func(w *writeTx) error {
		*result = RestoreResult{}
		opts := restoreOptions{redact: w.redact.Enabled(), paths: files.PathMap}
		if opts.redact {
			if err := registerRedactFunctions(w, w.redact); err != nil {
				return err
			}
			result.Redacted = true
		}
		// The deletion below keeps the last deleted point of each cumulative
		// series as its baseline, replacing an earlier one; remember those
		// for the points that are restored (mergeRestoredBaselines)
		if _, err := w.tx.ExecContext(ctx, `CREATE OR REPLACE TEMP TABLE restore_baselines_before AS
			SELECT * FROM metric_baselines`); err != nil {
			return fmt.Errorf("reading metric baselines: %w", err)
		}
		if replace && !inv.Empty() {
			for _, service := range inv.Services {
				deleted, err := deleteAllInRange(ctx, w, inv.From, inv.To, service)
				if err != nil {
					return err
				}
				result.Deleted.LogCount += deleted.LogCount
				result.Deleted.MetricCount += deleted.MetricCount
				result.Deleted.SpanCount += deleted.SpanCount
			}
		}
		for _, t := range files.restoreTables() {
			if t.file == "" {
				continue
			}
			n, err := s.restoreTable(ctx, w, t, opts)
			if err != nil {
				return err
			}
			switch t.table {
			case "otel_traces":
				result.Spans = n
				result.SpansSkipped = inv.Rows[t.table] - n
			case "otel_logs":
				result.Logs = n
				// The restored logs' message keys, from their stored text
				if err := fillMessageKeysTx(ctx, w); err != nil {
					return err
				}
			case "otel_metrics":
				result.Metrics = n
			}
		}
		if files.Metrics != "" {
			if _, err := w.tx.ExecContext(ctx, `INSERT OR IGNORE INTO metric_names
				SELECT DISTINCT ServiceName, MetricName FROM read_parquet(?)`, files.Metrics); err != nil {
				return fmt.Errorf("recording metric names: %w", err)
			}
		}
		n, err := mergeRestoredBaselines(ctx, w, files.Baselines, opts.redact)
		if err != nil {
			return err
		}
		result.Baselines = n
		if files.ImportState != "" {
			if err := s.restoreImportState(ctx, w, files.ImportState, files.PathMap, result); err != nil {
				return err
			}
		}
		return restoreDashboards(ctx, w, files.Dashboards, result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// restoreOptions are how restored rows are written: with their text
// redacted (the redaction functions are registered), and their session
// file paths mapped
type restoreOptions struct {
	redact bool
	paths  []PathMapping
}

// restoreTable inserts the rows of a table's file and returns their number
func (s *DuckDBStore) restoreTable(ctx context.Context, w *writeTx, t restoreTable, opts restoreOptions) (int64, error) {
	query, _, err := s.restoreSelect(ctx, t, opts)
	if err != nil {
		return 0, err
	}
	res, err := w.tx.ExecContext(ctx, query, t.file)
	if err != nil {
		return 0, fmt.Errorf("restoring %s: %w", t.table, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("restoring %s: %w", t.table, err)
	}
	return n, nil
}

// restoreSelect returns the INSERT of the rows of a table's file (with the
// file as its parameter), and the columns it computes because the file
// lacks them. The file's columns are matched to the table's by name;
// columns of the table the file lacks take their default, or are NULL (or
// computed, for the session IDs), and the file's other columns are
// ignored. SourceFile is mapped with opts.paths (NULL in exports before
// format 2); text is redacted with opts.redact, after the session IDs are
// computed from the file's attributes.
func (s *DuckDBStore) restoreSelect(ctx context.Context, t restoreTable, opts restoreOptions) (query string, computed []string, err error) {
	tableColumns, err := s.tableColumns(ctx, t.table)
	if err != nil {
		return "", nil, err
	}
	fileColumns, err := s.parquetColumns(ctx, t.file)
	if err != nil {
		return "", nil, err
	}

	var names, values []string
	sessionIDs := false
	for _, col := range tableColumns {
		names = append(names, quoteIdent(col.name))
		switch {
		case slices.Contains(restoreDerived[t.table], col.name):
			values = append(values, "NULL")
		case col.name == "SourceFile" && fileColumns[col.name]:
			values = append(values, mapPathExpr("p.SourceFile", opts.paths))
		case fileColumns[col.name]:
			value := "p." + quoteIdent(col.name)
			if opts.redact {
				value = t.restoreRedaction(col.name, value)
			}
			values = append(values, value)
		case t.attrs != "" && (col.name == "SessionId" || col.name == "SessionIds") && fileColumns[t.attrs]:
			sessionIDs = true
			computed = append(computed, col.name)
			if col.name == "SessionId" {
				values = append(values, "p.__session_ids[1]")
			} else {
				values = append(values, "CASE WHEN len(p.__session_ids) = 0 THEN NULL ELSE p.__session_ids END")
			}
		case col.def.Valid:
			values = append(values, col.def.String)
		case col.nullable:
			values = append(values, "NULL")
		default:
			return "", nil, fmt.Errorf("%s lacks the column %s of %s", t.file, col.name, t.table)
		}
	}

	source := "SELECT * FROM read_parquet(?)"
	if sessionIDs {
		source = "SELECT *, " + sessionIDsExpr(t.attrs) + " AS __session_ids FROM read_parquet(?)"
	}
	query = "INSERT INTO " + t.table + " (" + strings.Join(names, ", ") + ")\n" +
		"SELECT " + strings.Join(values, ", ") + " FROM (" + source + ") p"
	if t.table == "otel_traces" {
		// Spans are stored once (insertNewSpansTx)
		query += ` WHERE NOT EXISTS (SELECT 1 FROM otel_traces t
			WHERE t.ServiceName = p.ServiceName AND t.TraceId = p.TraceId AND t.SpanId = p.SpanId)`
	}
	return query, computed, nil
}

// sessionIDsExpr computes the session IDs of a row from its attributes like
// sessionIDColumns does at insert: the values of tools.SessionIDKeys in
// their order, without empty ones and repetitions
func sessionIDsExpr(attrs string) string {
	values := make([]string, len(tools.SessionIDKeys))
	for i, key := range tools.SessionIDKeys {
		values[i] = fmt.Sprintf(`NULLIF(json_extract_string(%s, '$."%s"'), '')`, attrs, strings.ReplaceAll(key, "'", "''"))
	}
	list := fmt.Sprintf("(CASE WHEN json_valid(%s) THEN [%s] ELSE []::VARCHAR[] END)", attrs, strings.Join(values, ", "))
	return fmt.Sprintf("list_filter(%[1]s, lambda x, i: x IS NOT NULL AND list_position(%[1]s, x) = i)", list)
}

type tableColumn struct {
	name     string
	nullable bool
	// def is the column's default expression, if it has one
	def sql.NullString
}

// tableColumns returns the columns of a table in their order
func (s *DuckDBStore) tableColumns(ctx context.Context, table string) ([]tableColumn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT column_name, is_nullable, column_default FROM duckdb_columns()
		WHERE database_name = current_database() AND schema_name = 'main' AND table_name = ?
		ORDER BY column_index`, table)
	if err != nil {
		return nil, fmt.Errorf("reading columns of %s: %w", table, err)
	}
	defer rows.Close()
	var cols []tableColumn
	for rows.Next() {
		var c tableColumn
		if err := rows.Scan(&c.name, &c.nullable, &c.def); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// parquetColumns returns the column names of a Parquet file
func (s *DuckDBStore) parquetColumns(ctx context.Context, file string) (map[string]bool, error) {
	return readParquetColumns(ctx, s.db, file)
}

// parquetColumnsTx is parquetColumns within a write transaction
func parquetColumnsTx(ctx context.Context, w *writeTx, file string) (map[string]bool, error) {
	return readParquetColumns(ctx, w.tx, file)
}

func readParquetColumns(ctx context.Context, q queryer, file string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, "DESCRIBE SELECT * FROM read_parquet(?)", file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if name, ok := values[0].(string); ok {
			names[name] = true
		}
	}
	return names, rows.Err()
}

// mergeRestoredBaselines settles the metric baselines after the records
// are stored. With redact, the attributes of the export's baselines are
// redacted like those of the restored points, so they name the same series. A series has one baseline, its previous point before the
// stored ones. Baselines at the time of a restored point are dropped: a
// replacing restore deleted the point first, which made it the baseline
// and would tie with the restored point. The baselines from before the
// restore come back where no later one remains, and the export's baselines
// (file, "" for none) are added, a series keeping the later one. It
// returns the number of the export's baselines stored.
func mergeRestoredBaselines(ctx context.Context, w *writeTx, file string, redact bool) (int64, error) {
	if err := dropRestoredBaselines(ctx, w); err != nil {
		return 0, err
	}
	if _, err := w.tx.ExecContext(ctx, `
		INSERT INTO metric_baselines (`+baselineColumns+`) SELECT `+baselineColumns+` FROM restore_baselines_before
		ON CONFLICT DO UPDATE SET `+baselineUpdate+`
			WHERE excluded.Timestamp > metric_baselines.Timestamp`); err != nil {
		return 0, fmt.Errorf("keeping metric baselines: %w", err)
	}
	var n int64
	if file != "" {
		// Exports of versions before migration 12 have no histogram columns
		cols, err := parquetColumnsTx(ctx, w, file)
		if err != nil {
			return 0, err
		}
		histogram := "Count, BucketCounts, ExplicitBounds"
		if !cols["Count"] || !cols["BucketCounts"] || !cols["ExplicitBounds"] {
			histogram = "NULL::UBIGINT, NULL::JSON, NULL::JSON"
		}
		// Redaction can give two series the same attributes, which then
		// are one series: it keeps the later baseline
		res, err := w.tx.ExecContext(ctx, `
			INSERT INTO metric_baselines (`+baselineColumns+`)
			SELECT ServiceName, MetricName, Attributes, ResourceAttributes,
				MAX(Timestamp), arg_max(Value, Timestamp), arg_max_null(Count, Timestamp),
				arg_max_null(BucketCounts, Timestamp), arg_max_null(ExplicitBounds, Timestamp)
			FROM (
				SELECT ServiceName, MetricName, `+redactRestoredBaselines(redact)+` , Timestamp, Value, `+histogram+`
				FROM read_parquet(?)
			) b(ServiceName, MetricName, Attributes, ResourceAttributes, Timestamp, Value, Count, BucketCounts, ExplicitBounds)
			GROUP BY ServiceName, MetricName, Attributes, ResourceAttributes
			ON CONFLICT DO UPDATE SET `+baselineUpdate+`
				WHERE excluded.Timestamp > metric_baselines.Timestamp`, file)
		if err != nil {
			return 0, fmt.Errorf("restoring metric baselines: %w", err)
		}
		if n, err = res.RowsAffected(); err != nil {
			return 0, fmt.Errorf("restoring metric baselines: %w", err)
		}
	}
	if err := dropRestoredBaselines(ctx, w); err != nil {
		return 0, err
	}
	if _, err := w.tx.ExecContext(ctx, "DROP TABLE restore_baselines_before"); err != nil {
		return 0, fmt.Errorf("dropping baselines copy: %w", err)
	}
	return n, nil
}

// dropRestoredBaselines removes the baselines at the time of a stored point
// of their series
func dropRestoredBaselines(ctx context.Context, w *writeTx) error {
	_, err := w.tx.ExecContext(ctx, `
		DELETE FROM metric_baselines b WHERE EXISTS (
			SELECT 1 FROM otel_metrics m
			WHERE m.ServiceName = b.ServiceName AND m.MetricName = b.MetricName
				AND m.Timestamp = b.Timestamp AND m.AggregationTemporality = 2
				AND CAST(m.Attributes AS VARCHAR) = b.Attributes
				AND CAST(m.ResourceAttributes AS VARCHAR) = b.ResourceAttributes)`)
	if err != nil {
		return fmt.Errorf("dropping baselines of restored points: %w", err)
	}
	return nil
}

// restoreDashboards stores the dashboards that do not exist by ID
func restoreDashboards(ctx context.Context, w *writeTx, dashboards []api.DashboardWithWidgets, result *RestoreResult) error {
	if len(dashboards) == 0 {
		return nil
	}
	var hasDefault bool
	if err := w.tx.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM dashboards WHERE is_default").Scan(&hasDefault); err != nil {
		return fmt.Errorf("reading the default dashboard: %w", err)
	}
	for _, d := range dashboards {
		exists, err := dashboardExists(ctx, w.tx, d.ID)
		if err != nil {
			return err
		}
		if exists || d.ID == "" {
			result.DashboardsSkipped++
			continue
		}
		isDefault := d.IsDefault && !hasDefault
		hasDefault = hasDefault || isDefault
		if _, err := w.tx.ExecContext(ctx, `
			INSERT INTO dashboards (id, name, description, is_default, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			d.ID, d.Name, d.Description, isDefault, d.CreatedAt, d.UpdatedAt); err != nil {
			return fmt.Errorf("restoring dashboard %s: %w", d.ID, err)
		}
		for _, widget := range d.Widgets {
			config, err := json.Marshal(widget.Config)
			if err != nil {
				return fmt.Errorf("restoring widget %s: %w", widget.ID, err)
			}
			if _, err := w.tx.ExecContext(ctx, `
				INSERT INTO dashboard_widgets (id, dashboard_id, widget_type, title, grid_column, grid_row,
					col_span, row_span, config, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT DO NOTHING`,
				widget.ID, d.ID, widget.WidgetType, widget.Title, widget.GridColumn, widget.GridRow,
				widget.ColSpan, widget.RowSpan, string(config), widget.CreatedAt, widget.UpdatedAt); err != nil {
				return fmt.Errorf("restoring widget %s: %w", widget.ID, err)
			}
		}
		result.Dashboards++
	}
	return nil
}

// restoreImportState merges the import state of the export's session files
// (file, paths mapped) into the database's. A file the database has no
// state of gets the export's, so importing or watching it continues where
// the export's database stopped: an unchanged file is skipped, a grown or
// changed one replaces its restored records (they carry its path), as for
// any file imported before. A file of which the database holds the same
// state keeps it. A file of which it holds another state (it imported the
// file too, or a replaced range held its records) is marked to be read
// again: partial, with its watch offsets at 0, so the next import or watch
// reads the whole file and replaces all of its records, whichever side
// they came from. Records of earlier versions without a file path
// (source_tracked false on either side) are replaced too.
func (s *DuckDBStore) restoreImportState(ctx context.Context, w *writeTx, file string, paths []PathMapping, result *RestoreResult) error {
	tableColumns, err := s.tableColumns(ctx, "import_state")
	if err != nil {
		return err
	}
	fileColumns, err := s.parquetColumns(ctx, file)
	if err != nil {
		return err
	}
	var names, values, compared []string
	for _, col := range tableColumns {
		name := quoteIdent(col.name)
		names = append(names, name)
		switch {
		case col.name == "file_path" && fileColumns[col.name]:
			values = append(values, mapPathExpr("p.file_path", paths)+" AS file_path")
		case fileColumns[col.name]:
			values = append(values, "p."+name)
		case col.def.Valid:
			values = append(values, col.def.String+" AS "+name)
		case col.nullable:
			values = append(values, "NULL AS "+name)
		default:
			return fmt.Errorf("%s lacks the column %s of import_state", file, col.name)
		}
		switch col.name {
		case "source", "file_path", "imported_at", "record_count":
		default:
			compared = append(compared, "s."+name+" IS DISTINCT FROM r."+name)
		}
	}
	// One state per file, also when mappings join paths
	if _, err := w.tx.ExecContext(ctx, `CREATE OR REPLACE TEMP TABLE restore_import_state AS
		SELECT * FROM (SELECT `+strings.Join(values, ", ")+` FROM read_parquet(?) p)
		QUALIFY row_number() OVER (PARTITION BY source, file_path ORDER BY imported_at DESC) = 1`, file); err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	var matched int64
	if err := w.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM restore_import_state r
		JOIN import_state s ON s.source = r.source AND s.file_path = r.file_path`).Scan(&matched); err != nil {
		return fmt.Errorf("comparing import state: %w", err)
	}
	res, err := w.tx.ExecContext(ctx, `
		UPDATE import_state s SET partial = true, byte_offset = 0, message_count = 0, parser_state = NULL,
			source_tracked = COALESCE(s.source_tracked, false) AND COALESCE(r.source_tracked, false)
		FROM restore_import_state r
		WHERE s.source = r.source AND s.file_path = r.file_path AND (`+strings.Join(compared, " OR ")+`)`)
	if err != nil {
		return fmt.Errorf("marking session files to read again: %w", err)
	}
	if result.SessionFilesReread, err = res.RowsAffected(); err != nil {
		return err
	}
	result.SessionFilesKept = matched - result.SessionFilesReread

	res, err = w.tx.ExecContext(ctx, `
		INSERT INTO import_state (`+strings.Join(names, ", ")+`)
		SELECT `+strings.Join(names, ", ")+` FROM restore_import_state r
		WHERE NOT EXISTS (SELECT 1 FROM import_state s WHERE s.source = r.source AND s.file_path = r.file_path)`)
	if err != nil {
		return fmt.Errorf("restoring import state: %w", err)
	}
	if result.SessionFiles, err = res.RowsAffected(); err != nil {
		return err
	}
	if _, err := w.tx.ExecContext(ctx, "DROP TABLE restore_import_state"); err != nil {
		return fmt.Errorf("dropping import state copy: %w", err)
	}
	return nil
}
