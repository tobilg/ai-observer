package config

import (
	"path/filepath"
	"testing"
)

func TestLifecycleSettingsDefaults(t *testing.T) {
	for _, key := range []string{"AI_OBSERVER_MAX_DB_SIZE", "AI_OBSERVER_ADMIN_API", "AI_OBSERVER_BACKUP_DIR", "AI_OBSERVER_SKIP_MIGRATION_BACKUP"} {
		t.Setenv(key, "")
	}
	t.Setenv("AI_OBSERVER_DATABASE_PATH", filepath.Join("var", "db", "x.duckdb"))
	cfg := Load()
	if cfg.MaxDBSize != 0 || cfg.AdminAPI || cfg.SkipMigrationBackup || cfg.StorageOptions().SkipMigrationBackup {
		t.Errorf("defaults = %+v", cfg)
	}
	if got, want := cfg.AdminBackupDir(), filepath.Join("var", "db", "backups"); got != want {
		t.Errorf("AdminBackupDir = %q, want %q", got, want)
	}
}

func TestLifecycleSettings(t *testing.T) {
	t.Setenv("AI_OBSERVER_MAX_DB_SIZE", "20GB")
	t.Setenv("AI_OBSERVER_ADMIN_API", "true")
	t.Setenv("AI_OBSERVER_BACKUP_DIR", "/srv/backups")
	t.Setenv("AI_OBSERVER_SKIP_MIGRATION_BACKUP", "1")
	cfg := Load()
	if cfg.MaxDBSize != 20<<30 || !cfg.AdminAPI || !cfg.SkipMigrationBackup || !cfg.StorageOptions().SkipMigrationBackup {
		t.Errorf("settings = %+v", cfg)
	}
	if cfg.AdminBackupDir() != "/srv/backups" {
		t.Errorf("AdminBackupDir = %q", cfg.AdminBackupDir())
	}

	t.Setenv("AI_OBSERVER_MAX_DB_SIZE", "20 gigs")
	if got := Load().MaxDBSize; got != 0 {
		t.Errorf("invalid size gives %d, want 0 (off)", got)
	}
}
