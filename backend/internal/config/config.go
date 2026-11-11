package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
)

type Config struct {
	// Network interface both servers listen on. Defaults to loopback so the
	// unauthenticated API and OTLP endpoints are not reachable from the network.
	Host string

	// Server ports
	OTLPPort int
	APIPort  int

	// OTLPGRPCPort is the OTLP/gRPC port (AI_OBSERVER_OTLP_GRPC_PORT, default
	// 4317); 0 disables OTLP/gRPC ("0" or "off")
	OTLPGRPCPort int

	// Database
	DatabasePath string

	// DBMemoryLimit bounds the memory DuckDB uses (AI_OBSERVER_DB_MEMORY_LIMIT),
	// as a size such as "2GB"; empty keeps DuckDB's default, 80% of the
	// machine's memory
	DBMemoryLimit string

	// DBThreads is the number of threads DuckDB runs queries on
	// (AI_OBSERVER_DB_THREADS); 0 keeps DuckDB's default, one per CPU core
	DBThreads int

	// Frontend
	FrontendURL string

	// Additional host names accepted in the API's Host header and as browser
	// origins. IP addresses and localhost are always accepted; "*" disables
	// both checks.
	AllowedHosts []string

	// Telemetry older than this many days is deleted while the server runs;
	// 0 keeps everything
	RetentionDays int

	// AuthToken, when set, is required on OTLP and API requests (see
	// server.TokenAuth). Empty leaves both servers unauthenticated.
	AuthToken string

	// MCPEnabled serves the MCP endpoint (/mcp on the API port), which lets
	// agents read the stored sessions, logs, traces and metrics. Off by
	// default; AI_OBSERVER_MCP=true or the --mcp flag of serve and watch
	// turn it on.
	MCPEnabled bool

	// Redaction selects what is replaced in telemetry before it is stored
	// (AI_OBSERVER_REDACT): secrets by default, secrets and personal data,
	// or nothing
	Redaction redact.Mode

	// StoreAllSpans stores the spans that are otherwise left out as noise
	// (AI_OBSERVER_STORE_ALL_SPANS; tools.Provider.NoiseSpans): Codex's
	// spans for every streamed response event and session file write
	StoreAllSpans bool

	// TLSCert and TLSKey are PEM files of a certificate (chain) and its
	// private key (AI_OBSERVER_TLS_CERT, AI_OBSERVER_TLS_KEY). Set together,
	// the HTTP servers (API and OTLP/HTTP) speak HTTPS; empty, plain HTTP.
	// The server checks them at startup.
	TLSCert string
	TLSKey  string

	// MaxDBSize bounds the telemetry kept, in bytes of the database's used
	// blocks (AI_OBSERVER_MAX_DB_SIZE, such as 20GB): above it the server
	// deletes the oldest days. 0 keeps everything. The file keeps its size;
	// deleted data frees blocks inside it, which new data reuses.
	MaxDBSize int64

	// AdminAPI serves the maintenance endpoints /api/admin/* (delete,
	// export, backup, checkpoint) on the API port (AI_OBSERVER_ADMIN_API).
	// Off by default: they delete data and write files.
	AdminAPI bool

	// BackupDir is the directory the admin API writes backups and exports
	// to (AI_OBSERVER_BACKUP_DIR); empty means "backups" next to the
	// database (AdminBackupDir)
	BackupDir string

	// SkipMigrationBackup migrates a database without copying it first
	// (AI_OBSERVER_SKIP_MIGRATION_BACKUP)
	SkipMigrationBackup bool

	// LogLevel and LogFormat are the logging settings in effect
	// (AI_OBSERVER_LOG_LEVEL: DEBUG, INFO, WARN or ERROR; AI_OBSERVER_LOG_FORMAT:
	// text or json), for the status; commands set up logging with
	// LogSettings before they load the configuration
	LogLevel  string
	LogFormat string
}

// Load reads the configuration from the environment. Invalid numbers are
// logged as warnings and replaced by their defaults, so a typo such as
// AI_OBSERVER_RETENTION_DAYS=30d does not go unnoticed.
func Load() *Config {
	level, format, _ := LogSettings()
	return &Config{
		Host:          getHost("AI_OBSERVER_HOST", "127.0.0.1"),
		OTLPPort:      getEnvInt("AI_OBSERVER_OTLP_PORT", 4318, 1, 65535),
		OTLPGRPCPort:  getOptionalPort("AI_OBSERVER_OTLP_GRPC_PORT", 4317),
		APIPort:       getEnvInt("AI_OBSERVER_API_PORT", 8080, 1, 65535),
		DatabasePath:  getEnv("AI_OBSERVER_DATABASE_PATH", "./data/ai-observer.duckdb"),
		DBMemoryLimit: getMemoryLimit("AI_OBSERVER_DB_MEMORY_LIMIT"),
		DBThreads:     getEnvInt("AI_OBSERVER_DB_THREADS", 0, 1, maxDBThreads),
		FrontendURL:   getOrigin("AI_OBSERVER_FRONTEND_URL", "http://localhost:5173"),
		AllowedHosts:  getHostList("AI_OBSERVER_ALLOWED_HOSTS"),
		RetentionDays: getEnvInt("AI_OBSERVER_RETENTION_DAYS", 0, 0, maxRetentionDays),
		AuthToken:     strings.TrimSpace(os.Getenv("AI_OBSERVER_AUTH_TOKEN")),
		MCPEnabled:    getEnvBool("AI_OBSERVER_MCP", false),
		Redaction:     getRedactionMode(),
		StoreAllSpans: getEnvBool("AI_OBSERVER_STORE_ALL_SPANS", false),
		TLSCert:       strings.TrimSpace(os.Getenv("AI_OBSERVER_TLS_CERT")),
		TLSKey:        strings.TrimSpace(os.Getenv("AI_OBSERVER_TLS_KEY")),

		MaxDBSize:           getByteSize("AI_OBSERVER_MAX_DB_SIZE"),
		AdminAPI:            getEnvBool("AI_OBSERVER_ADMIN_API", false),
		BackupDir:           strings.TrimSpace(os.Getenv("AI_OBSERVER_BACKUP_DIR")),
		SkipMigrationBackup: getEnvBool("AI_OBSERVER_SKIP_MIGRATION_BACKUP", false),

		LogLevel:  level,
		LogFormat: format,
	}
}

// getEnvBool returns the boolean in key (true/false, 1/0, yes/no, on/off),
// or defaultValue when it is unset. Other values are logged and ignored.
func getEnvBool(key string, defaultValue bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "":
		return defaultValue
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		logger.Warn("Ignoring invalid setting, using the default", "key", key, "value", value, "default", defaultValue)
		return defaultValue
	}
}

// getRedactionMode reads AI_OBSERVER_REDACT. An unknown mode is logged and
// replaced by the default, which redacts secrets.
func getRedactionMode() redact.Mode {
	value := os.Getenv("AI_OBSERVER_REDACT")
	mode, err := redact.ParseMode(value)
	if err != nil {
		logger.Warn("Ignoring invalid setting, using the default",
			"key", "AI_OBSERVER_REDACT", "value", value, "default", string(redact.DefaultMode), "error", err)
	}
	return mode
}

// LogFormat reads AI_OBSERVER_LOG_FORMAT: "text" (default) or "json". It is
// read before the logger is set up, so an invalid value is returned as an
// error for the caller to log, together with the default.
func LogFormat() (string, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("AI_OBSERVER_LOG_FORMAT")))
	switch value {
	case "", logger.FormatText:
		return logger.FormatText, nil
	case logger.FormatJSON:
		return logger.FormatJSON, nil
	default:
		return logger.FormatText, fmt.Errorf("invalid AI_OBSERVER_LOG_FORMAT %q: expected text or json", value)
	}
}

// LogLevel reads AI_OBSERVER_LOG_LEVEL: DEBUG, INFO (default), WARN (or
// WARNING) or ERROR, in any case. Like LogFormat it returns an invalid
// value as an error, with the default.
func LogLevel() (string, error) {
	value := strings.ToUpper(strings.TrimSpace(os.Getenv("AI_OBSERVER_LOG_LEVEL")))
	switch value {
	case "":
		return "INFO", nil
	case "DEBUG", "INFO", "WARN", "ERROR":
		return value, nil
	case "WARNING":
		return "WARN", nil
	default:
		return "INFO", fmt.Errorf("invalid AI_OBSERVER_LOG_LEVEL %q: expected DEBUG, INFO, WARN or ERROR", value)
	}
}

// LogSettings returns the log level and format (LogLevel, LogFormat), with
// the errors of invalid values joined
func LogSettings() (level, format string, err error) {
	level, levelErr := LogLevel()
	format, formatErr := LogFormat()
	return level, format, errors.Join(levelErr, formatErr)
}

// maxDBThreads bounds AI_OBSERVER_DB_THREADS
const maxDBThreads = 1024

// StorageOptions returns the database settings of the configuration
func (c *Config) StorageOptions() storage.Options {
	return storage.Options{MemoryLimit: c.DBMemoryLimit, Threads: c.DBThreads, SkipMigrationBackup: c.SkipMigrationBackup}
}

// AdminBackupDir returns the directory the admin API writes backups and
// exports to: AI_OBSERVER_BACKUP_DIR, or "backups" next to the database
func (c *Config) AdminBackupDir() string {
	if c.BackupDir != "" {
		return c.BackupDir
	}
	return filepath.Join(filepath.Dir(c.DatabasePath), "backups")
}

// getByteSize returns the size in key, such as "20GB" or "512MiB", in
// bytes, or 0 when it is unset. Other values are logged and ignored.
func getByteSize(key string) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0
	}
	size, err := storage.ParseByteSize(value)
	if err != nil {
		logger.Warn("Ignoring invalid setting, using the default",
			"key", key, "value", value, "default", "off", "expected", "a size such as 20GB or 512MiB")
		return 0
	}
	return size
}

// getMemoryLimit returns the memory size in key, such as "2GB" or "512 MiB",
// or "" when it is unset. Other values are logged and ignored.
func getMemoryLimit(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return ""
	}
	if !storage.ValidMemoryLimit(value) {
		logger.Warn("Ignoring invalid setting, using the default",
			"key", key, "value", value, "default", "80% of memory", "expected", "a size such as 2GB or 512MB")
		return ""
	}
	return value
}

// maxRetentionDays bounds AI_OBSERVER_RETENTION_DAYS (100 years), so the
// cutoff time cannot overflow
const maxRetentionDays = 36500

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// hostName matches DNS host names (letters, digits, hyphens and dots)
var hostName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

// validHost reports whether value is an IP address or a host name, without
// scheme or port
func validHost(value string) bool {
	return net.ParseIP(value) != nil || hostName.MatchString(value)
}

// getHost returns the interface in key, an IP address or host name, or
// defaultValue when it is unset. Other values (a URL, host:port) are logged
// and ignored.
func getHost(key, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	if !validHost(value) {
		logger.Warn("Ignoring invalid setting, using the default",
			"key", key, "value", value, "default", defaultValue, "expected", "an IP address or host name, such as 0.0.0.0")
		return defaultValue
	}
	return value
}

// getOrigin returns the browser origin in key (scheme, host and optional
// port, such as http://localhost:5173), or defaultValue when it is unset.
// Other values are logged and ignored.
func getOrigin(key, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		logger.Warn("Ignoring invalid setting, using the default",
			"key", key, "value", value, "default", defaultValue, "expected", "an origin such as https://observer.example.com:8443")
		return defaultValue
	}
	return strings.TrimSuffix(value, "/")
}

// getHostList returns the host names in key, comma-separated; "*" stands
// for any. Entries that are no host name or IP address (URLs, host:port)
// are logged and left out.
func getHostList(key string) []string {
	var hosts []string
	for _, value := range getEnvList(key) {
		if value != "*" && !validHost(strings.TrimSuffix(strings.Trim(value, "[]"), ".")) {
			logger.Warn("Ignoring invalid host name", "key", key, "value", value, "expected", "a host name such as observer.example.com, or *")
			continue
		}
		hosts = append(hosts, value)
	}
	return hosts
}

// getEnvInt returns the integer in key, or defaultValue when it is unset. A
// value that is not an integer in [minValue, maxValue] is logged and ignored.
func getEnvInt(key string, defaultValue, minValue, maxValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil || intValue < minValue || intValue > maxValue {
		logger.Warn("Ignoring invalid setting, using the default",
			"key", key, "value", value, "default", defaultValue, "min", minValue, "max", maxValue)
		return defaultValue
	}
	return intValue
}

// getOptionalPort returns the port in key, defaultValue when it is unset, and
// 0 (disabled) for "0" or "off" (also "false", "no", "disabled"). Other
// values are logged and replaced by the default.
func getOptionalPort(key string, defaultValue int) int {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "0", "off", "false", "no", "disabled":
		return 0
	}
	return getEnvInt(key, defaultValue, 1, 65535)
}

// getEnvList splits a comma-separated environment variable, dropping empty entries
func getEnvList(key string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(key), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}
