package analytics

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

//go:embed fallback_prices.json
var fallbackPrices []byte

const priceCatalogURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

type ModelPrice struct{ Input, CacheRead, CacheCreation, Output int64 }
type Pricing struct {
	Source, Version string
	UpdatedAt       time.Time
	Models          map[string]ModelPrice
}

func ParsePricing(raw []byte, source, version string) (Pricing, error) {
	var docs map[string]map[string]json.RawMessage
	if json.Unmarshal(raw, &docs) != nil {
		return Pricing{}, errors.New("invalid pricing payload")
	}
	models := map[string]ModelPrice{}
	for id, doc := range docs {
		input, okIn := nanoRate(doc["input_cost_per_token"])
		output, okOut := nanoRate(doc["output_cost_per_token"])
		if !okIn || !okOut {
			continue
		}
		read, _ := nanoRate(doc["cache_read_input_token_cost"])
		creation, _ := nanoRate(doc["cache_creation_input_token_cost"])
		models[id] = ModelPrice{Input: input, CacheRead: read, CacheCreation: creation, Output: output}
	}
	if len(models) == 0 {
		return Pricing{}, errors.New("pricing payload contains no usable models")
	}
	return Pricing{Source: source, Version: version, UpdatedAt: time.Now().UTC(), Models: models}, nil
}
func nanoRate(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return int64(math.Round(f * 1e9)), true
}
func (p Pricing) Cost(model string, b TokenBreakdown) (int64, int64, bool) {
	candidates := []string{model, "openai/" + model, "anthropic/" + model}
	var rate ModelPrice
	ok := false
	for _, id := range candidates {
		if value, exists := p.Models[id]; exists {
			rate = value
			ok = true
			break
		}
	}
	if !ok {
		return 0, 0, false
	}
	readRate := rate.CacheRead
	if readRate == 0 {
		readRate = rate.Input
	}
	creationRate := rate.CacheCreation
	if creationRate == 0 {
		creationRate = rate.Input
	}
	cost := b.InputUncached*rate.Input + b.CacheRead*readRate + b.CacheCreation*creationRate + (b.OutputNonReasoning+b.Reasoning)*rate.Output
	savings := b.CacheRead * (rate.Input - readRate)
	if savings < 0 {
		savings = 0
	}
	return cost, savings, true
}
func priceVersion(headers map[string][]string) string {
	for _, key := range []string{"Etag", "ETag", "Last-Modified"} {
		if values := headers[key]; len(values) > 0 && strings.TrimSpace(values[0]) != "" {
			return strings.TrimSpace(values[0])
		}
	}
	return time.Now().UTC().Format("20060102")
}
