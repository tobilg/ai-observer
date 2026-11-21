package storage

import "context"

// Development builds recorded portions of migration 9 under versions 9–14.
// Its original name distinguishes these from the released versions, even after
// a later release uses the same version numbers.
const previewMigration9Name = "hex trace and span ids"

// migrateTelemetryV9 runs after adding the histogram baseline, message key
// and Codex match columns. The caller records the release migration in this
// same transaction, so a failed upgrade resumes from its original ledger.
func migrateTelemetryV9(ctx context.Context, w *writeTx) error {
	var previous int
	if err := w.tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&previous); err != nil {
		return err
	}
	if previous < 9 {
		// Repair earlier OTLP/JSON hex IDs that were decoded as base64.
		for _, stmt := range []string{
			migration9IDSQL("otel_traces", "TraceId", 16),
			migration9IDSQL("otel_traces", "SpanId", 8),
			migration9IDSQL("otel_traces", "ParentSpanId", 8),
			migration9IDSQL("otel_logs", "TraceId", 16),
			migration9IDSQL("otel_logs", "SpanId", 8),
			migration9LinkIDsSQL(`"Links.TraceId"`, 16),
			migration9LinkIDsSQL(`"Links.SpanId"`, 8),
		} {
			if _, err := w.tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}
	if previous < 10 {
		// Drop Codex's duplicated total, normalize cache types and derive
		// usage from previously stored app and exec server events.
		for _, stmt := range []string{
			`DELETE FROM otel_metrics
			WHERE MetricName = 'codex_cli_rs.token.usage' AND json_extract_string(Attributes, '$.type') = 'tool'`,
			`UPDATE otel_metrics SET Attributes = json_merge_patch(Attributes, '{"type": "cacheRead"}')
			WHERE MetricName = 'codex_cli_rs.token.usage' AND json_extract_string(Attributes, '$.type') = 'cache_read'`,
			`UPDATE otel_metrics SET Attributes = json_merge_patch(Attributes, '{"type": "cacheWrite"}')
			WHERE MetricName = 'codex_cli_rs.token.usage' AND json_extract_string(Attributes, '$.type') = 'cache_creation'`,
		} {
			if _, err := w.tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if err := deriveCodexServerUsage(ctx, w); err != nil {
			return err
		}
	}
	if previous < 11 {
		if _, err := w.tx.ExecContext(ctx, migration9ClaudeOverlap); err != nil {
			return err
		}
	}
	if previous >= 9 {
		// Only the recognized preview ledger is eligible to run migration 9
		// again. Replace its entries with one released entry.
		_, err := w.tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version BETWEEN 9 AND 14")
		return err
	}
	return nil
}

// Keep file usage unless every request at the same file and timestamp was
// also recorded over OTLP with metered usage for its session within an hour.
const migration9ClaudeOverlap = `DELETE FROM otel_metrics USING (
	WITH file_requests AS (
		SELECT SourceFile, Timestamp, SessionId, json_extract_string(LogAttributes, '$.request_id') AS request_id
		FROM otel_logs
		WHERE ServiceName = 'claude-code'
			AND json_extract_string(LogAttributes, '$."event.name"') = 'claude_code.api_request'
			AND json_extract_string(LogAttributes, '$.import_source') IS NOT NULL
	), otlp_requests AS (
		SELECT DISTINCT request_id FROM (
			SELECT json_extract_string(LogAttributes, '$.request_id') AS request_id
			FROM otel_logs
			WHERE ServiceName = 'claude-code' AND SourceFile IS NULL
				AND json_extract_string(LogAttributes, '$."event.name"') = 'api_request'
				AND json_extract_string(LogAttributes, '$.import_source') IS NULL
		) WHERE request_id IS NOT NULL
	), metered AS (
		SELECT DISTINCT session_id, date_trunc('minute', Timestamp) AS minute FROM (
			SELECT json_extract_string(Attributes, '$."session.id"') AS session_id, Timestamp
			FROM otel_metrics
			WHERE ServiceName = 'claude-code' AND MetricName = 'claude_code.token.usage'
				AND SourceFile IS NULL AND json_extract_string(Attributes, '$.import_source') IS NULL
		) WHERE session_id IS NOT NULL
	), recorded AS (
		SELECT f.SourceFile, f.Timestamp,
			f.request_id IN (SELECT request_id FROM otlp_requests)
			AND EXISTS (SELECT 1 FROM metered m WHERE m.session_id = f.SessionId
				AND m.minute BETWEEN f.Timestamp - INTERVAL 61 MINUTE AND f.Timestamp + INTERVAL 60 MINUTE) AS recorded
		FROM file_requests f
	)
	SELECT SourceFile, Timestamp
	FROM recorded
	GROUP BY SourceFile, Timestamp
	HAVING bool_and(COALESCE(recorded, false))
) AS covered
WHERE otel_metrics.ServiceName = 'claude-code'
	AND otel_metrics.MetricName IN ('claude_code.token.usage', 'claude_code.token.usage_user_facing',
		'claude_code.cost.usage', 'claude_code.cost.usage_user_facing')
	AND otel_metrics.SourceFile IS NOT DISTINCT FROM covered.SourceFile
	AND otel_metrics.Timestamp = covered.Timestamp
	AND json_extract_string(otel_metrics.Attributes, '$.import_source') IS NOT NULL`
