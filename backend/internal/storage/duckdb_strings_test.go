package storage

import (
	"bytes"
	"strings"
	"testing"
)

// DuckDB stores up to 12 bytes inline and longer values through a pointer.
// Exercise both paths with -race/checkptr enabled: duckdb-go v2.10503.1 could
// read the latter pointer from a misaligned Go stack copy and abort the process.
// Upstream fix: duckdb/duckdb-go@d5b9df8f289bda95d840a8c5fc186541a132fa86.
func TestDuckDBStringAndBlobRoundTrip(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	if _, err := store.DB().Exec(`CREATE TABLE string_boundary (id INTEGER, text_value VARCHAR, blob_value BLOB)`); err != nil {
		t.Fatal(err)
	}

	lengths := []int{0, 1, 11, 12, 13, 64, 4096}
	texts := make([]string, len(lengths))
	blobs := make([][]byte, len(lengths))
	for i, length := range lengths {
		texts[i] = strings.Repeat("x", length)
		blobs[i] = make([]byte, length)
		for j := range blobs[i] {
			blobs[i][j] = byte(j * 37) // Include NUL and non-UTF-8 bytes.
		}
		if _, err := store.DB().Exec(`INSERT INTO string_boundary VALUES (?, ?, ?)`, i, texts[i], blobs[i]); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := store.DB().Query(`SELECT text_value, blob_value FROM string_boundary ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var gotTexts []string
	var gotBlobs [][]byte
	for rows.Next() {
		var text string
		var blob []byte
		if err := rows.Scan(&text, &blob); err != nil {
			t.Fatal(err)
		}
		gotTexts = append(gotTexts, text)
		gotBlobs = append(gotBlobs, blob)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(gotTexts) != len(lengths) {
		t.Fatalf("read %d rows, want %d", len(gotTexts), len(lengths))
	}
	// Values must also remain valid after the driver's result buffers are freed.
	for i, length := range lengths {
		if gotTexts[i] != texts[i] || !bytes.Equal(gotBlobs[i], blobs[i]) {
			t.Errorf("VARCHAR/BLOB round trip corrupted a %d-byte value", length)
		}
	}
}
