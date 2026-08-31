package analytics

import "testing"

func TestOpenRouterPricingResolvesUniqueShortModelID(t *testing.T) {
	raw := []byte(`{"data":[{"id":"z-ai/glm-5.3-flash","pricing":{"prompt":"0.000000075","completion":"0.00000025","input_cache_read":"0.000000015"}},{"id":"z-ai/glm-5.3-flash:batch","pricing":{"prompt":"0.00000015","completion":"0.0000005"}}]}`)
	pricing, err := ParseOpenRouterPricing(raw, "test")
	if err != nil {
		t.Fatal(err)
	}
	cost, savings, ok := pricing.Cost("openrouter", "glm-5.3-flash", TokenBreakdown{InputUncached: 100, CacheRead: 100, OutputNonReasoning: 20})
	if !ok || cost != 14000 || savings != 6000 {
		t.Fatalf("unexpected price result: cost=%d savings=%d ok=%v", cost, savings, ok)
	}
}

func TestPricingDoesNotGuessAmbiguousModelAlias(t *testing.T) {
	raw := []byte(`{"data":[{"id":"vendor-a/shared","pricing":{"prompt":"0.000001","completion":"0.000002"}},{"id":"vendor-b/shared","pricing":{"prompt":"0.000003","completion":"0.000004"}}]}`)
	pricing, err := ParseOpenRouterPricing(raw, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := pricing.Cost("openrouter", "shared", TokenBreakdown{InputUncached: 1}); ok {
		t.Fatal("ambiguous short model ID must remain unpriced")
	}
}

func TestLiteLLMPricingUsesProviderScopedPrefix(t *testing.T) {
	raw := []byte(`{"openai/same":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002},"anthropic/same":{"input_cost_per_token":0.000003,"output_cost_per_token":0.000004}}`)
	pricing, err := ParsePricing(raw, "LiteLLM", "test")
	if err != nil {
		t.Fatal(err)
	}
	cost, _, ok := pricing.Cost("anthropic", "same", TokenBreakdown{InputUncached: 1, OutputNonReasoning: 1})
	if !ok || cost != 7000 {
		t.Fatalf("provider-scoped cost=%d ok=%v", cost, ok)
	}
}

func TestOpenRouterEventsNeverUseLiteLLMEstimate(t *testing.T) {
	raw := []byte(`{"glm-5.3-flash":{"input_cost_per_token":0.000009,"output_cost_per_token":0.000009}}`)
	pricing, err := ParsePricing(raw, "LiteLLM", "test")
	if err != nil { t.Fatal(err) }
	if _, _, ok := pricing.Cost("openrouter", "glm-5.3-flash", TokenBreakdown{InputUncached: 1}); ok {
		t.Fatal("OpenRouter usage must use the OpenRouter catalog")
	}
}
