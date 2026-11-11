package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ImportState represents the import state for a single file
type ImportState struct {
	Source       string
	FilePath     string
	FileHash     string
	ImportedAt   time.Time
	RecordCount  int
	ByteOffset   int64
	MessageCount int
	ParserState  string
	// FileSize and FileModTimeNs describe the file the import parsed; while
	// they match, the file is unchanged and need not be hashed
	FileSize      int64
	FileModTimeNs int64
	// SourceTracked is set when all stored records of the file carry its
	// path in SourceFile, so a re-import can replace them. Records stored by
	// versions before migration 3 carry none.
	SourceTracked bool
	// Partial is set when an import stored only the records in a date range:
	// the next import reads the file again even if it is unchanged. When
	// records after the range were left out, ByteOffset and MessageCount are
	// 0, so watch reads the whole file again and replaces its records.
	Partial bool
}

const importStateColumns = `source, file_path, file_hash, imported_at, record_count, byte_offset,
	message_count, parser_state, file_size, file_mtime_ns, source_tracked, partial`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanImportState(row rowScanner) (*ImportState, error) {
	var state ImportState
	var parserState sql.NullString
	var size, mtime sql.NullInt64
	var tracked, partial sql.NullBool
	if err := row.Scan(
		&state.Source,
		&state.FilePath,
		&state.FileHash,
		&state.ImportedAt,
		&state.RecordCount,
		&state.ByteOffset,
		&state.MessageCount,
		&parserState,
		&size,
		&mtime,
		&tracked,
		&partial,
	); err != nil {
		return nil, err
	}
	state.ParserState = parserState.String
	state.FileSize = size.Int64
	state.FileModTimeNs = mtime.Int64
	state.SourceTracked = tracked.Bool
	state.Partial = partial.Bool
	return &state, nil
}

// GetImportState retrieves the import state for a specific file
func (s *DuckDBStore) GetImportState(ctx context.Context, source, filePath string) (*ImportState, error) {
	query := `SELECT ` + importStateColumns + ` FROM import_state WHERE source = ? AND file_path = ?`

	state, err := scanImportState(s.db.QueryRowContext(ctx, query, source, filePath))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying import state: %w", err)
	}
	return state, nil
}

// SetImportState creates or updates the import state for a file
func (s *DuckDBStore) SetImportState(ctx context.Context, state *ImportState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return setImportState(ctx, s.db, state)
}

func setImportState(ctx context.Context, execer execContexter, state *ImportState) error {
	// Use INSERT OR REPLACE for upsert behavior
	query := `INSERT OR REPLACE INTO import_state (` + importStateColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := execer.ExecContext(ctx, query,
		state.Source,
		state.FilePath,
		state.FileHash,
		state.ImportedAt,
		state.RecordCount,
		state.ByteOffset,
		state.MessageCount,
		state.ParserState,
		state.FileSize,
		state.FileModTimeNs,
		state.SourceTracked,
		state.Partial,
	)
	if err != nil {
		return fmt.Errorf("setting import state: %w", err)
	}

	return nil
}

// SetWatchFields updates the watch-specific fields for a file, creating the row if needed
func (s *DuckDBStore) SetWatchFields(ctx context.Context, source, filePath string, byteOffset int64, messageCount int, parserState string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return setWatchFields(ctx, s.db, source, filePath, byteOffset, messageCount, parserState)
}

func setWatchFields(ctx context.Context, execer execContexter, source, filePath string, byteOffset int64, messageCount int, parserState string) error {
	// Try to update existing row first
	updateQuery := `
		UPDATE import_state
		SET byte_offset = ?, message_count = ?, parser_state = ?, imported_at = ?
		WHERE source = ? AND file_path = ?
	`
	now := time.Now()
	result, err := execer.ExecContext(ctx, updateQuery, byteOffset, messageCount, parserState, now, source, filePath)
	if err != nil {
		return fmt.Errorf("updating watch fields: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}

	// If no existing row, insert a new one. Everything watch stores for the
	// file from now on carries its path, so the file is source-tracked.
	if rowsAffected == 0 {
		insertQuery := `
			INSERT INTO import_state (source, file_path, file_hash, imported_at, record_count, byte_offset, message_count, parser_state, source_tracked)
			VALUES (?, ?, '', ?, 0, ?, ?, ?, true)
		`
		_, err := execer.ExecContext(ctx, insertQuery, source, filePath, now, byteOffset, messageCount, parserState)
		if err != nil {
			return fmt.Errorf("inserting watch fields: %w", err)
		}
	}

	return nil
}
