// Package diskspace tells how much disk space is available, so that
// copies of the database (backups, compaction) can fail before they start
// rather than half-way, with a full disk
package diskspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// Available returns the bytes available to this process on the file
// system of dir. A dir that does not exist yet is measured at its nearest
// existing parent.
func Available(dir string) (uint64, error) {
	dir, err := existingDir(dir)
	if err != nil {
		return 0, err
	}
	return available(dir)
}

// existingDir returns dir or its nearest existing parent
func existingDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", dir, err)
	}
	for {
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no existing directory for %s", dir)
		}
		abs = parent
	}
}

// Error explains that an operation needs more space than is available
type Error struct {
	// What is being written, such as "the backup"
	What      string
	Dir       string
	Needed    uint64
	Available uint64
}

func (e *Error) Error() string {
	return fmt.Sprintf("not enough disk space for %s in %s: needs about %s, %s available",
		e.What, e.Dir, FormatBytes(int64(e.Needed)), FormatBytes(int64(e.Available)))
}

// Check returns an *Error when the file system of dir has less than needed
// bytes available for what
func Check(dir string, needed uint64, what string) error {
	avail, err := Available(dir)
	if err != nil {
		return fmt.Errorf("checking free disk space for %s: %w", what, err)
	}
	if avail < needed {
		return &Error{What: what, Dir: dir, Needed: needed, Available: avail}
	}
	return nil
}

// FormatBytes formats a byte count for messages: "512 B", "1.5 MB" (powers
// of 1024, like the dashboard)
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FileSize returns the size of the file at path, 0 when there is none
func FileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
