# CLIProxyAPI Usage Analytics Plugin

Native usage analytics for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI). It records finalized `UsagePlugin` events without prompts, responses, authorization headers, OAuth tokens, or raw inference keys.

## Features

- Canonical non-overlapping accounting for input, cache read/write, output, reasoning, and unclassified tokens.
- SQLite WAL storage with transactional migrations and 90-day default retention.
- Stable HMAC client identities; raw inference keys are never persisted.
- Provider-aware pricing: the OpenRouter catalog is authoritative for OpenRouter models, while the LiteLLM table is used for other providers. Both use last-known-good caches, nano-USD arithmetic, exact non-ambiguous model matching, and explicit unpriced events.
- Embedded React dashboard in PT-BR and English.

## Configuration

```yaml
plugins:
  enabled: true
  configs:
    usage-analytics:
      enabled: true
      retention_days: 90
      identity_mode: full
```

Open `/v0/resource/plugins/usage-analytics/dashboard` directly or from the CLIProxyAPI Management sidebar and enter the Management key. The key remains in the tab's session storage.

## Development

```sh
cd ui && npm ci && npm run build
cd .. && go test ./... && go vet ./...
go build -trimpath -buildmode=c-shared -o usage-analytics.so ./cmd/usage-analytics-plugin
```

See [SECURITY.md](SECURITY.md) before reporting a vulnerability. MIT licensed.
