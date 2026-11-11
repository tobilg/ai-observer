package diskspace

import (
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestAvailableMeasuresExistingParent(t *testing.T) {
	dir := t.TempDir()
	avail, err := Available(filepath.Join(dir, "not", "yet"))
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if avail == 0 {
		t.Fatal("no space available in the temp directory")
	}
}

func TestCheckReportsShortage(t *testing.T) {
	dir := t.TempDir()
	if err := Check(dir, 1, "the test"); err != nil {
		t.Fatalf("Check(1 byte): %v", err)
	}
	err := Check(dir, math.MaxUint64, "the backup")
	var short *Error
	if !errors.As(err, &short) {
		t.Fatalf("Check(max) = %v, want *Error", err)
	}
	if !strings.Contains(err.Error(), "not enough disk space for the backup") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestFormatBytes(t *testing.T) {
	for in, want := range map[int64]string{
		0:                 "0 B",
		500:               "500 B",
		1023:              "1023 B",
		1024:              "1.0 KB",
		1536:              "1.5 KB",
		1048576:           "1.0 MB",
		750 * 1024 * 1024: "750.0 MB",
		1073741824:        "1.0 GB",
		1099511627776:     "1.0 TB",
	} {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
