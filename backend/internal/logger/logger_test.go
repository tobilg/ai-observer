package logger

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// These tests run serially and join all workers before restoring global state.
func resetLogger(t *testing.T) {
	t.Helper()
	defaultLoggerMu.Lock()
	previous, previousSlog := defaultLogger, slog.Default()
	defaultLogger = nil
	defaultLoggerMu.Unlock()
	t.Cleanup(func() {
		defaultLoggerMu.Lock()
		defer defaultLoggerMu.Unlock()
		defaultLogger = previous
		slog.SetDefault(previousSlog)
	})
}

func TestLoggerConcurrentFirstUse(t *testing.T) {
	resetLogger(t)
	const workers = 64
	start := make(chan struct{})
	results := make(chan *slog.Logger, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			log := Logger()
			log.Debug("suppressed first-use message")
			results <- log
		})
	}
	close(start)
	wg.Wait()
	close(results)
	current := Logger()
	for log := range results {
		if log != current {
			t.Error("concurrent first use returned different default loggers")
		}
	}
	if current != slog.Default() {
		t.Error("package logger and slog default differ")
	}
}

func TestLoggerConcurrentReconfiguration(t *testing.T) {
	resetLogger(t)
	Initialize(slog.LevelWarn)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Go(func() {
			<-start
			for range 100 {
				switch worker % 4 {
				case 0:
					Initialize(slog.LevelWarn)
				case 1:
					InitializeText(slog.LevelError)
				case 2:
					DebugContext(context.Background(), "suppressed concurrent message")
				default:
					WithRequestID("test").Debug("suppressed child logger message")
				}
			}
		})
	}
	close(start)
	wg.Wait()
	if Logger() != slog.Default() {
		t.Error("reconfiguration left package logger and slog default inconsistent")
	}
}

func TestLoggerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func()
		text      bool
		level     slog.Level
	}{
		{"default", func() {}, false, slog.LevelInfo},
		{"json_before_first_use", func() { Initialize(slog.LevelWarn) }, false, slog.LevelWarn},
		{"text_before_first_use", func() { InitializeText(slog.LevelDebug) }, true, slog.LevelDebug},
		{"reconfigure_after_first_use", func() { Logger(); InitializeText(slog.LevelError) }, true, slog.LevelError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetLogger(t)
			tc.configure()
			log := Logger()
			_, isText := log.Handler().(*slog.TextHandler)
			_, isJSON := log.Handler().(*slog.JSONHandler)
			if isText != tc.text || isJSON == tc.text {
				t.Fatalf("unexpected handler %T", log.Handler())
			}
			ctx := context.Background()
			if !log.Enabled(ctx, tc.level) || log.Enabled(ctx, tc.level-1) {
				t.Error("configured log threshold was not preserved")
			}
			if log != slog.Default() {
				t.Error("package logger and slog default differ")
			}
		})
	}
}
