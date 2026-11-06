package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/redact"
)

type Config struct {
	// Network interface both servers listen on. Defaults to loopback so the
	// unauthenticated API and OTLP endpoints are not reachable from the network.
	Host string

	// Server ports
	OTLPPort int
	APIPort  int

	// Database
	DatabasePath string

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
}

// Load reads the configuration from the environment. Invalid numbers are
// logged as warnings and replaced by their defaults, so a typo such as
// AI_OBSERVER_RETENTION_DAYS=30d does not go unnoticed.
func Load() *Config {
	return &Config{
		Host:          getEnv("AI_OBSERVER_HOST", "127.0.0.1"),
		OTLPPort:      getEnvInt("AI_OBSERVER_OTLP_PORT", 4318, 1, 65535),
		APIPort:       getEnvInt("AI_OBSERVER_API_PORT", 8080, 1, 65535),
		DatabasePath:  getEnv("AI_OBSERVER_DATABASE_PATH", "./data/ai-observer.duckdb"),
		FrontendURL:   getEnv("AI_OBSERVER_FRONTEND_URL", "http://localhost:5173"),
		AllowedHosts:  getEnvList("AI_OBSERVER_ALLOWED_HOSTS"),
		RetentionDays: getEnvInt("AI_OBSERVER_RETENTION_DAYS", 0, 0, maxRetentionDays),
		AuthToken:     strings.TrimSpace(os.Getenv("AI_OBSERVER_AUTH_TOKEN")),
		MCPEnabled:    getEnvBool("AI_OBSERVER_MCP", false),
		Redaction:     getRedactionMode(),
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

// maxRetentionDays bounds AI_OBSERVER_RETENTION_DAYS (100 years), so the
// cutoff time cannot overflow
const maxRetentionDays = 36500

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
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
