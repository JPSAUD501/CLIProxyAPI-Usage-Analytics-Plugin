package analytics

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreHashesInferenceKeyAndPersistsNoContent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	secret := "test-inference-secret-that-must-not-be-persisted"
	event := Event{RequestedAt: time.Now(), Provider: "codex", Model: "gpt-test", APIKey: secret, Tokens: TokenBreakdown{Total: 1, InputUncached: 1, Quality: AccountingComplete}}
	if err := store.Insert(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Requests(context.Background(), Filters{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	if rows[0].ClientHMAC == "" || rows[0].ClientHMAC == secret {
		t.Fatalf("client identity was not HMACed")
	}
	for _, name := range []string{"usage.db", "usage.db-wal"} {
		raw, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr == nil && strings.Contains(string(raw), secret) {
			t.Fatalf("raw inference key persisted in %s", name)
		}
	}
}

func TestClientHMACIsStableAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := first.ClientHMAC("client-key")
	_ = first.Close()
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := second.ClientHMAC("client-key"); got != want {
		t.Fatalf("HMAC changed across restart")
	}
}
