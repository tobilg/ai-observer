package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/msgmerge"
	"github.com/tobilg/ai-observer/internal/tools"
)

// QuerySessions returns a page of the sessions with logs in [from, to], the
// most recently active in the range first. Each session shows its whole
// extent: start, last time, message count and latest model over all of its
// logs, also those outside the range.
// Supports: Claude Code (transcript.message), Gemini CLI (session.id), Codex CLI (conversation.id),
// Copilot (session.id / gen_ai.conversation.id / copilot_chat.session_id), OpenCode (session.id)
func (s *DuckDBStore) QuerySessions(ctx context.Context, service string, from, to time.Time, page Page) (*api.SessionsResponse, error) {
	// SessionId is extracted from the tool-specific attributes (session.id,
	// conversation.id, gen_ai.conversation.id, ...) when a log is stored
	var where conditions
	where.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	where.add("SessionId IS NOT NULL")
	where.addService(service)

	limit, offset, err := pageLimit(page)
	if err != nil {
		return nil, err
	}
	var after conditions
	if page.Cursor != "" {
		c, err := decodeCursor(page.Cursor, 2)
		if err != nil {
			return nil, err
		}
		cond, args := newestFirstAfter("last_active", []string{"SessionId", "ServiceName"}, c.Time, []any{c.Keys[0], c.Keys[1]})
		after.add(cond, args...)
	}
	// The total is counted over the grouped sessions before the cursor
	// condition, in the same scan as the page
	query := `
		WITH active AS (
			SELECT SessionId, ServiceName, MAX(Timestamp) AS last_active
			FROM otel_logs` + where.clause("WHERE") + `
			GROUP BY SessionId, ServiceName
		), counted AS (
			SELECT *, COUNT(*) OVER () AS total FROM active
		)
		SELECT SessionId, ServiceName, last_active, total
		FROM counted` + after.clause("WHERE") + `
		ORDER BY last_active DESC, SessionId DESC, ServiceName DESC
		LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, concatArgs(where.args, after.args, []any{limit + 1, offset})...)
	if err != nil {
		return nil, fmt.Errorf("querying sessions: %w", err)
	}
	defer rows.Close()

	var sessions []api.Session
	var lastActive []time.Time
	total := -1
	for rows.Next() {
		var session api.Session
		var active time.Time
		if err := rows.Scan(&session.SessionID, &session.ServiceName, &active, &total); err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}
		// Replaced by the session's whole extent below
		session.StartTime, session.LastTime = active, active
		sessions = append(sessions, session)
		lastActive = append(lastActive, active)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sessions: %w", err)
	}

	if total < 0 {
		// An empty page has no row to carry the total
		if page.Cursor == "" && offset == 0 {
			total = 0
		} else {
			countQuery := "SELECT COUNT(*) FROM (SELECT 1 FROM otel_logs" + where.clause("WHERE") + " GROUP BY SessionId, ServiceName)"
			if err := s.db.QueryRowContext(ctx, countQuery, where.args...).Scan(&total); err != nil {
				return nil, fmt.Errorf("counting sessions: %w", err)
			}
		}
	}

	resp := &api.SessionsResponse{Sessions: sessions, Total: total}
	if len(sessions) > limit {
		resp.Sessions = sessions[:limit]
		resp.HasMore = true
		last := sessions[limit-1]
		resp.NextCursor = encodeCursor(pageCursor{
			Time: formatTimeForDB(lastActive[limit-1]),
			Keys: []string{last.SessionID, last.ServiceName},
		})
	}
	if resp.Sessions == nil {
		resp.Sessions = []api.Session{} // clients read a list
	}
	if err := s.fillSessionExtents(ctx, resp.Sessions); err != nil {
		return nil, err
	}
	return resp, nil
}

// fillSessionExtents sets the start, last time, message count, latest
// model, title, repository, branch and usage of sessions from all of their
// logs (sessionDetailsQuery). Messages are the logs that become transcript
// messages (tools.TranscriptEvents); other events of a session (conversation
// starts, connection events) do not count. The count and usage are the
// transcript's, which shows the records of one message once and fills it in
// with the others' usage (msgmerge.Merge): sessions whose records can merge
// (recorded both in session files and as OTLP events, and Claude Code's and
// Gemini CLI's OTLP sessions, which record a response in two events) are
// merged again from their stored message keys (mergeSessionMessages), the
// others are counted by sessionDetailsQuery, which is exact for them.
// Transcripts also match the messages of GenAI spans to these logs (Claude
// Code's tool calls); sessions without message logs count their GenAI spans
// instead (countSpanMessages), an estimate, since a span can become two
// messages.
func (s *DuckDBStore) fillSessionExtents(ctx context.Context, sessions []api.Session) error {
	if len(sessions) == 0 {
		return nil
	}
	type sessionKey struct{ id, service string }
	index := make(map[sessionKey]int, len(sessions))
	ids := make(map[string]bool, len(sessions))
	var placeholders []string
	var args []any
	for i, session := range sessions {
		index[sessionKey{session.SessionID, session.ServiceName}] = i
		if !ids[session.SessionID] {
			ids[session.SessionID] = true
			placeholders = append(placeholders, "?")
			args = append(args, session.SessionID)
		}
	}
	query, detailArgs := sessionDetailsQuery(len(placeholders))
	rows, err := s.db.QueryContext(ctx, query, concatArgs(args, detailArgs)...)
	if err != nil {
		return fmt.Errorf("querying session extents: %w", err)
	}
	defer rows.Close()
	// Sessions whose records can merge: recorded in session files and as
	// OTLP events, or with events of a tool that records a message in two
	files, otlp, merging := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for rows.Next() {
		var key sessionKey
		var start, last time.Time
		var count int
		var hasFile, hasEvents bool
		var model, prompt, repository, branch sql.NullString
		var cost sql.NullFloat64
		var inputTokens, outputTokens sql.NullInt64
		if err := rows.Scan(&key.id, &key.service, &start, &last, &count, &hasFile, &hasEvents, &model,
			&prompt, &repository, &branch, &cost, &inputTokens, &outputTokens); err != nil {
			return fmt.Errorf("scanning session extent: %w", err)
		}
		files[key.id] = files[key.id] || hasFile
		otlp[key.id] = otlp[key.id] || hasEvents
		if hasEvents && mergesEvents(key.service) {
			merging[key.id] = true
		}
		i, ok := index[key]
		if !ok {
			continue // the same session ID from a service not on the page
		}
		session := &sessions[i]
		session.StartTime, session.LastTime = start, last
		session.MessageCount = count
		session.Model = model.String
		session.Title = SessionTitle(prompt.String, s.redactor.String)
		session.Repository, session.Branch = repository.String, branch.String
		session.CostUSD = nullFloat(cost)
		session.InputTokens, session.OutputTokens = nullInt(inputTokens), nullInt(outputTokens)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating session extents: %w", err)
	}
	var merge []string
	for id := range ids {
		if merging[id] || files[id] && otlp[id] {
			merge = append(merge, id)
		}
	}
	if err := s.mergeSessionMessages(ctx, sessions, merge); err != nil {
		return err
	}
	var withoutMessages []int
	for i := range sessions {
		if sessions[i].MessageCount == 0 {
			withoutMessages = append(withoutMessages, i)
		}
	}
	return s.countSpanMessages(ctx, sessions, withoutMessages)
}

// mergesEvents reports whether the OTLP events of service record a message
// in more than one event (msgmerge.Merge): Claude Code's api_request and
// assistant_response, Gemini CLI's api_response and operation details
func mergesEvents(service string) bool {
	tool, ok := tools.ToolForService(service)
	return ok && (tool == tools.Claude || tool == tools.Gemini)
}

// mergeSessionMessages sets the message count and usage of the sessions
// with the given IDs to their transcripts': it merges each session's
// message records by their stored keys (message_keys.go) as transcripts
// merge them, in the transcripts' order, and counts the shown messages and
// their filled-in usage per service. Keys that are not stored for this
// version are computed from the records' attributes.
func (s *DuckDBStore) mergeSessionMessages(ctx context.Context, sessions []api.Session, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	type sessionKey struct{ id, service string }
	index := make(map[sessionKey]int, len(sessions))
	for i, session := range sessions {
		index[sessionKey{session.SessionID, session.ServiceName}] = i
	}
	// The order of SessionLogs, which transcripts are built from. Only the
	// keys are read: reading the attributes as well, even where a CASE
	// does not use them, made the query 3x slower.
	rows, err := s.db.QueryContext(ctx, `
		SELECT SessionId, ServiceName, Timestamp, MessageKey,
			CASE WHEN MessageKey IS NULL OR NOT starts_with(MessageKey, ?) THEN rowid END
		FROM otel_logs
		WHERE SessionId IN (`+placeholderList(len(ids))+`) AND MessageKey IS DISTINCT FROM ?
		ORDER BY SessionId, Timestamp, rowid`,
		concatArgs([]any{msgmerge.KeyPrefix()}, stringArgs(ids), []any{msgmerge.NotMessage()})...)
	if err != nil {
		return fmt.Errorf("reading session message keys: %w", err)
	}
	type record struct {
		session, service string
		entry            msgmerge.Entry
		message          bool
	}
	var records []record
	stale := make(map[int64]int)
	for rows.Next() {
		var r record
		var stored sql.NullString
		var rowid sql.NullInt64
		if err := rows.Scan(&r.session, &r.service, &r.entry.Time, &stored, &rowid); err != nil {
			rows.Close()
			return fmt.Errorf("scanning session message key: %w", err)
		}
		if rowid.Valid {
			stale[rowid.Int64] = len(records)
		} else {
			r.entry.Key, r.message = msgmerge.DecodeKey(stored.String)
		}
		records = append(records, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating session message keys: %w", err)
	}
	if err := s.computeMessageKeys(ctx, stale, func(i int, key msgmerge.Key, ok bool) {
		records[i].entry.Key, records[i].message = key, ok
	}); err != nil {
		return err
	}

	var entries []msgmerge.Entry
	var services []string
	for i, r := range records {
		if r.message {
			entries = append(entries, r.entry)
			services = append(services, r.service)
		}
		if i+1 < len(records) && records[i+1].session == r.session {
			continue
		}
		if len(entries) > 0 {
			session := r.session
			applySessionMerge(sessions, func(service string) (int, bool) {
				i, ok := index[sessionKey{session, service}]
				return i, ok
			}, entries, services)
		}
		entries, services = entries[:0], services[:0]
	}
	return nil
}

// computeMessageKeys computes the message keys of the logs with the given
// row IDs from their attributes (keys that are not stored for this
// version: message_keys.go) and passes them to set with the index stale
// maps them to
func (s *DuckDBStore) computeMessageKeys(ctx context.Context, stale map[int64]int, set func(i int, key msgmerge.Key, ok bool)) error {
	if len(stale) == 0 {
		return nil
	}
	ids := make([]any, 0, len(stale))
	for id := range stale {
		ids = append(ids, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT rowid, ServiceName, Body, LogAttributes FROM otel_logs
		WHERE rowid IN (`+placeholderList(len(ids))+`)`, ids...)
	if err != nil {
		return fmt.Errorf("reading logs without message keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rowid int64
		var service string
		var body sql.NullString
		var attrs any
		if err := rows.Scan(&rowid, &service, &body, &attrs); err != nil {
			return fmt.Errorf("scanning log: %w", err)
		}
		key, ok := msgmerge.KeyOf(service, body.String, scanJSONToMap(attrs))
		set(stale[rowid], key, ok)
	}
	return rows.Err()
}

// applySessionMerge merges the records of one session (entries, recorded by
// services) and sets the message count and usage of its sessions on the
// page (row returns the index of the session of a service)
func applySessionMerge(sessions []api.Session, row func(service string) (int, bool), entries []msgmerge.Entry, services []string) {
	plan := msgmerge.Merge(entries)
	usage := make([]msgmerge.Usage, len(entries))
	for i, e := range entries {
		usage[i] = e.Key.Usage
	}
	plan.Replay(func(into, from int) {
		usage[into].Fill(usage[from])
	})
	type totals struct {
		count                        int
		input, output                int64
		cost                         float64
		hasInput, hasOutput, hasCost bool
	}
	byService := make(map[string]*totals)
	for i := range entries {
		t := byService[services[i]]
		if t == nil {
			t = &totals{}
			byService[services[i]] = t
		}
		if plan.Into[i] != msgmerge.Shown {
			continue
		}
		u := usage[i]
		t.count++
		t.input += int64(u.InputTokens)
		t.output += int64(u.OutputTokens)
		t.cost += u.CostUSD
		t.hasInput = t.hasInput || u.HasInput
		t.hasOutput = t.hasOutput || u.HasOutput
		t.hasCost = t.hasCost || u.HasCost
	}
	for service, t := range byService {
		i, ok := row(service)
		if !ok {
			continue
		}
		s := &sessions[i]
		s.MessageCount = t.count
		s.InputTokens, s.OutputTokens, s.CostUSD = nil, nil, nil
		if t.hasInput {
			s.InputTokens = &t.input
		}
		if t.hasOutput {
			s.OutputTokens = &t.output
		}
		if t.hasCost {
			s.CostUSD = &t.cost
		}
	}
}

// countSpanMessages sets the message count of the sessions at the given
// indices, none of whose logs is a message, to the number of their GenAI
// spans, the spans that become transcript messages (Copilot's conversations
// are on spans). Sessions without such spans keep 0: their transcript is
// empty, as for Codex's approval reviews.
func (s *DuckDBStore) countSpanMessages(ctx context.Context, sessions []api.Session, indices []int) error {
	if len(indices) == 0 {
		return nil
	}
	type sessionKey struct{ id, service string }
	index := make(map[sessionKey]int, len(indices))
	ids := make([]string, len(indices))
	for n, i := range indices {
		index[sessionKey{sessions[i].SessionID, sessions[i].ServiceName}] = i
		ids[n] = sessions[i].SessionID
	}
	placeholders := "?" + strings.Repeat(", ?", len(ids)-1)
	// A span lists the aliases of its session (SessionIds); logs may name
	// the session by another of them than the span's first
	rows, err := s.db.QueryContext(ctx, `
		SELECT sid, ServiceName, COUNT(*)
		FROM (
			SELECT unnest(SessionIds) AS sid, ServiceName
			FROM otel_traces
			WHERE list_has_any(SessionIds, [`+placeholders+`])
			  AND json_exists(SpanAttributes, '$."gen_ai.operation.name"')
		)
		WHERE sid IN (`+placeholders+`)
		GROUP BY sid, ServiceName`, concatArgs(stringArgs(ids), stringArgs(ids))...)
	if err != nil {
		return fmt.Errorf("counting session spans: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key sessionKey
		var count int
		if err := rows.Scan(&key.id, &key.service, &count); err != nil {
			return fmt.Errorf("scanning session span count: %w", err)
		}
		if i, ok := index[key]; ok {
			sessions[i].MessageCount = count
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating session span counts: %w", err)
	}
	return nil
}
