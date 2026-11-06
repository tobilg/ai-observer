package server

import (
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/redact"
)

// NewRedactor returns the redactor for AI_OBSERVER_REDACT and logs what it
// redacts. serve, watch and import store telemetry through it.
func NewRedactor(mode redact.Mode) *redact.Redactor {
	switch mode {
	case redact.Off:
		logger.Info("Redaction off: telemetry is stored as received (AI_OBSERVER_REDACT=off)")
	case redact.All:
		logger.Info("Redacting secrets and personal data in stored telemetry (AI_OBSERVER_REDACT=all)")
	default:
		logger.Debug("Redacting secrets in stored telemetry (AI_OBSERVER_REDACT=secrets)")
	}
	return redact.New(mode)
}
