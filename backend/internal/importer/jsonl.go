package importer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"iter"
)

// jsonlLines iterates over the non-empty, whitespace-trimmed lines of r.
//
// Unlike bufio.Scanner it has no maximum line length: session files contain
// multi-megabyte lines (large tool outputs, inline images), and a length limit
// would make the whole file fail to parse. A read error is yielded once with a
// nil line, after which iteration stops.
func jsonlLines(r io.Reader) iter.Seq2[[]byte, error] {
	return jsonlLinesCounted(r, nil)
}

// jsonlLinesCounted is jsonlLines that also sets *complete, if not nil, to the
// number of bytes in the complete lines read so far: newline-terminated ones,
// and a final line without newline that is valid JSON. Such a line has been
// imported, so watch must not read it again; an invalid one may still be
// being written. Watch treats the end of a file the same way.
func jsonlLinesCounted(r io.Reader, complete *int64) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		reader := bufio.NewReaderSize(r, 64*1024)
		for {
			line, err := reader.ReadBytes('\n')
			trimmed := bytes.TrimSpace(line)
			if complete != nil && len(line) > 0 &&
				(line[len(line)-1] == '\n' || (err == io.EOF && json.Valid(trimmed))) {
				*complete += int64(len(line))
			}
			if len(trimmed) > 0 {
				if !yield(trimmed, nil) {
					return
				}
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				yield(nil, err)
				return
			}
		}
	}
}
