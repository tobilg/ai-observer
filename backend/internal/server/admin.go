package server

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/handlers"
	"github.com/tobilg/ai-observer/internal/logger"
)

// adminPathPrefix is the path of the admin API (AI_OBSERVER_ADMIN_API)
const adminPathPrefix = "/api/admin/"

// adminRoutes mounts the admin API under /api/admin, behind the API's
// Host, Origin and token checks. Its operations (deletion, export, backup,
// checkpoint) can take longer than the API's request timeout: they run
// without it (exceptPrefix) and end when the server shuts down. Disabled,
// every admin path answers 404 with how to enable it.
func (s *Server) adminRoutes(r chi.Router) {
	if !s.config.AdminAPI {
		r.HandleFunc("/*", handlers.AdminDisabled)
		return
	}
	backupDir := absPath(s.config.AdminBackupDir())
	admin := handlers.NewAdmin(s.storage, backupDir)
	r.Use(func(next http.Handler) http.Handler { return endOnStop(s.stopping, next) })
	r.Post("/delete", admin.Delete)
	r.Post("/export", admin.Export)
	r.Post("/backup", admin.Backup)
	r.Post("/checkpoint", admin.Checkpoint)
	logger.Info("Admin API enabled: maintenance endpoints under /api/admin delete data and write files",
		"backup_dir", backupDir)
}

// exceptPrefix applies middleware mw to every request but those whose path
// starts with prefix
func exceptPrefix(prefix string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}

// absPath returns path made absolute, or as it is when that fails
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
