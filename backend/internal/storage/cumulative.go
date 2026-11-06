package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// deltaAliases maps the ".delta" series that earlier versions derived from
// cumulative counters at ingestion to those counters. Metric series turn
// cumulative points into per-interval increases themselves, so the aliases are
// answered from the counters, for stored data of any age.
var deltaAliases = map[string]string{
	"gemini_cli.token.usage.delta":          "gemini_cli.token.usage",
	"gemini_cli.api.request.count.delta":    "gemini_cli.api.request.count",
	"gemini_cli.file.operation.count.delta": "gemini_cli.file.operation.count",
	"gemini_cli.session.count.delta":        "gemini_cli.session.count",
}

// seriesSourceMetric returns the stored metric a series query reads
func seriesSourceMetric(name string) string {
	if source, ok := deltaAliases[name]; ok {
		return source
	}
	return name
}

// rebuildGeminiCost recomputes all stored Gemini cost points from the token
// points (migration 5): cost of the uncached input only, since Gemini's input
// tokens include the cached ones. OTLP costs are rebuilt like ingestion
// derives them (otlp.DeriveGeminiCostMetrics). Imported costs are one point
// per response, rebuilt from the response's token points (same timestamp,
// model and source file); imported costs without token points are kept.
func rebuildGeminiCost(ctx context.Context, w *writeTx) error {
	if err := rebuildGeminiOTLPCost(ctx, w); err != nil {
		return err
	}
	return rebuildGeminiImportedCost(ctx, w)
}

const geminiImported = `json_extract_string(Attributes, '$.import_source') IS NOT NULL`

func rebuildGeminiOTLPCost(ctx context.Context, w *writeTx) error {
	rows, err := w.tx.QueryContext(ctx, `
		SELECT Timestamp, ServiceName, ResourceAttributes, ScopeName, ScopeVersion,
			Attributes, Value, AggregationTemporality
		FROM otel_metrics
		WHERE MetricName = ? AND Value IS NOT NULL AND NOT `+geminiImported, otlp.GeminiTokenUsageMetric)
	if err != nil {
		return fmt.Errorf("reading Gemini token usage: %w", err)
	}
	var tokens []api.MetricDataPoint
	for rows.Next() {
		var m api.MetricDataPoint
		var resourceAttrs, attrs any
		var scopeName, scopeVersion sql.NullString
		var temporality sql.NullInt32
		var value float64
		if err := rows.Scan(&m.Timestamp, &m.ServiceName, &resourceAttrs, &scopeName, &scopeVersion,
			&attrs, &value, &temporality); err != nil {
			rows.Close()
			return fmt.Errorf("scanning Gemini token usage: %w", err)
		}
		m.MetricName = otlp.GeminiTokenUsageMetric
		m.MetricType = "sum"
		m.ResourceAttributes = scanJSONToMap(resourceAttrs)
		m.Attributes = scanJSONToMap(attrs)
		m.ScopeName = scopeName.String
		m.ScopeVersion = scopeVersion.String
		m.Value = &value
		if temporality.Valid {
			m.AggregationTemporality = &temporality.Int32
		}
		tokens = append(tokens, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reading Gemini token usage: %w", err)
	}
	rows.Close()

	if _, err := w.tx.ExecContext(ctx, `DELETE FROM otel_metrics WHERE MetricName = ? AND NOT `+geminiImported,
		otlp.GeminiCostUsageMetric); err != nil {
		return fmt.Errorf("deleting Gemini cost: %w", err)
	}
	return insertMetricsTx(ctx, w, otlp.DeriveGeminiCostMetrics(tokens), "")
}

// geminiResponseKey identifies the imported points of one Gemini response
type geminiResponseKey struct {
	ts         time.Time
	service    string
	model      string
	sourceFile string
}

func rebuildGeminiImportedCost(ctx context.Context, w *writeTx) error {
	rows, err := w.tx.QueryContext(ctx, `
		SELECT Timestamp, ServiceName, COALESCE(json_extract_string(Attributes, '$.model'), ''),
			COALESCE(json_extract_string(Attributes, '$.type'), ''), COALESCE(SourceFile, ''), Value
		FROM otel_metrics
		WHERE MetricName = ? AND Value IS NOT NULL AND `+geminiImported, otlp.GeminiTokenUsageMetric)
	if err != nil {
		return fmt.Errorf("reading imported Gemini token usage: %w", err)
	}
	type usage struct{ input, cached, output, thought int64 }
	responses := make(map[geminiResponseKey]*usage)
	for rows.Next() {
		var key geminiResponseKey
		var tokenType string
		var value float64
		if err := rows.Scan(&key.ts, &key.service, &key.model, &tokenType, &key.sourceFile, &value); err != nil {
			rows.Close()
			return fmt.Errorf("scanning imported Gemini token usage: %w", err)
		}
		u := responses[key]
		if u == nil {
			u = &usage{}
			responses[key] = u
		}
		switch tokenType {
		case pricing.GeminiTokenTypeInput:
			u.input += int64(value)
		case pricing.GeminiTokenTypeCache:
			u.cached += int64(value)
		case pricing.GeminiTokenTypeOutput:
			u.output += int64(value)
		case pricing.GeminiTokenTypeThought:
			u.thought += int64(value)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reading imported Gemini token usage: %w", err)
	}
	rows.Close()

	// The cost points of the responses that have token points are replaced
	costRows, err := w.tx.QueryContext(ctx, `
		SELECT rowid, Timestamp, ServiceName, COALESCE(json_extract_string(Attributes, '$.model'), ''), COALESCE(SourceFile, '')
		FROM otel_metrics
		WHERE MetricName = ? AND `+geminiImported, otlp.GeminiCostUsageMetric)
	if err != nil {
		return fmt.Errorf("reading imported Gemini cost: %w", err)
	}
	var replaced []any
	for costRows.Next() {
		var rowid int64
		var key geminiResponseKey
		if err := costRows.Scan(&rowid, &key.ts, &key.service, &key.model, &key.sourceFile); err != nil {
			costRows.Close()
			return fmt.Errorf("scanning imported Gemini cost: %w", err)
		}
		if responses[key] != nil {
			replaced = append(replaced, rowid)
		}
	}
	if err := costRows.Err(); err != nil {
		costRows.Close()
		return fmt.Errorf("reading imported Gemini cost: %w", err)
	}
	costRows.Close()
	for start := 0; start < len(replaced); start += 1000 {
		batch := replaced[start:min(start+1000, len(replaced))]
		if _, err := w.tx.ExecContext(ctx, "DELETE FROM otel_metrics WHERE rowid IN (?"+
			strings.Repeat(", ?", len(batch)-1)+")", batch...); err != nil {
			return fmt.Errorf("deleting imported Gemini cost: %w", err)
		}
	}

	byFile := make(map[string][]api.MetricDataPoint)
	for key, u := range responses {
		cost := pricing.CalculateGeminiCost(key.model, u.input, u.cached, u.output, u.thought)
		if cost == nil || *cost <= 0 {
			continue
		}
		byFile[key.sourceFile] = append(byFile[key.sourceFile], api.MetricDataPoint{
			Timestamp:   key.ts,
			ServiceName: key.service,
			MetricName:  otlp.GeminiCostUsageMetric,
			MetricType:  "sum",
			Value:       cost,
			Attributes:  map[string]string{"model": key.model, "import_source": "local_jsonl"},
		})
	}
	for sourceFile, costs := range byFile {
		if err := insertMetricsTx(ctx, w, costs, sourceFile); err != nil {
			return err
		}
	}
	return nil
}
