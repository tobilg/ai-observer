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
	// hashes remembers the files hashed so far, so the preview and the
	// import of one run hash each changed file once
	hashes map[string]hashedFile
}

// hashedFile is the hash of a file with the size and modification time it had
type hashedFile struct {
	size, modTimeNs int64
	hash            string
}

// NewStateManager creates a new state manager
func NewStateManager(store *storage.DuckDBStore) *StateManager {
	return &StateManager{store: store, hashes: make(map[string]hashedFile)}
}

// fileHash returns the hash of the file at path, hashing it only if it
// changed since it was last hashed
func (m *StateManager) fileHash(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	size, modTime := info.Size(), info.ModTime().UnixNano()
	if h, ok := m.hashes[path]; ok && h.size == size && h.modTimeNs == modTime {
		return h.hash, nil
	}
	hash, err := computeFileHash(path)
	if err != nil {
		return "", err
	}
	m.hashes[path] = hashedFile{size: size, modTimeNs: modTime, hash: hash}
	return hash, nil
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
	status, _, err := m.fileStatus(ctx, source, filePath)
	return status, err
}

// fileStatus determines if a file needs to be imported and returns its stored
// import state (nil for a new file)
func (m *StateManager) fileStatus(ctx context.Context, source SourceType, filePath string) (FileStatus, *storage.ImportState, error) {
	// Get stored state
	state, err := m.store.GetImportState(ctx, string(source), filePath)
	if err != nil {
		return "", nil, fmt.Errorf("getting import state: %w", err)
	}

	// File has never been imported; no need to read it just to hash it
	if state == nil {
		return StatusNew, nil, nil
	}

	// An import of a date range left out records of the file
	if state.Partial {
		return StatusModified, state, nil
	}

	// Same size and modification time as when it was imported: unchanged,
	// without reading the whole file to hash it
	if state.FileSize > 0 {
		if info, err := os.Stat(filePath); err == nil &&
			info.Size() == state.FileSize && info.ModTime().UnixNano() == state.FileModTimeNs {
			return StatusCurrent, state, nil
		}
	}

	// Get current file hash
	currentHash, err := m.fileHash(filePath)
	if err != nil {
		return "", nil, fmt.Errorf("computing file hash: %w", err)
	}

	// File has been modified
	if state.FileHash != currentHash {
		return StatusModified, state, nil
	}

	// File is current (already imported, no changes)
	return StatusCurrent, state, nil
}

// NewImportState builds the import state recorded for a fully imported file.
// fileHash should be the hash of the parsed content (ImportResult.FileHash);
// if it is empty, the file is hashed as it is now.
func (m *StateManager) NewImportState(source SourceType, filePath, fileHash string, recordCount int) (*storage.ImportState, error) {
	if fileHash == "" {
		hash, err := computeFileHash(filePath)
		if err != nil {
			return nil, fmt.Errorf("computing file hash: %w", err)
		}
		fileHash = hash
	}

	return &storage.ImportState{
		Source:      string(source),
		FilePath:    filePath,
		FileHash:    fileHash,
		ImportedAt:  time.Now(),
		RecordCount: recordCount,
	}, nil
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
