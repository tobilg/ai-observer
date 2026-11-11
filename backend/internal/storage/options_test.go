package storage

import (
	"strings"
	"testing"
)

func TestStoreOptionsSetDatabaseLimits(t *testing.T) {
	store, err := NewDuckDBStoreWithOptions(":memory:", Options{MemoryLimit: "512MiB", Threads: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var memory string
	var threads int
	err = store.DB().QueryRow("SELECT current_setting('memory_limit'), current_setting('threads')::INT").Scan(&memory, &threads)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(memory, "512") || threads != 2 {
		t.Errorf("memory_limit = %q, threads = %d; want 512 MiB and 2", memory, threads)
	}
}

func TestStoreOptionsRejectInvalidMemoryLimit(t *testing.T) {
	for _, limit := range []string{"2", "1GB'; DROP TABLE otel_logs; --", "80%"} {
		if store, err := NewDuckDBStoreWithOptions(":memory:", Options{MemoryLimit: limit}); err == nil {
			store.Close()
			t.Errorf("memory limit %q was accepted", limit)
		}
	}
}
