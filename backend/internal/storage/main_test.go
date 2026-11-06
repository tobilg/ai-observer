package storage

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	closeBenchDB()
	os.Exit(code)
}
