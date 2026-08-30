# CLIProxyAPI Usage Analytics Plugin

Native usage analytics for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI). It records finalized `UsagePlugin` events without prompts, responses, authorization headers, OAuth tokens, or raw inference keys.

## Features

- Canonical non-overlapping accounting for input, cache read/write, output, reasoning, and unclassified tokens.
- SQLite WAL storage with transactional migrations and 90-day default retention.
- Stable HMAC client identities; raw inference keys are never persisted.
- LiteLLM pricing with a last-known-good cache, versioned fallback, nano-USD arithmetic, and explicit unpriced events.
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

Open `/v0/resource/plugins/usage-analytics/dashboard` and enter the Management key. The key remains in the tab's session storage.

## Development

```sh
cd ui && npm ci && npm run build
cd .. && go test ./... && go vet ./...
go build -trimpath -buildmode=c-shared -o usage-analytics.so ./cmd/usage-analytics-plugin
```

See [SECURITY.md](SECURITY.md) before reporting a vulnerability. MIT licensed.
