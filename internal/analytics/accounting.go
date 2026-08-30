package analytics

import "strings"

type AccountingQuality string

const (
	AccountingComplete     AccountingQuality = "complete"
	AccountingUnclassified AccountingQuality = "unclassified"
	AccountingInconsistent AccountingQuality = "inconsistent"
)

type TokenBreakdown struct {
	Quality            AccountingQuality `json:"quality"`
	Total              int64             `json:"total"`
	InputUncached      int64             `json:"input_uncached"`
	CacheRead          int64             `json:"cache_read"`
	CacheCreation      int64             `json:"cache_creation"`
	OutputNonReasoning int64             `json:"output_non_reasoning"`
	Reasoning          int64             `json:"reasoning"`
	Unclassified       int64             `json:"unclassified"`
}

type RawUsage struct {
	Input, Output, Reasoning, Cached, CacheRead, CacheCreation, Total int64
}

func Account(provider, executor string, d RawUsage) TokenBreakdown {
	if anyNegative(d) {
		return inconsistent(d.Total)
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	executor = strings.ToLower(strings.TrimSpace(executor))
	cacheRead := d.CacheRead
	if cacheRead == 0 {
		cacheRead = d.Cached
	}
	switch {
	case matches(provider, executor, "claude", "anthropic"):
		return independent(d.Input, cacheRead, d.CacheCreation, d.Output, d.Reasoning, d.Total)
	case matches(provider, executor, "gemini", "aistudio", "antigravity", "vertex", "interaction"):
		return separateReasoning(d.Input, cacheRead, d.CacheCreation, d.Output, d.Reasoning, d.Total)
	case matches(provider, executor, "codex", "openai", "xai", "kimi", "openrouter"):
		return subset(d.Input, cacheRead, d.CacheCreation, d.Output, d.Reasoning, d.Total)
	default:
		if d.Total == 0 {
			return TokenBreakdown{Quality: AccountingComplete}
		}
		return TokenBreakdown{Quality: AccountingUnclassified, Total: d.Total, Unclassified: d.Total}
	}
}

func subset(input, read, creation, output, reasoning, total int64) TokenBreakdown {
	if read+creation > input || reasoning > output {
		return inconsistent(total)
	}
	return resolve(TokenBreakdown{InputUncached: input - read - creation, CacheRead: read, CacheCreation: creation, OutputNonReasoning: output - reasoning, Reasoning: reasoning}, total)
}

func independent(input, read, creation, output, reasoning, total int64) TokenBreakdown {
	return resolve(TokenBreakdown{InputUncached: input, CacheRead: read, CacheCreation: creation, OutputNonReasoning: output, Reasoning: reasoning}, total)
}

func separateReasoning(input, read, creation, output, reasoning, total int64) TokenBreakdown {
	if read+creation > input {
		return inconsistent(total)
	}
	return resolve(TokenBreakdown{InputUncached: input - read - creation, CacheRead: read, CacheCreation: creation, OutputNonReasoning: output, Reasoning: reasoning}, total)
}

func resolve(b TokenBreakdown, authoritative int64) TokenBreakdown {
	known := b.InputUncached + b.CacheRead + b.CacheCreation + b.OutputNonReasoning + b.Reasoning
	if authoritative == 0 {
		authoritative = known
	}
	if authoritative < known {
		return inconsistent(authoritative)
	}
	b.Total = authoritative
	b.Unclassified = authoritative - known
	b.Quality = AccountingComplete
	if b.Unclassified > 0 {
		b.Quality = AccountingUnclassified
	}
	return b
}

func inconsistent(total int64) TokenBreakdown {
	if total < 0 {
		total = 0
	}
	return TokenBreakdown{Quality: AccountingInconsistent, Total: total, Unclassified: total}
}

func anyNegative(d RawUsage) bool {
	return d.Input < 0 || d.Output < 0 || d.Reasoning < 0 || d.Cached < 0 || d.CacheRead < 0 || d.CacheCreation < 0 || d.Total < 0
}

func matches(provider, executor string, values ...string) bool {
	for _, value := range values {
		if provider == value || executor == value || strings.Contains(provider, value) || strings.Contains(executor, value) {
			return true
		}
	}
	return false
}
