package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// exportedStates runs ExportImportStateQuery and returns the exported
// states by file path
func exportedStates(t *testing.T, store *DuckDBStore, from, to *time.Time, service string) map[string]ImportState {
	t.Helper()
	query, args := ExportImportStateQuery(from, to, service)
	rows, err := store.db.Query("SELECT "+importStateColumns+" FROM ("+query+")", args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	states := map[string]ImportState{}
	for rows.Next() {
		state, err := scanImportState(rows)
		if err != nil {
			t.Fatal(err)
		}
		states[state.FilePath] = *state
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return states
}

func TestExportImportStateQuery(t *testing.T) {
	store, err := NewDuckDBStore(filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	day := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)

	// A Claude file with records on two days, a Codex file with records on
	// the first day, a Gemini file without records (watched, empty), and a
	// Claude file imported by an earlier version (records without path)
	file := func(source, path, service string, days ...int) {
		var logs []api.LogRecord
		for _, d := range days {
			logs = append(logs, api.LogRecord{Timestamp: day.Add(time.Duration(d)*24*time.Hour + time.Hour), ServiceName: service, Body: "x"})
		}
		state := &ImportState{Source: source, FilePath: path, FileHash: "h", ImportedAt: day, RecordCount: len(logs),
			ByteOffset: 100, MessageCount: 4, ParserState: "{}", FileSize: 100, FileModTimeNs: 7, SourceTracked: true}
		if err := store.InsertImportedFile(ctx, logs, nil, nil, state, nil); err != nil {
			t.Fatal(err)
		}
	}
	file("claude-code", "/c/two-days.jsonl", "claude-code", 0, 1)
	file("codex", "/x/first-day.jsonl", "codex_cli_rs", 0)
	file("gemini", "/g/empty.json", "gemini_cli")
	if err := store.SetImportState(ctx, &ImportState{Source: "claude-code", FilePath: "/c/legacy.jsonl", FileHash: "h",
		ImportedAt: day, ByteOffset: 50, FileSize: 50}); err != nil {
		t.Fatal(err)
	}

	unchanged := func(s ImportState) bool {
		return !s.Partial && s.ByteOffset > 0 && s.ParserState != ""
	}
	reread := func(s ImportState) bool {
		return s.Partial && s.ByteOffset == 0 && s.MessageCount == 0 && s.ParserState == ""
	}

	// All records: every state, as it is
	all := exportedStates(t, store, nil, nil, "")
	if len(all) != 4 || !unchanged(all["/c/two-days.jsonl"]) || !unchanged(all["/x/first-day.jsonl"]) || !unchanged(all["/g/empty.json"]) {
		t.Errorf("export of all records: %+v", all)
	}
	if all["/c/legacy.jsonl"].Partial || all["/c/legacy.jsonl"].ByteOffset != 50 {
		t.Errorf("legacy state changed in an export of all records: %+v", all["/c/legacy.jsonl"])
	}

	// A tool: its states only
	claude := exportedStates(t, store, nil, nil, "claude-code")
	if len(claude) != 2 || !unchanged(claude["/c/two-days.jsonl"]) {
		t.Errorf("export of claude-code: %+v", claude)
	}
	codex := exportedStates(t, store, nil, nil, "codex")
	if len(codex) != 1 || !unchanged(codex["/x/first-day.jsonl"]) {
		t.Errorf("export of codex: %+v", codex)
	}

	// The first day: the file cut by the range is read again; files without
	// records in it are left out, except the legacy one, read again too
	from, to := day, day.Add(24*time.Hour-time.Second)
	first := exportedStates(t, store, &from, &to, "")
	if len(first) != 3 {
		t.Errorf("export of the first day: %+v", first)
	}
	if s := first["/c/two-days.jsonl"]; !reread(s) || !s.SourceTracked {
		t.Errorf("cut file: %+v, want read again", s)
	}
	if s := first["/x/first-day.jsonl"]; !unchanged(s) {
		t.Errorf("file within the range: %+v, want unchanged", s)
	}
	if s, ok := first["/c/legacy.jsonl"]; !ok || !reread(s) || s.SourceTracked {
		t.Errorf("legacy file: %+v (%v), want read again", s, ok)
	}

	// The second day of Codex: no Codex records in it, and no Codex file of
	// an earlier version, so no states
	from, to = day.Add(24*time.Hour), day.Add(48*time.Hour)
	if second := exportedStates(t, store, &from, &to, "codex"); len(second) != 0 {
		t.Errorf("export of codex's second day: %+v", second)
	}

	// One service of a tool, without file records: no states, not even of
	// the tool's files without records
	if service := exportedStates(t, store, nil, nil, "codex-app-server"); len(service) != 0 {
		t.Errorf("export of a service without file records: %+v", service)
	}
}
