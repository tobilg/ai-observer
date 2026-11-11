package importer

import (
	"context"
	"errors"

	"github.com/tobilg/ai-observer/internal/storage"
)

// Codex sessions reach several hundred MB (one of 512 MB holds 147,000
// records with 262 MB of text). Their records are parsed and stored in
// batches, so the whole file's records are never in memory at once; the
// batches, the replacement of the file's earlier records and its import state
// are still written in one transaction.

// batchFileSize is the content size above which a file is read in batches (a
// variable for tests)
var batchFileSize int64 = 64 << 20

// importBatchRecords is the number of records per batch (a variable for
// tests)
var importBatchRecords = 10_000

// batchParser is a SessionParser that can hand a file's records over in
// batches (CodexParser.ParseFileInBatches)
type batchParser interface {
	ParseFileInBatches(ctx context.Context, path string, batchSize int, emit func(*ImportResult) error) (*ImportResult, error)
}

// batchable returns the batch parser of a file of the given content size that
// is read in batches: a large file imported without a date range, since a
// range needs all of the file's records to tell what it leaves out
func batchable(parser SessionParser, opts Options, size int64) (batchParser, bool) {
	bp, ok := parser.(batchParser)
	return bp, ok && opts.FromDate == nil && opts.ToDate == nil && size > batchFileSize
}

// parseForPreview parses a file for the import preview, which needs only the
// numbers of its records: a large file is counted batch by batch
func parseForPreview(ctx context.Context, parser SessionParser, path string, opts Options) (*ImportResult, error) {
	info, err := StatSessionFile(path)
	if err != nil {
		return nil, err
	}
	bp, ok := batchable(parser, opts, info.Size)
	if !ok {
		return parser.ParseFile(ctx, path)
	}
	var logs, metrics, spans int
	result, err := bp.ParseFileInBatches(ctx, path, importBatchRecords, func(b *ImportResult) error {
		logs += len(b.Logs)
		metrics += len(b.Metrics)
		spans += len(b.Spans)
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.batched = &recordCounts{logs: logs, metrics: metrics, spans: spans}
	return result, nil
}

// errNoRecords rolls back a batched file that had no records to store
var errNoRecords = errors.New("no records")

// importInBatches parses and stores a large file batch by batch in one
// transaction (dst's own, or the one dst is). It returns the file's result
// (without records) and the numbers of records stored; none when the file
// had none, in which case nothing changed.
func (i *Importer) importInBatches(ctx context.Context, bp batchParser, source SourceType, filePath string, info SessionFileInfo, prev *storage.ImportState, dst fileWriter) (*ImportResult, recordCounts, error) {
	var stored recordCounts
	var result *ImportResult
	run := func(w fileWriter) error {
		stored = recordCounts{}
		// The batches are stored under the file's path; the import state
		// written with them is replaced by the final one below
		state := &storage.ImportState{Source: string(source), FilePath: filePath}
		var replace *storage.FileRecords
		if prev != nil {
			replace = &storage.FileRecords{Legacy: !prev.SourceTracked}
		}
		var err error
		result, err = bp.ParseFileInBatches(ctx, filePath, importBatchRecords, func(b *ImportResult) error {
			stored.logs += len(b.Logs)
			stored.metrics += len(b.Metrics)
			stored.spans += len(b.Spans)
			err := w.InsertImportedFile(ctx, b.Logs, b.Metrics, b.Spans, state, replace)
			replace = nil
			return err
		})
		if err != nil {
			return err
		}
		if stored.logs+stored.metrics+stored.spans == 0 {
			return errNoRecords
		}
		final, err := i.state.NewImportState(source, filePath, result.FileHash, result.RecordCount)
		if err != nil {
			return err
		}
		// As importSource records a file read without a date range
		final.ByteOffset = result.ParsedBytes
		final.MessageCount = result.MessageCount
		final.FileSize = info.Size
		final.FileModTimeNs = info.ModTimeNs
		final.ParserState = result.ParserState
		final.SourceTracked = true
		return w.InsertImportedFile(ctx, nil, nil, nil, final, nil)
	}

	var err error
	if store, ok := dst.(interface {
		InImportTx(context.Context, func(*storage.ImportTx) error) error
	}); ok {
		err = store.InImportTx(ctx, func(tx *storage.ImportTx) error { return run(tx) })
	} else {
		err = run(dst)
	}
	if errors.Is(err, errNoRecords) {
		return result, recordCounts{}, nil
	}
	return result, stored, err
}

// recordCounts are the numbers of a file's records
type recordCounts struct {
	logs, metrics, spans int
}
