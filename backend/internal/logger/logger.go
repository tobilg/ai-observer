package logger

import (
	"context"
	"log/slog"
	"os"
	"sync"
)

// Protect both lazy initialization and explicit reconfiguration. Callers keep
// the returned logger and emit records after the lock has been released.
var (
	defaultLoggerMu sync.Mutex
	defaultLogger   *slog.Logger
)

// Initialize sets up the global structured logger
func Initialize(level slog.Level) {
	defaultLoggerMu.Lock()
	defer defaultLoggerMu.Unlock()

	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// InitializeText sets up a text-based logger (better for development)
func InitializeText(level slog.Level) {
	defaultLoggerMu.Lock()
	defer defaultLoggerMu.Unlock()

	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Logger returns the default logger
func Logger() *slog.Logger {
	defaultLoggerMu.Lock()
	defer defaultLoggerMu.Unlock()

	if defaultLogger == nil {
		handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
		defaultLogger = slog.New(handler)
		slog.SetDefault(defaultLogger)
	}
	return defaultLogger
}

// With returns a logger with additional attributes
func With(args ...any) *slog.Logger {
	return Logger().With(args...)
}

// WithRequestID returns a logger with request ID attached
func WithRequestID(requestID string) *slog.Logger {
	return Logger().With("request_id", requestID)
}

// WithService returns a logger with service name attached
func WithService(serviceName string) *slog.Logger {
	return Logger().With("service", serviceName)
}

// Convenience methods that use the default logger

// Debug logs at debug level
func Debug(msg string, args ...any) {
	Logger().Debug(msg, args...)
}

// Info logs at info level
func Info(msg string, args ...any) {
	Logger().Info(msg, args...)
}

// Warn logs at warn level
func Warn(msg string, args ...any) {
	Logger().Warn(msg, args...)
}

// Error logs at error level
func Error(msg string, args ...any) {
	Logger().Error(msg, args...)
}

// DebugContext logs at debug level with context
func DebugContext(ctx context.Context, msg string, args ...any) {
	Logger().DebugContext(ctx, msg, args...)
}

// InfoContext logs at info level with context
func InfoContext(ctx context.Context, msg string, args ...any) {
	Logger().InfoContext(ctx, msg, args...)
}

// WarnContext logs at warn level with context
func WarnContext(ctx context.Context, msg string, args ...any) {
	Logger().WarnContext(ctx, msg, args...)
}

// ErrorContext logs at error level with context
func ErrorContext(ctx context.Context, msg string, args ...any) {
	Logger().ErrorContext(ctx, msg, args...)
}
