package server

import "strings"

// modelPrice is USD per 1,000,000 tokens (input, output).
type modelPrice struct{ inPerM, outPerM float64 }

// priceTable maps a model id substring -> price. Lookup is substring-based
// (case-insensitive) so provider-prefixed ids like "kr/claude-opus-4.8-..."
// or "cx/gpt-5.5-review" still match. First match in priority order wins.
var priceTable = []struct {
	match string
	price modelPrice
}{
	{"claude-opus", modelPrice{15.0, 75.0}},
	{"claude-haiku", modelPrice{1.0, 5.0}},
	{"claude-sonnet", modelPrice{3.0, 15.0}},
	{"claude", modelPrice{3.0, 15.0}},
	{"gpt-5", modelPrice{1.25, 10.0}},
	{"gpt-4o-mini", modelPrice{0.15, 0.6}},
	{"gpt-4o", modelPrice{2.5, 10.0}},
	{"gpt-4", modelPrice{10.0, 30.0}},
	{"gpt", modelPrice{1.0, 3.0}},
	{"gemini-3.1-pro", modelPrice{1.25, 10.0}},
	{"gemini-3.1-flash", modelPrice{0.075, 0.3}},
	{"gemini-3-flash", modelPrice{0.075, 0.3}},
	{"gemini-pro", modelPrice{1.25, 5.0}},
	{"gemini", modelPrice{0.3, 1.2}},
	{"gemma", modelPrice{0.05, 0.15}},
}

// computeCost returns USD cost for the given model id and token counts. Returns
// 0 when no price entry matches (unknown model) — better an honest 0 than a
// fabricated number.
func computeCost(modelID string, inputTokens, outputTokens int64) float64 {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return 0
	}
	for _, e := range priceTable {
		if strings.Contains(id, e.match) {
			return float64(inputTokens)/1e6*e.price.inPerM + float64(outputTokens)/1e6*e.price.outPerM
		}
	}
	return 0
}
