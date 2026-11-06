package pricing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Reading and writing pricing files (data/*.json) in their layout, so that
// automated updates change only the lines of changed prices

// EmbeddedPricingFile returns a built-in pricing file, such as "claude.json"
func EmbeddedPricingFile(name string) ([]byte, error) {
	return pricingFS.ReadFile("data/" + name)
}

// modelKeyOrder returns the keys of "models" in the order of the file
func modelKeyOrder(data []byte) ([]string, error) {
	var raw struct {
		Models json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Models) == 0 {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw.Models))
	if _, err := dec.Token(); err != nil { // {
		return nil, fmt.Errorf("reading model order: %w", err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("reading model order: %w", err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, fmt.Errorf("reading model order: %w", err)
		}
	}
	return keys, nil
}

func describePrices(e ModelEntry) string {
	s := fmt.Sprintf("$%s in / $%s out", formatMTok(e.InputCostPerMTok), formatMTok(e.OutputCostPerMTok))
	if e.LongContext != nil {
		s += fmt.Sprintf(", above %d tokens $%s / $%s", e.LongContext.ThresholdInputTokens,
			formatMTok(e.LongContext.InputCostPerMTok), formatMTok(e.LongContext.OutputCostPerMTok))
	}
	return s
}

// priceChanges describes the price fields that differ, or returns ""
func priceChanges(before, after ModelEntry) string {
	var changes []string
	field := func(name string, a, b float64) {
		if a != b {
			changes = append(changes, fmt.Sprintf("%s %s -> %s", name, formatMTok(a), formatMTok(b)))
		}
	}
	field("input", before.InputCostPerMTok, after.InputCostPerMTok)
	field("output", before.OutputCostPerMTok, after.OutputCostPerMTok)
	field("cache read", before.CacheReadCostPerMTok, after.CacheReadCostPerMTok)
	field("cache write", before.CacheWriteCostPerMTok, after.CacheWriteCostPerMTok)
	var lcBefore, lcAfter LongContextEntry
	if before.LongContext != nil {
		lcBefore = *before.LongContext
	}
	if after.LongContext != nil {
		lcAfter = *after.LongContext
	}
	if lcBefore.ThresholdInputTokens != lcAfter.ThresholdInputTokens {
		changes = append(changes, fmt.Sprintf("long-context threshold %d -> %d", lcBefore.ThresholdInputTokens, lcAfter.ThresholdInputTokens))
	}
	field("long-context input", lcBefore.InputCostPerMTok, lcAfter.InputCostPerMTok)
	field("long-context output", lcBefore.OutputCostPerMTok, lcAfter.OutputCostPerMTok)
	field("long-context cache read", lcBefore.CacheReadCostPerMTok, lcAfter.CacheReadCostPerMTok)
	field("long-context cache write", lcBefore.CacheWriteCostPerMTok, lcAfter.CacheWriteCostPerMTok)
	return strings.Join(changes, ", ")
}

func formatMTok(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// FormatPricingFile writes a pricing file in the layout of the built-in files
// (aliases on one line, fields in schema order), with models in order, so that
// updates change only the lines whose values changed
func FormatPricingFile(data PricingData, order []string) []byte {
	var b bytes.Buffer
	str := func(s string) string {
		q, _ := json.Marshal(s)
		return string(q)
	}
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"provider\": %s,\n", str(string(data.Provider)))
	if data.Source != "" {
		fmt.Fprintf(&b, "  \"source\": %s,\n", str(data.Source))
	}
	fmt.Fprintf(&b, "  \"lastUpdated\": %s,\n", str(data.LastUpdated))
	b.WriteString("  \"models\": {\n")
	for i, key := range order {
		e := data.Models[key]
		var fields []string
		if len(e.Aliases) > 0 {
			quoted := make([]string, len(e.Aliases))
			for j, alias := range e.Aliases {
				quoted[j] = str(alias)
			}
			fields = append(fields, `"aliases": [`+strings.Join(quoted, ", ")+`]`)
		}
		fields = append(fields,
			`"inputCostPerMTok": `+formatMTok(e.InputCostPerMTok),
			`"outputCostPerMTok": `+formatMTok(e.OutputCostPerMTok))
		if e.CacheReadCostPerMTok != 0 {
			fields = append(fields, `"cacheReadCostPerMTok": `+formatMTok(e.CacheReadCostPerMTok))
		}
		if e.CacheWriteCostPerMTok != 0 {
			fields = append(fields, `"cacheWriteCostPerMTok": `+formatMTok(e.CacheWriteCostPerMTok))
		}
		if e.Deprecated {
			fields = append(fields, `"deprecated": true`)
		}
		if lc := e.LongContext; lc != nil {
			lcFields := []string{
				fmt.Sprintf(`"thresholdInputTokens": %d`, lc.ThresholdInputTokens),
				`"inputCostPerMTok": ` + formatMTok(lc.InputCostPerMTok),
				`"outputCostPerMTok": ` + formatMTok(lc.OutputCostPerMTok),
			}
			if lc.CacheReadCostPerMTok != 0 {
				lcFields = append(lcFields, `"cacheReadCostPerMTok": `+formatMTok(lc.CacheReadCostPerMTok))
			}
			if lc.CacheWriteCostPerMTok != 0 {
				lcFields = append(lcFields, `"cacheWriteCostPerMTok": `+formatMTok(lc.CacheWriteCostPerMTok))
			}
			fields = append(fields, "\"longContext\": {\n        "+strings.Join(lcFields, ",\n        ")+"\n      }")
		}
		fmt.Fprintf(&b, "    %s: {\n      %s\n    }", str(key), strings.Join(fields, ",\n      "))
		if i < len(order)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  }\n}\n")
	return b.Bytes()
}
