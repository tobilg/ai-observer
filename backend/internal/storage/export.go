package storage

import (
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/tools"
)

// ServiceCondition is the SQL condition of a service filter for queries
// built outside this package, such as exports: a tool name (codex) matches
// all of the tool's services, a service name only itself. It returns "" for
// no filter.
func ServiceCondition(service string) (string, []any) {
	return serviceCondition(service)
}

// ExportBaselinesQuery returns the query of the metric baselines an export
// of the metrics of service (a service filter) in [from, to] needs for its
// cumulative series to keep their per-interval increases: the stored
// baselines up to to, and, for an export from a start, the last point of
// each cumulative series before it within the lookback of series queries.
// Each series keeps its latest one, like the baselines of a deletion
// (keepCumulativeBaselines).
func ExportBaselinesQuery(from, to *time.Time, service string) (string, []any) {
	cond, serviceArgs := serviceCondition(service)

	var stored conditions
	if to != nil {
		stored.add("Timestamp <= ?::TIMESTAMP", formatTimeForDB(*to))
	}
	if cond != "" {
		stored.add(cond, serviceArgs...)
	}
	parts := []string{`
		SELECT ` + baselineColumns + `
		FROM metric_baselines` + stored.clause("WHERE")}
	args := append([]any{}, stored.args...)

	if from != nil {
		var before conditions
		before.add("Timestamp < ?::TIMESTAMP AND Timestamp >= ?::TIMESTAMP",
			formatTimeForDB(*from), formatTimeForDB(from.Add(-cumulativeLookback)))
		before.add("AggregationTemporality = 2 AND COALESCE(Value, Sum) IS NOT NULL")
		if cond != "" {
			before.add(cond, serviceArgs...)
		}
		parts = append(parts, `
		SELECT ServiceName, MetricName, CAST(Attributes AS VARCHAR), CAST(ResourceAttributes AS VARCHAR),
			Timestamp, COALESCE(Value, Sum), Count, BucketCounts, ExplicitBounds
		FROM otel_metrics`+before.clause("WHERE"))
		args = append(args, before.args...)
	}

	query := `
		SELECT ServiceName, MetricName, Attributes, ResourceAttributes,
			MAX(Timestamp) AS Timestamp, arg_max(Value, Timestamp) AS Value,
			arg_max_null(Count, Timestamp) AS Count, arg_max_null(BucketCounts, Timestamp) AS BucketCounts,
			arg_max_null(ExplicitBounds, Timestamp) AS ExplicitBounds
		FROM (` + strings.Join(parts, "\n		UNION ALL") + `
		)
		GROUP BY ServiceName, MetricName, Attributes, ResourceAttributes`
	return query, args
}

// ExportImportStateQuery returns the query of the import state an export of
// the records of service (a service filter) in [from, to] carries, so that
// importing or watching the same session files after a restore replaces
// their records instead of storing them again. The exported records keep
// their session file path (SourceFile), which the state is keyed by.
//
// A file's state is exported when records of the file are exported. When
// the file also has records the export leaves out (outside the range or
// the service filter), its state is marked partial with its watch offsets
// at 0: the next import or watch reads the whole file again and replaces
// its records, as after an import of a date range. An export of all
// records of a tool (or all tools) also carries the state of the tool's
// files without stored records; a ranged one carries only the state of
// such files with records of earlier versions (source_tracked false),
// marked to be read again, so their restored records are replaced too.
func ExportImportStateQuery(from, to *time.Time, service string) (string, []any) {
	var sel conditions
	if from != nil {
		sel.add("Timestamp >= ?::TIMESTAMP", formatTimeForDB(*from))
	}
	if to != nil {
		sel.add("Timestamp <= ?::TIMESTAMP", formatTimeForDB(*to))
	}
	sel.addService(service)
	selected := "true"
	if len(sel.sql) > 0 {
		selected = strings.Join(sel.sql, " AND ")
	}

	ranged := "false"
	if from != nil || to != nil {
		ranged = "true"
	}
	// Files without records: those of the exported tool
	stateOnly := "false"
	var stateOnlyArgs []any
	switch {
	case service == "":
		stateOnly = "true"
	default:
		if tool, ok := tools.Parse(service); ok {
			stateOnly = "s.source = ?"
			stateOnlyArgs = []any{string(tool)}
		}
	}

	reread := "(COALESCE(f.outside, false) OR (f.SourceFile IS NULL AND " + ranged + "))"
	query := `
		WITH file_records AS (
			SELECT SourceFile, bool_or(` + selected + `) AS exported, bool_or(NOT (` + selected + `)) AS outside
			FROM (
				SELECT SourceFile, Timestamp, ServiceName FROM otel_logs WHERE SourceFile IS NOT NULL
				UNION ALL
				SELECT SourceFile, Timestamp, ServiceName FROM otel_metrics WHERE SourceFile IS NOT NULL
				UNION ALL
				SELECT SourceFile, Timestamp, ServiceName FROM otel_traces WHERE SourceFile IS NOT NULL
			)
			GROUP BY SourceFile
		)
		SELECT s.* REPLACE (
			COALESCE(s.partial, false) OR ` + reread + ` AS partial,
			CASE WHEN ` + reread + ` THEN 0 ELSE s.byte_offset END AS byte_offset,
			CASE WHEN ` + reread + ` THEN 0 ELSE s.message_count END AS message_count,
			CASE WHEN ` + reread + ` THEN NULL ELSE s.parser_state END AS parser_state
		)
		FROM import_state s
		LEFT JOIN file_records f ON f.SourceFile = s.file_path
		WHERE COALESCE(f.exported, false)
			OR (f.SourceFile IS NULL AND ` + stateOnly + ` AND (NOT ` + ranged + ` OR NOT COALESCE(s.source_tracked, false)))
		ORDER BY s.source, s.file_path`
	args := concatArgs(sel.args, sel.args, stateOnlyArgs)
	return query, args
}
