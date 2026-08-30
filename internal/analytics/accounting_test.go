package analytics

import "testing"

func TestSubsetDoesNotDoubleCountCacheOrReasoning(t *testing.T) {
	b := Account("codex", "", RawUsage{Input: 100, CacheRead: 40, Output: 30, Reasoning: 10, Total: 130})
	if b.Total != 130 || b.InputUncached != 60 || b.CacheRead != 40 || b.OutputNonReasoning != 20 || b.Reasoning != 10 || b.Quality != AccountingComplete {
		t.Fatalf("unexpected breakdown: %#v", b)
	}
}

func TestUnknownProviderPreservesAuthoritativeTotal(t *testing.T) {
	b := Account("future-provider", "", RawUsage{Input: 9, Output: 4, Total: 13})
	if b.Total != 13 || b.Unclassified != 13 || b.Quality != AccountingUnclassified {
		t.Fatalf("unexpected breakdown: %#v", b)
	}
}

func TestContradictoryTotalsAreInconsistent(t *testing.T) {
	b := Account("openai", "", RawUsage{Input: 100, Output: 20, Total: 10})
	if b.Quality != AccountingInconsistent || b.Unclassified != 10 {
		t.Fatalf("unexpected breakdown: %#v", b)
	}
}
