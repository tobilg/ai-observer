package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/attrval"
	"github.com/tobilg/ai-observer/internal/tools"
)

// Usage overlap: a session that reaches AI Observer both over OTLP (from the
// running tool) and from its session file (import, watch) has its token usage
// and cost recorded twice. The rule: each model request is counted once, from
// OTLP when OTLP recorded it, from the file otherwise. The file's usage
// records of a request that OTLP recorded are removed; everything else of
// both sides is kept (transcript messages, the file's api_request log, the
// OTLP events).
//
// The rule is enforced in the write transaction of every insert that stores
// usage of either side (reconcileUsage): inserting file records removes
// their usage records of requests OTLP has recorded, and inserting OTLP
// records removes the file usage records of the requests they record. Writes
// are serialized (s.mu), so the outcome does not depend on the order in
// which the two sides arrive, and re-importing or watching a file again
// applies it again. Records of sessions seen through one path only are never
// touched: the rule removes a file record only when its OTLP counterpart is
// stored.
//
// Per tool, a request is recognized on both sides as follows:
//
//   - Claude Code: by its API request ID. OTLP's api_request event carries
//     request_id, as does the claude_code.api_request log that imports and
//     watch store beside the request's token and cost points (same file, same
//     timestamp). A file request counts as recorded by OTLP when an OTLP
//     api_request event has its request_id and the session also sent
//     claude_code.token.usage points over OTLP within an hour of it, so a
//     tool that exports logs but not metrics keeps the file's usage. Claude
//     Code records the api_request event and its token and cost metrics
//     together (equal sums on stored data). Requests that only OTLP records
//     (tool routing, compaction) are not in the files; requests only the file
//     records (before telemetry was enabled, or while AI Observer was down)
//     keep their file usage.
//   - Codex: by the response's token counts. Codex writes the usage of every
//     response to its session file (token_count) and AI Observer derives
//     codex_cli_rs.token.usage from the response.completed event over OTLP;
//     both carry the same input, cached, cache write, output and reasoning
//     counts, written within milliseconds of each other. A file response
//     counts as recorded by OTLP when an OTLP response of the same session
//     (when both carry session.id) has the same counts of every token type
//     within codexMatchWindow. Each OTLP response matches at most one file
//     response. There is no shared request ID.
//   - Gemini CLI: by the session and the response's token counts. Gemini
//     CLI writes the token counts of every response (input, output, cached,
//     thoughts, tool) to its session file and sends them in its
//     api_response event, both from the response's last usage metadata;
//     its token metrics are cumulative counters per session, recorded in
//     the same callback as the event. A file response counts as recorded by
//     OTLP when an OTLP api_response event of the same session has the same
//     counts within geminiMatchWindow, and the session sent
//     gemini_cli.token.usage points over OTLP at or within
//     geminiMeteredWindow after the event (a cumulative point includes all
//     earlier responses of the process), so a tool that exports logs but
//     not metrics keeps the file's usage. The session file and the events
//     carry the same session ID (the config's; a resumed session keeps
//     its). The file's response points are its token and cost points at
//     the response's time. No request ID links the two; equal counts of all
//     five types in one session are taken as one response (the input count
//     includes the whole conversation so far).

const (
	claudeService = "claude-code"
	// Claude Code's api_request event over OTLP, and the log imports store
	// beside a request's usage points
	claudeOTLPRequestEvent = "api_request"
	claudeFileRequestEvent = "claude_code.api_request"
	claudeTokenMetric      = "claude_code.token.usage"

	geminiService       = "gemini_cli"
	geminiResponseEvent = "gemini_cli.api_response"
	geminiTokenMetric   = "gemini_cli.token.usage"
	geminiCostMetric    = "gemini_cli.cost.usage"

	codexFileService = "codex_cli_rs"
	codexTokenMetric = "codex_cli_rs.token.usage"
	codexCostMetric  = "codex_cli_rs.cost.usage"

	// claudeRequestSlack bounds how far apart the file entry of a request
	// and its OTLP event may be beyond the request's duration: the entry is
	// written when the response starts, the event when it ends (19 s apart
	// at most on stored data, minus the duration)
	claudeRequestSlack = 5 * time.Minute
	// claudeMaxRequestDuration bounds the duration of a request whose OTLP
	// event carries none
	claudeMaxRequestDuration = 2 * time.Hour
	// claudeMeteredWindow is how close to a request the session's OTLP
	// token metrics must be (Claude Code exports them every minute)
	claudeMeteredWindow = time.Hour
	// codexMatchWindow bounds the time between a response's token_count in
	// the session file and its response.completed event (12 ms at most on
	// stored data)
	codexMatchWindow = 30 * time.Second
	// geminiMatchWindow bounds the time between a response in the session
	// file and its api_response event: the file records a response of tool
	// calls only once its calls have run (msgmerge.GeminiMatchWindow)
	geminiMatchWindow = time.Hour
	// geminiMeteredWindow is how soon after a response's event the
	// session's next OTLP token metrics must follow (Gemini CLI exports
	// them every 10 to 30 seconds, and on exit)
	geminiMeteredWindow = time.Hour
)

// claudeFileUsageMetrics are the metrics imports and watch store for the
// usage of a Claude Code request
var claudeFileUsageMetrics = []string{
	"claude_code.token.usage", "claude_code.token.usage_user_facing",
	"claude_code.cost.usage", "claude_code.cost.usage_user_facing",
}

// usageWindow is a time range of records and the Claude Code sessions they
// belong to
type usageWindow struct {
	from, to time.Time
	sessions map[string]bool
}

func (u *usageWindow) add(t time.Time) {
	if u.from.IsZero() || t.Before(u.from) {
		u.from = t
	}
	if t.After(u.to) {
		u.to = t
	}
}

func (u *usageWindow) addSession(id string) {
	if u.sessions == nil {
		u.sessions = make(map[string]bool)
	}
	u.sessions[id] = true
}

func (u *usageWindow) empty() bool { return u.from.IsZero() }

func (u *usageWindow) sessionList() []string {
	ids := make([]string, 0, len(u.sessions))
	for id := range u.sessions {
		ids = append(ids, id)
	}
	return ids
}

// reconcileUsage applies the usage overlap rule to the records an insert has
// just stored in w: fromFile tells whether they were read from a session
// file. It returns the number of file usage points removed.
func reconcileUsage(ctx context.Context, w *writeTx, logs []api.LogRecord, metrics []api.MetricDataPoint, fromFile bool) (int64, error) {
	var claude, codex, gemini usageWindow
	for _, l := range logs {
		if !fromFile && l.ServiceName == geminiService && l.LogAttributes["event.name"] == geminiResponseEvent && l.LogAttributes["session.id"] != "" {
			// The file responses this event can record
			gemini.add(l.Timestamp.Add(-geminiMatchWindow))
			gemini.add(l.Timestamp.Add(geminiMatchWindow))
			gemini.addSession(l.LogAttributes["session.id"])
			continue
		}
		if l.ServiceName != claudeService || l.LogAttributes["request_id"] == "" {
			continue
		}
		session := l.LogAttributes["session.id"]
		if session == "" {
			continue
		}
		switch event := l.LogAttributes["event.name"]; {
		case fromFile && event == claudeFileRequestEvent:
			claude.add(l.Timestamp)
			claude.addSession(session)
		case !fromFile && event == claudeOTLPRequestEvent:
			// The file entry of the request is written when the response
			// starts: up to its duration before the event
			duration := claudeMaxRequestDuration
			if ms := attrval.Int(l.LogAttributes, "duration_ms"); ms > 0 {
				duration = time.Duration(ms) * time.Millisecond
			}
			claude.add(l.Timestamp.Add(-duration - claudeRequestSlack))
			claude.add(l.Timestamp.Add(claudeRequestSlack))
			claude.addSession(session)
		}
	}
	for _, m := range metrics {
		switch {
		case !fromFile && m.ServiceName == claudeService && m.MetricName == claudeTokenMetric && m.Attributes["session.id"] != "":
			// The session's first token metrics cover the requests whose
			// events arrived before them
			claude.add(m.Timestamp.Add(-claudeMeteredWindow))
			claude.add(m.Timestamp)
			claude.addSession(m.Attributes["session.id"])
		case m.ServiceName == geminiService && m.MetricName == geminiTokenMetric && m.Attributes["session.id"] != "" &&
			(m.Attributes["import_source"] != "") == fromFile:
			if fromFile {
				gemini.add(m.Timestamp)
			} else {
				// The session's next token metrics cover the responses
				// whose events arrived before them
				gemini.add(m.Timestamp.Add(-geminiMeteredWindow - geminiMatchWindow))
				gemini.add(m.Timestamp.Add(geminiMatchWindow))
			}
			gemini.addSession(m.Attributes["session.id"])
		case m.MetricName == codexTokenMetric && tools.Codex.HasService(m.ServiceName) && (m.Attributes["import_source"] != "") == fromFile:
			codex.add(m.Timestamp.Add(-codexMatchWindow))
			codex.add(m.Timestamp.Add(codexMatchWindow))
		}
	}

	var removed int64
	for _, r := range []struct {
		tool      tools.Tool
		window    usageWindow
		reconcile func() (int64, error)
	}{
		{tools.Claude, claude, func() (int64, error) {
			return reconcileClaudeUsage(ctx, w, claude.sessionList(), claude.from, claude.to)
		}},
		{tools.Codex, codex, func() (int64, error) { return reconcileCodexUsage(ctx, w, codex.from, codex.to) }},
		{tools.Gemini, gemini, func() (int64, error) {
			return reconcileGeminiUsage(ctx, w, gemini.sessionList(), gemini.from, gemini.to)
		}},
	} {
		if r.window.empty() {
			continue
		}
		// File records are being stored, or OTLP ones with files read before
		if !fromFile {
			if has, err := hasFileRecords(ctx, w, r.tool); err != nil {
				return 0, err
			} else if !has {
				continue
			}
		}
		n, err := r.reconcile()
		if err != nil {
			return 0, err
		}
		removed += n
	}
	return removed, nil
}

// hasFileRecords reports whether session files of tool were ever imported or
// watched, so that OTLP inserts of users who never read files skip the
// reconciliation queries
func hasFileRecords(ctx context.Context, w *writeTx, tool tools.Tool) (bool, error) {
	var one int
	err := w.tx.QueryRowContext(ctx, `SELECT 1 FROM import_state WHERE source = ? LIMIT 1`, string(tool)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading import state: %w", err)
	}
	return true, nil
}

// reconcileClaudeUsage removes the usage points of the Claude Code requests
// of sessions read from files between from and to that OTLP recorded (see
// the rule above). The points of a request are those its claude_code.api_request
// log has: same file, same timestamp; a timestamp is only taken when every
// request logged at it in the file was recorded by OTLP.
func reconcileClaudeUsage(ctx context.Context, w *writeTx, sessions []string, from, to time.Time) (int64, error) {
	if len(sessions) == 0 {
		return 0, nil
	}
	args := []any{
		formatTimeForDB(from), formatTimeForDB(to), claudeService, claudeFileRequestEvent,
	}
	args = append(args, stringArgs(sessions)...)
	args = append(args,
		formatTimeForDB(from.Add(-claudeRequestSlack)), formatTimeForDB(to.Add(claudeMaxRequestDuration)), claudeService, claudeOTLPRequestEvent,
		formatTimeForDB(from.Add(-claudeMeteredWindow)), formatTimeForDB(to.Add(claudeMeteredWindow)), claudeService, claudeTokenMetric,
		claudeService)
	args = append(args, stringArgs(claudeFileUsageMetrics)...)
	query := `DELETE FROM otel_metrics USING (
			WITH file_requests AS (
				SELECT SourceFile, Timestamp, SessionId, json_extract_string(LogAttributes, '$.request_id') AS request_id
				FROM otel_logs
				WHERE Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND ServiceName = ?
					AND json_extract_string(LogAttributes, '$."event.name"') = ?
					AND json_extract_string(LogAttributes, '$.import_source') IS NOT NULL
					AND SessionId IN (` + placeholderList(len(sessions)) + `)
			), otlp_requests AS (
				SELECT DISTINCT request_id FROM (
					SELECT json_extract_string(LogAttributes, '$.request_id') AS request_id
					FROM otel_logs
					WHERE Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND ServiceName = ? AND SourceFile IS NULL
						AND json_extract_string(LogAttributes, '$."event.name"') = ?
						AND json_extract_string(LogAttributes, '$.import_source') IS NULL
				) WHERE request_id IS NOT NULL
			), metered AS (
				SELECT DISTINCT session_id FROM (
					SELECT json_extract_string(Attributes, '$."session.id"') AS session_id
					FROM otel_metrics
					WHERE Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND ServiceName = ? AND MetricName = ?
						AND SourceFile IS NULL AND json_extract_string(Attributes, '$.import_source') IS NULL
				) WHERE session_id IS NOT NULL
			)
			-- A timestamp of a file counts only when every request logged at
			-- it was recorded by OTLP (NULLs count as not recorded)
			SELECT SourceFile, Timestamp
			FROM file_requests
			GROUP BY SourceFile, Timestamp
			HAVING bool_and(COALESCE(request_id IN (SELECT request_id FROM otlp_requests)
				AND SessionId IN (SELECT session_id FROM metered), false))
		) AS covered
		WHERE otel_metrics.ServiceName = ?
			AND otel_metrics.MetricName IN (` + placeholderList(len(claudeFileUsageMetrics)) + `)
			AND otel_metrics.SourceFile IS NOT DISTINCT FROM covered.SourceFile
			AND otel_metrics.Timestamp = covered.Timestamp
			AND json_extract_string(otel_metrics.Attributes, '$.import_source') IS NOT NULL`
	res, err := w.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("removing Claude Code file usage recorded over OTLP: %w", err)
	}
	return res.RowsAffected()
}

// reconcileCodexUsage removes the usage points of the Codex responses read
// from files between from and to that OTLP recorded (see the rule above). A
// response is the token points of one file and timestamp, or of one OTLP
// service, timestamp and session; its fingerprint is the value of each token
// type.
func reconcileCodexUsage(ctx context.Context, w *writeTx, from, to time.Time) (int64, error) {
	services := tools.Codex.ServiceNames()
	window := fmt.Sprintf("INTERVAL %d MILLISECOND", codexMatchWindow.Milliseconds())
	args := []any{formatTimeForDB(from.Add(-codexMatchWindow)), formatTimeForDB(to.Add(codexMatchWindow)), codexTokenMetric}
	args = append(args, stringArgs(services)...)
	args = append(args, formatTimeForDB(from), formatTimeForDB(to), codexFileService)
	// Remember matches on the OTLP token points themselves. The file points
	// disappear after this transaction, so later writes cannot infer a match
	// from their presence. rowid is used only within this transaction.
	query := `CREATE TEMP TABLE codex_usage_matches AS
		WITH responses AS (
			SELECT min(rowid) AS id, SourceFile, Timestamp, ServiceName,
				json_extract_string(Attributes, '$.import_source') IS NOT NULL AS from_file,
				json_extract_string(Attributes, '$."session.id"') AS session_id,
				any_value(CodexMatch) AS prior_match,
				string_agg(json_extract_string(Attributes, '$.type') || '=' || CAST(CAST(Value AS BIGINT) AS VARCHAR),
					',' ORDER BY json_extract_string(Attributes, '$.type')) AS fingerprint
			FROM otel_metrics
			WHERE Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND MetricName = ?
				AND ServiceName IN (` + placeholderList(len(services)) + `)
			GROUP BY ALL
		), candidates AS (
			SELECT o.id AS otlp_id, f.id AS file_id,
				struct_pack(file_path := f.SourceFile, recorded_at := f.Timestamp, session_id := f.session_id) AS file_response,
				o.prior_match IS NOT NULL AS already_matched,
				o.session_id = f.session_id AS same_session,
				abs(epoch_us(o.Timestamp) - epoch_us(f.Timestamp)) AS distance
			FROM responses f JOIN responses o
				ON o.fingerprint = f.fingerprint
				AND o.Timestamp BETWEEN f.Timestamp - ` + window + ` AND f.Timestamp + ` + window + `
				AND (o.session_id IS NULL OR f.session_id IS NULL OR o.session_id = f.session_id)
			WHERE f.from_file AND f.Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND f.ServiceName = ?
				AND NOT o.from_file AND o.SourceFile IS NULL
				AND (o.prior_match IS NULL OR o.prior_match = file_response)
		), choices AS (
			SELECT * FROM candidates
			QUALIFY row_number() OVER (PARTITION BY otlp_id ORDER BY same_session DESC NULLS LAST, distance, file_id) = 1
		)
		SELECT otlp_id, file_response FROM choices
		QUALIFY row_number() OVER (PARTITION BY file_id ORDER BY already_matched DESC, same_session DESC NULLS LAST, distance, otlp_id) = 1`
	var removed int64
	for {
		if _, err := w.tx.ExecContext(ctx, query, args...); err != nil {
			return 0, fmt.Errorf("matching Codex file usage to OTLP: %w", err)
		}
		if _, err := w.tx.ExecContext(ctx, `UPDATE otel_metrics SET CodexMatch = m.file_response
			FROM codex_usage_matches m WHERE otel_metrics.rowid = m.otlp_id`); err != nil {
			return 0, fmt.Errorf("recording Codex usage matches: %w", err)
		}
		res, err := w.tx.ExecContext(ctx, `DELETE FROM otel_metrics USING codex_usage_matches m
			WHERE ServiceName = ? AND MetricName IN (?, ?)
				AND Timestamp = m.file_response.recorded_at
				AND SourceFile IS NOT DISTINCT FROM m.file_response.file_path
				AND json_extract_string(Attributes, '$."session.id"') IS NOT DISTINCT FROM m.file_response.session_id
				AND json_extract_string(Attributes, '$.import_source') IS NOT NULL`, codexFileService, codexTokenMetric, codexCostMetric)
		if err != nil {
			return 0, fmt.Errorf("removing matched Codex file usage: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if _, err := w.tx.ExecContext(ctx, "DROP TABLE temp.main.codex_usage_matches"); err != nil {
			return 0, err
		}
		if n == 0 {
			return removed, nil
		}
		// If two OTLP responses chose the same file response, the remaining
		// one can now match another file response.
		removed += n
	}
}

// reconcileGeminiUsage removes the usage points of the Gemini CLI responses
// of sessions read from files between from and to that OTLP recorded (see
// the rule above). A file response is the token points of one file and
// timestamp; its counts are the values of its token types (0 for a type
// without a point: imports store none for 0).
func reconcileGeminiUsage(ctx context.Context, w *writeTx, sessions []string, from, to time.Time) (int64, error) {
	if len(sessions) == 0 {
		return 0, nil
	}
	window := fmt.Sprintf("INTERVAL %d SECOND", int(geminiMatchWindow/time.Second))
	metered := fmt.Sprintf("INTERVAL %d SECOND", int(geminiMeteredWindow/time.Second))
	count := func(column, key string) string {
		return fmt.Sprintf("COALESCE(TRY_CAST(json_extract_string(%s, '$.%s') AS BIGINT), 0)", column, key)
	}
	tokens := func(tokenType string) string {
		return "CAST(COALESCE(SUM(Value) FILTER (WHERE token_type = '" + tokenType + "'), 0) AS BIGINT)"
	}
	args := []any{formatTimeForDB(from), formatTimeForDB(to), geminiService, geminiTokenMetric}
	args = append(args, stringArgs(sessions)...)
	args = append(args, formatTimeForDB(from.Add(-geminiMatchWindow)), formatTimeForDB(to.Add(geminiMatchWindow)), geminiService, geminiResponseEvent)
	args = append(args, stringArgs(sessions)...)
	args = append(args, formatTimeForDB(from.Add(-geminiMatchWindow)), formatTimeForDB(to.Add(geminiMatchWindow+geminiMeteredWindow)), geminiService, geminiTokenMetric)
	args = append(args, geminiService, geminiTokenMetric, geminiCostMetric)
	query := `DELETE FROM otel_metrics USING (
			WITH file_points AS (
				SELECT SourceFile, Timestamp,
					json_extract_string(Attributes, '$."session.id"') AS session_id,
					json_extract_string(Attributes, '$.type') AS token_type,
					Value
				FROM otel_metrics
				WHERE Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND ServiceName = ? AND MetricName = ?
					AND json_extract_string(Attributes, '$.import_source') IS NOT NULL
			), file_responses AS (
				SELECT SourceFile, Timestamp, session_id,
					` + tokens("input") + ` AS input, ` + tokens("output") + ` AS output, ` + tokens("cache") + ` AS cached,
					` + tokens("thought") + ` AS thoughts, ` + tokens("tool") + ` AS tool
				FROM file_points
				WHERE session_id IN (` + placeholderList(len(sessions)) + `)
				GROUP BY SourceFile, Timestamp, session_id
			), events AS (
				SELECT Timestamp, SessionId,
					` + count("LogAttributes", "input_token_count") + ` AS input,
					` + count("LogAttributes", "output_token_count") + ` AS output,
					` + count("LogAttributes", "cached_content_token_count") + ` AS cached,
					` + count("LogAttributes", "thoughts_token_count") + ` AS thoughts,
					` + count("LogAttributes", "tool_token_count") + ` AS tool
				FROM otel_logs
				WHERE Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND ServiceName = ? AND SourceFile IS NULL
					AND json_extract_string(LogAttributes, '$."event.name"') = ?
					AND json_extract_string(LogAttributes, '$.import_source') IS NULL
					AND SessionId IN (` + placeholderList(len(sessions)) + `)
			), meter AS (
				SELECT json_extract_string(Attributes, '$."session.id"') AS session_id, Timestamp
				FROM otel_metrics
				WHERE Timestamp BETWEEN ?::TIMESTAMP AND ?::TIMESTAMP AND ServiceName = ? AND MetricName = ?
					AND SourceFile IS NULL AND json_extract_string(Attributes, '$.import_source') IS NULL
			)
			SELECT DISTINCT f.SourceFile, f.Timestamp
			FROM file_responses f JOIN events e
				ON e.SessionId = f.session_id AND e.input = f.input AND e.output = f.output AND e.cached = f.cached
				AND e.thoughts = f.thoughts AND e.tool = f.tool
				AND e.Timestamp BETWEEN f.Timestamp - ` + window + ` AND f.Timestamp + ` + window + `
			WHERE f.input > 0 AND EXISTS (SELECT 1 FROM meter m WHERE m.session_id = e.SessionId
				AND m.Timestamp >= e.Timestamp AND m.Timestamp <= e.Timestamp + ` + metered + `)
		) AS covered
		WHERE otel_metrics.ServiceName = ?
			AND otel_metrics.MetricName IN (?, ?)
			AND otel_metrics.SourceFile IS NOT DISTINCT FROM covered.SourceFile
			AND otel_metrics.Timestamp = covered.Timestamp
			AND json_extract_string(otel_metrics.Attributes, '$.import_source') IS NOT NULL`
	res, err := w.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("removing Gemini CLI file usage recorded over OTLP: %w", err)
	}
	return res.RowsAffected()
}

// placeholderList returns n comma-separated placeholders
func placeholderList(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
