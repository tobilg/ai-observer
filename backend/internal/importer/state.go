package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

// StateManager handles import state tracking and deduplication
type StateManager struct {
	store *storage.DuckDBStore
}

// NewStateManager creates a new state manager
func NewStateManager(store *storage.DuckDBStore) *StateManager {
	return &StateManager{store: store}
}

// FileStatus represents the import status of a file
type FileStatus string

const (
	StatusNew      FileStatus = "new"      // File has not been imported before
	StatusModified FileStatus = "modified" // File has been modified since last import
	StatusCurrent  FileStatus = "current"  // File has already been imported and hasn't changed
)

// CheckFileStatus determines if a file needs to be imported
func (m *StateManager) CheckFileStatus(ctx context.Context, source SourceType, filePath string) (FileStatus, error) {
	// Get current file hash
	currentHash, err := computeFileHash(filePath)
	if err != nil {
		return "", fmt.Errorf("computing file hash: %w", err)
	}

	// Get stored state
	state, err := m.store.GetImportState(ctx, string(source), filePath)
	if err != nil {
		return "", fmt.Errorf("getting import state: %w", err)
	}

	// File has never been imported
	if state == nil {
		return StatusNew, nil
	}

	// File has been modified
	if state.FileHash != currentHash {
		return StatusModified, nil
	}

	// File is current (already imported, no changes)
	return StatusCurrent, nil
}

// RecordImport records that a file has been imported
func (m *StateManager) RecordImport(ctx context.Context, source SourceType, filePath string, recordCount int) error {
	hash, err := computeFileHash(filePath)
	if err != nil {
		return fmt.Errorf("computing file hash: %w", err)
	}

	state := &storage.ImportState{
		Source:      string(source),
		FilePath:    filePath,
		FileHash:    hash,
		ImportedAt:  time.Now(),
		RecordCount: recordCount,
	}

	if err := m.store.SetImportState(ctx, state); err != nil {
		return fmt.Errorf("setting import state: %w", err)
	}

	return nil
}

// ClearSource removes all import state for a source
func (m *StateManager) ClearSource(ctx context.Context, source SourceType) error {
	return m.store.ClearImportState(ctx, string(source))
}

// GetImportedFiles returns all imported files for a source
func (m *StateManager) GetImportedFiles(ctx context.Context, source SourceType) ([]storage.ImportState, error) {
	return m.store.ListImportedFiles(ctx, string(source))
}

// computeFileHash computes the SHA256 hash of a file
func computeFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ShouldImportFile determines if a file should be imported based on options and status
func ShouldImportFile(status FileStatus, force bool) bool {
	switch status {
	case StatusNew:
		return true
	case StatusModified:
		return true
	case StatusCurrent:
		return force // Only import if force flag is set
	default:
		return false
	}
}

// StatusToString converts FileStatus to a display string
func StatusToString(status FileStatus) string {
	switch status {
	case StatusNew:
		return "new"
	case StatusModified:
		return "modified"
	case StatusCurrent:
		return "skipped"
	default:
		return "unknown"
	}
}
