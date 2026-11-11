package storage

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tobilg/ai-observer/internal/msgmerge"
	"github.com/tobilg/ai-observer/internal/tools"
)

// bodyEvent is the event name a log's body gives when its event.name
// attribute is empty (msgmerge.EventName)
const bodyEvent = "CASE WHEN contains(Body, '.') AND NOT contains(Body, ' ') THEN Body END"

// Session titles: the first user prompt, on one line
const (
	// TitleLength bounds a title, in characters (runes)
	TitleLength = 200
	// titleSourceLength bounds the prompt text a title is made from, so a
	// long pasted prompt is not redacted whole. It leaves the redactor far
	// more text than a title shows (titleSourceMargin).
	titleSourceLength = 4000
	// titleSourceMargin is the text at the end of a cut prompt that a title
	// never shows: a secret cut there may not be recognized
	titleSourceMargin = 1000
)

// SessionTitle returns the title of a session whose first user prompt is
// prompt: redacted with redact (may be nil), whitespace collapsed to single
// spaces, cut to TitleLength characters with "…". It is "" for blank
// prompts, for prompts the tool did not record (msgmerge.UnrecordedPrompt)
// and for context the tool injected as a user message
// (msgmerge.InjectedPrompt, such as Codex's <environment_context>).
// Session lists and transcripts make titles with it, so both show the same.
func SessionTitle(prompt string, redact func(string) string) string {
	text := strings.TrimSpace(prompt)
	if text == "" || msgmerge.UnrecordedPrompt(text) || msgmerge.InjectedPrompt(text) {
		return ""
	}
	cut := false
	if utf8.RuneCountInString(text) > titleSourceLength {
		text, cut = cutRunes(text, titleSourceLength), true
	}
	if redact != nil {
		text = redact(text)
	}
	text = strings.Join(strings.Fields(text), " ")
	n := utf8.RuneCountInString(text)
	shown := min(n, TitleLength)
	if cut {
		// Collapsed whitespace can move the end of the cut text into the
		// title: never show its last titleSourceMargin characters
		shown = min(shown, max(n-titleSourceMargin, 0))
	}
	switch {
	case shown <= 1 && shown < n:
		return "" // nothing that can be shown
	case shown < n:
		return strings.TrimSpace(cutRunes(text, shown-1)) + "…"
	}
	return text
}

// cutRunes returns the first n runes of s
func cutRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// sessionDetailsQuery returns the query of fillSessionExtents for sessions
// placeholders (session IDs, the query's first arguments), and the
// arguments that follow them. Per session and service it reads, from all of
// its logs: the first and last time, the number of message logs (the logs
// that become transcript messages; the transcript's count when none of the
// session's records merge, see fillSessionExtents), whether the session file
// and OTLP events record messages, the latest model, the first user prompt that
// gives a title (one character more than SessionTitle reads, so it sees the
// cut), the latest repository and branch, and the usage of the messages
// (what transcripts read: tools.CostKeys, InputTokenKeys, OutputTokenKeys),
// NULL when no message carries it, summed over the message logs.
//
// The attributes are read with one JSON extraction of all keys per log:
// Codex's tool results carry their output, so a session's attributes are
// often MBs, and every extraction parses them again (22 extractions took 4x
// as long as the whole session list did before).
func sessionDetailsQuery(sessions int) (string, []any) {
	placeholders := func(n int) string { return "?" + strings.Repeat(", ?", n-1) }
	messages := tools.TranscriptEvents()
	prompts := tools.PromptEvents
	users := tools.EventsWithRole(tools.RoleUser)
	// Expressions first: extract reads the keys they refer to. The event
	// name is read as transcripts read it: the event.name attribute, else
	// the body (Codex CLI's events have it there); a session shows the
	// model of its latest log that names one.
	var a attributePaths
	event := "COALESCE(" + a.value("event.name") + ", " + bodyEvent + ")"
	details := `event IN (` + placeholders(len(messages)) + `) AS is_message,
				` + a.value(tools.ModelKeys...) + ` AS model,
				CASE
					WHEN event IN (` + placeholders(len(prompts)) + `) THEN COALESCE(` + a.value(tools.PromptKey) + `, Body)
					WHEN event IN (` + placeholders(len(users)) + `) THEN Body
					WHEN event = ? AND ` + a.column("message.role") + ` = ? THEN Body
				END AS prompt,
				` + a.value(tools.RepositoryKey) + ` AS repository,
				` + a.value(tools.BranchKey) + ` AS branch,
				` + a.number(tools.CostKeys...) + ` AS cost,
				TRY_CAST(trunc(` + a.number(tools.InputTokenKeys...) + `) AS BIGINT) AS input_tokens,
				TRY_CAST(trunc(` + a.number(tools.OutputTokenKeys...) + `) AS BIGINT) AS output_tokens`
	query := `
		WITH session_logs AS (
			SELECT SessionId, ServiceName, Timestamp, Body, ` + a.extract("LogAttributes") + ` AS v
			FROM otel_logs
			WHERE SessionId IN (` + placeholders(sessions) + `)
		), events AS (
			SELECT *, ` + event + ` AS event FROM session_logs
		), details AS (
			SELECT SessionId, ServiceName, Timestamp, event, ` + details + `
			FROM events
		)
		SELECT
			SessionId,
			ServiceName,
			MIN(Timestamp),
			MAX(Timestamp),
			COUNT(*) FILTER (WHERE is_message),
			COALESCE(bool_or(is_message AND event = ?), false),
			COALESCE(bool_or(is_message AND event <> ?), false),
			arg_max(model, Timestamp) FILTER (WHERE model IS NOT NULL),
			arg_min(left(prompt, ` + fmt.Sprint(titleSourceLength+1) + `), Timestamp)
				FILTER (WHERE regexp_matches(prompt, '\S')
					AND (strlen(prompt) > ` + fmt.Sprint(msgmerge.UnrecordedPromptMaxLen) + ` OR NOT regexp_full_match(prompt, ` + sqlString(msgmerge.UnrecordedPromptPattern) + `))
					AND NOT regexp_matches(prompt, ` + sqlString(msgmerge.InjectedPromptPattern) + `)),
			arg_max(repository, Timestamp) FILTER (WHERE repository IS NOT NULL),
			arg_max(branch, Timestamp) FILTER (WHERE branch IS NOT NULL),
			SUM(cost) FILTER (WHERE is_message),
			SUM(input_tokens) FILTER (WHERE is_message),
			SUM(output_tokens) FILTER (WHERE is_message)
		FROM details
		GROUP BY SessionId, ServiceName`
	args := concatArgs(stringArgs(messages), stringArgs(prompts), stringArgs(users),
		[]any{tools.TranscriptMessageEvent, tools.RoleUser,
			tools.TranscriptMessageEvent, tools.TranscriptMessageEvent})
	return query, args
}

// attributePaths collects the attribute keys a query reads, so that they are
// extracted from the JSON attributes in one pass (extract) and referred to
// by position
type attributePaths struct {
	keys []string
}

// column is the SQL expression for the value of key ("v[i]"), NULL when the
// key is missing
func (a *attributePaths) column(key string) string {
	i := slices.Index(a.keys, key)
	if i < 0 {
		a.keys = append(a.keys, key)
		i = len(a.keys) - 1
	}
	return fmt.Sprintf("v[%d]", i+1)
}

// value is the SQL expression for the first non-empty value of keys
// (attributeValueExpr)
func (a *attributePaths) value(keys ...string) string {
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = "NULLIF(" + a.column(key) + ", '')"
	}
	return "COALESCE(" + strings.Join(values, ", ") + ")"
}

// number is the SQL expression for the number in the first of keys with a
// non-blank value, NULL when it is no number (attrval.Float)
func (a *attributePaths) number(keys ...string) string {
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = "NULLIF(trim(" + a.column(key) + "), '')"
	}
	return "TRY_CAST(COALESCE(" + strings.Join(values, ", ") + ") AS DOUBLE)"
}

// extract is the SQL expression extracting the values of all keys referred
// to so far from the JSON column, as a list (call it last)
func (a *attributePaths) extract(column string) string {
	paths := make([]string, len(a.keys))
	for i, key := range a.keys {
		paths[i] = `'$."` + key + `"'`
	}
	return "json_extract_string(" + column + ", [" + strings.Join(paths, ", ") + "])"
}

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// sqlString returns a constant of the code as an SQL string literal. A
// literal regular expression is compiled once per query; as a parameter, per
// row.
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// sqlIn returns the condition that expr is one of values, which must be
// constants of the code, never request data. It is list_contains of a list
// literal: DuckDB evaluates IN lists, of parameters or literals, as joins
// that copy the logs' attributes (50 of 120 ms of a Codex session's count).
func sqlIn(expr string, values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return "list_contains([" + strings.Join(quoted, ", ") + "], " + expr + ")"
}
