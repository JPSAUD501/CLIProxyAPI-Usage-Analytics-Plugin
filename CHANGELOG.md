# Changelog

## 1.2.0 - 2026-08-30

- Reuse the authenticated Management Center session when the dashboard is embedded.
- Keep the bridged management key in memory only and reject cross-origin messages.
- Retain direct-URL key entry as a protected standalone fallback.

## 1.1.1 - 2026-08-30

- Refresh pricing catalogs through the authenticated Management host callback and accept both core and canonical HTTP response wire shapes.

## 1.1.0 - 2026-08-30

- Redesign the dashboard around the T3 Code usage hierarchy with provider series, totals, filters, and model breakdown.
- Allow same-origin embedding in the CLIProxyAPI Management console while continuing to block third-party framing.
- Use the OpenRouter model catalog for OpenRouter estimates and retain LiteLLM for other providers.
- Reprice previously unpriced events after a catalog refresh without guessing ambiguous model aliases.

## 1.0.0 - 2026-08-30

- Initial cross-platform release with usage accounting, SQLite persistence, cost estimation, Management API, and embedded dashboard.
