package importer

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Codex compresses session files whose last change is older than 7 days
// (codex-rs rollout/src/compression.rs): rollout-….jsonl becomes
// rollout-….jsonl.zst, one zstd frame with the content size in its header and
// the modification time of the .jsonl. Resuming the session writes the .jsonl
// again (same content and time) and appends to it. Both forms are one session
// file, identified by its plain .jsonl path everywhere (import state,
// SourceFile, watch), and its content, size and hash are those of the JSONL.

// compressedSuffix is the suffix Codex adds to a compressed session file
const compressedSuffix = ".zst"

// CompressedSessionFile returns the plain path of a compressed session file
// (x.jsonl for x.jsonl.zst), and false for other paths
func CompressedSessionFile(path string) (string, bool) {
	plain, ok := strings.CutSuffix(path, compressedSuffix)
	if !ok || !strings.HasSuffix(plain, ".jsonl") {
		return "", false
	}
	return plain, true
}

// SessionFileInfo describes a session file by its content: the size of the
// JSONL and the modification time of the file holding it
type SessionFileInfo struct {
	// Size is the size of the JSONL content; -1 when a compressed file does
	// not record it, so only its hash can tell whether it changed
	Size       int64
	ModTimeNs  int64
	Compressed bool
}

// StatSessionFile describes the session file at path. When path does not
// exist and its compressed form does, that is described, with the content size
// from its frame header (not decompressed).
func StatSessionFile(path string) (SessionFileInfo, error) {
	info, err := os.Stat(path)
	if err == nil {
		return SessionFileInfo{Size: info.Size(), ModTimeNs: info.ModTime().UnixNano()}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return SessionFileInfo{}, err
	}
	file, zinfo, zerr := openCompressed(path)
	if zerr != nil {
		return SessionFileInfo{}, err
	}
	defer file.Close()
	size := int64(-1)
	var header zstd.Header
	buf := make([]byte, zstd.HeaderMaxSize)
	if n, _ := io.ReadFull(file, buf); header.Decode(buf[:n]) == nil && header.HasFCS {
		size = int64(header.FrameContentSize)
	}
	return SessionFileInfo{Size: size, ModTimeNs: zinfo.ModTime().UnixNano(), Compressed: true}, nil
}

// SessionFileSize returns the content size of the session file at path
// (StatSessionFile), or -1 if it cannot be read or is not known
func SessionFileSize(path string) int64 {
	info, err := StatSessionFile(path)
	if err != nil {
		return -1
	}
	return info.Size
}

// OpenSessionFile opens the session file at path for reading its content:
// the file itself, or, when it does not exist, its compressed form through a
// streaming decoder (the content is never held in memory as a whole). The
// returned bool tells whether it is compressed; a compressed file cannot seek.
func OpenSessionFile(path string) (io.ReadCloser, bool, error) {
	file, err := os.Open(path)
	if err == nil {
		return file, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, false, err
	}
	zfile, _, zerr := openCompressed(path)
	if zerr != nil {
		return nil, false, err
	}
	decoder, derr := zstd.NewReader(zfile,
		// One goroutine and small buffers: the content is read once, in order
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true))
	if derr != nil {
		zfile.Close()
		return nil, false, fmt.Errorf("reading %s: %w", path+compressedSuffix, derr)
	}
	return &decodedFile{Decoder: decoder, file: zfile, path: path + compressedSuffix}, true, nil
}

// openCompressed opens the compressed form of the session file at path
func openCompressed(path string) (*os.File, os.FileInfo, error) {
	if !strings.HasSuffix(path, ".jsonl") {
		return nil, nil, fs.ErrNotExist
	}
	file, err := os.Open(path + compressedSuffix)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

// decodedFile reads a compressed session file's content
type decodedFile struct {
	*zstd.Decoder
	file *os.File
	path string
}

func (d *decodedFile) Read(p []byte) (int, error) {
	n, err := d.Decoder.Read(p)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("decompressing %s: %w", d.path, err)
	}
	return n, err
}

func (d *decodedFile) Close() error {
	d.Decoder.Close()
	return d.file.Close()
}
