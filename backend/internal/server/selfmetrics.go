package server

import (
	"github.com/tobilg/ai-observer/internal/selfmetrics"
	"github.com/tobilg/ai-observer/internal/storage"
)

// recordRetention counts the rows a retention run deleted
func recordRetention(deleted storage.DeleteSummary) {
	selfmetrics.RetentionDeletedRows.With("spans").Add(int(deleted.SpanCount))
	selfmetrics.RetentionDeletedRows.With("logs").Add(int(deleted.LogCount))
	selfmetrics.RetentionDeletedRows.With("metrics").Add(int(deleted.MetricCount))
}
