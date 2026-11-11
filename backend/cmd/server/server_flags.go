package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/logger"
)

// initLogging sets up the logger of a command from AI_OBSERVER_LOG_LEVEL and
// AI_OBSERVER_LOG_FORMAT (text or json; an invalid format is reported and
// replaced by text)
func initLogging() {
	level, format, err := config.LogSettings()
	logger.Init(parseLogLevel(level), format)
	if err != nil {
		logger.Warn("Ignoring invalid setting, using the default", "error", err, "level", level, "format", format)
	}
}

// serverFlags are the listen and database options of serve and watch. Flags
// that are given override the environment (AI_OBSERVER_HOST,
// AI_OBSERVER_API_PORT, AI_OBSERVER_OTLP_PORT, AI_OBSERVER_OTLP_GRPC_PORT,
// AI_OBSERVER_DATABASE_PATH).
type serverFlags struct {
	Host     string
	APIPort  portFlag
	OTLPPort portFlag
	// OTLPGRPCPort is --otlp-grpc-port, which also takes 0 or off
	OTLPGRPCPort optionalPortFlag
	DB           string
}

// register defines the flags on fs; otlp adds --otlp-port
func (f *serverFlags) register(fs *flag.FlagSet, otlp bool) {
	fs.Var(valueFlag{&f.Host, "host"}, "host", "Interface to listen on, 0.0.0.0 for all (default: AI_OBSERVER_HOST or 127.0.0.1)")
	fs.Var(&f.APIPort, "api-port", "API and dashboard port (default: AI_OBSERVER_API_PORT or 8080)")
	if otlp {
		fs.Var(&f.OTLPPort, "otlp-port", "OTLP/HTTP ingestion port (default: AI_OBSERVER_OTLP_PORT or 4318)")
		fs.Var(&f.OTLPGRPCPort, "otlp-grpc-port", "OTLP/gRPC ingestion port, 0 or off to turn it off (default: AI_OBSERVER_OTLP_GRPC_PORT or 4317)")
	}
	registerDBFlag(fs, &f.DB)
}

// apply overrides cfg with the flags that were given
func (f *serverFlags) apply(cfg *config.Config) {
	if f.Host != "" {
		cfg.Host = f.Host
	}
	if f.APIPort != 0 {
		cfg.APIPort = int(f.APIPort)
	}
	if f.OTLPPort != 0 {
		cfg.OTLPPort = int(f.OTLPPort)
	}
	if f.OTLPGRPCPort.set {
		cfg.OTLPGRPCPort = f.OTLPGRPCPort.port
	}
	if f.DB != "" {
		cfg.DatabasePath = f.DB
	}
}

// registerDBFlag defines --db, the database file, on fs
func registerDBFlag(fs *flag.FlagSet, db *string) {
	fs.Var(valueFlag{db, "file"}, "db", "DuckDB database file (default: AI_OBSERVER_DATABASE_PATH or ./data/ai-observer.duckdb)")
}

// portFlag is a TCP port given as a flag; 0 when not given
type portFlag int

func (p *portFlag) String() string {
	if p == nil || *p == 0 {
		return ""
	}
	return strconv.Itoa(int(*p))
}

func (p *portFlag) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("expected a port from 1 to 65535")
	}
	*p = portFlag(n)
	return nil
}

// TypeName names the value in the help text (printFlags)
func (p *portFlag) TypeName() string { return "port" }

// optionalPortFlag is a TCP port given as a flag that can also be turned off
// with 0 or off; set tells whether it was given
type optionalPortFlag struct {
	port int
	set  bool
}

func (p *optionalPortFlag) String() string {
	if p == nil || !p.set {
		return ""
	}
	if p.port == 0 {
		return "off"
	}
	return strconv.Itoa(p.port)
}

func (p *optionalPortFlag) Set(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "off":
		*p = optionalPortFlag{set: true}
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("expected a port from 1 to 65535, or 0 or off")
	}
	*p = optionalPortFlag{port: n, set: true}
	return nil
}

// TypeName names the value in the help text (printFlags)
func (p *optionalPortFlag) TypeName() string { return "port" }
