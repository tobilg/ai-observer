package watcher

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
)

// replayRepositoryPrefix recovers context from old watcher state or a file
// skipped without backfill or after full import. It reads only the committed prefix and
// emits no records. SectionReader leaves the incremental file position intact.
func replayRepositoryPrefix(ctx context.Context, file *os.File, offset int64, observe func([]byte)) error {
	reader := bufio.NewReader(io.NewSectionReader(file, 0, offset))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("recovering repository context: %w", err)
		}
		if len(line) > 0 {
			observe(line)
		}
		if err == io.EOF {
			return nil
		}
	}
}
