package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type execContexter interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

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
}

// GetImportState retrieves the import state for a specific file
func (s *DuckDBStore) GetImportState(ctx context.Context, source, filePath string) (*ImportState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT source, file_path, file_hash, imported_at, record_count, byte_offset, message_count, parser_state
		FROM import_state
		WHERE source = ? AND file_path = ?
	`

	var state ImportState
	var parserState sql.NullString
	err := s.db.QueryRowContext(ctx, query, source, filePath).Scan(
		&state.Source,
		&state.FilePath,
		&state.FileHash,
		&state.ImportedAt,
		&state.RecordCount,
		&state.ByteOffset,
		&state.MessageCount,
		&parserState,
	)
	state.ParserState = parserState.String

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying import state: %w", err)
	}

	return &state, nil
}

// SetImportState creates or updates the import state for a file
func (s *DuckDBStore) SetImportState(ctx context.Context, state *ImportState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use INSERT OR REPLACE for upsert behavior
	query := `
		INSERT OR REPLACE INTO import_state (source, file_path, file_hash, imported_at, record_count, byte_offset, message_count, parser_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		state.Source,
		state.FilePath,
		state.FileHash,
		state.ImportedAt,
		state.RecordCount,
		state.ByteOffset,
		state.MessageCount,
		state.ParserState,
	)
	if err != nil {
		return fmt.Errorf("setting import state: %w", err)
	}

	return nil
}

// DeleteImportState removes the import state for a specific file
func (s *DuckDBStore) DeleteImportState(ctx context.Context, source, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `DELETE FROM import_state WHERE source = ? AND file_path = ?`
	_, err := s.db.ExecContext(ctx, query, source, filePath)
	if err != nil {
		return fmt.Errorf("deleting import state: %w", err)
	}

	return nil
}

// ListImportedFiles returns all imported files for a specific source
func (s *DuckDBStore) ListImportedFiles(ctx context.Context, source string) ([]ImportState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT source, file_path, file_hash, imported_at, record_count, byte_offset, message_count, parser_state
		FROM import_state
		WHERE source = ?
		ORDER BY imported_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, source)
	if err != nil {
		return nil, fmt.Errorf("listing imported files: %w", err)
	}
	defer rows.Close()

	var states []ImportState
	for rows.Next() {
		var state ImportState
		var parserState sql.NullString
		if err := rows.Scan(
			&state.Source,
			&state.FilePath,
			&state.FileHash,
			&state.ImportedAt,
			&state.RecordCount,
			&state.ByteOffset,
			&state.MessageCount,
			&parserState,
		); err != nil {
			return nil, fmt.Errorf("scanning import state: %w", err)
		}
		state.ParserState = parserState.String
		states = append(states, state)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating import states: %w", err)
	}

	return states, nil
}

// ClearImportState removes all import state for a specific source
func (s *DuckDBStore) ClearImportState(ctx context.Context, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `DELETE FROM import_state WHERE source = ?`
	_, err := s.db.ExecContext(ctx, query, source)
	if err != nil {
		return fmt.Errorf("clearing import state: %w", err)
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

	// If no existing row, insert a new one
	if rowsAffected == 0 {
		insertQuery := `
			INSERT INTO import_state (source, file_path, file_hash, imported_at, record_count, byte_offset, message_count, parser_state)
			VALUES (?, ?, '', ?, 0, ?, ?, ?)
		`
		_, err := execer.ExecContext(ctx, insertQuery, source, filePath, now, byteOffset, messageCount, parserState)
		if err != nil {
			return fmt.Errorf("inserting watch fields: %w", err)
		}
	}

	return nil
}

// GetImportStats returns counts of imported files by source
func (s *DuckDBStore) GetImportStats(ctx context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT source, COUNT(*) as count
		FROM import_state
		GROUP BY source
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying import stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var source string
		var count int
		if err := rows.Scan(&source, &count); err != nil {
			return nil, fmt.Errorf("scanning import stats: %w", err)
		}
		stats[source] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating import stats: %w", err)
	}

	return stats, nil
}
