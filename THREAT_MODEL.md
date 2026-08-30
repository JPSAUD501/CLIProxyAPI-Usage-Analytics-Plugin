# Threat model

Protected assets are Management credentials, inference credentials, OAuth material, prompts, responses, usage records, and account identities. Trust boundaries are the CLIProxyAPI plugin ABI, host HTTP callback, persistent plugin directory, browser Management session, and upstream pricing catalog.

Controls include Management-authenticated data routes, a data-free public shell, no-store responses, CSP, bounded request bodies and pagination, HMAC client identities, SQLite file permissions, transactional migrations, atomic last-known-good pricing, and fail-closed parsing. The plugin never logs or stores prompt/response bodies, authorization headers, OAuth tokens, or upstream error bodies.

Administrators remain responsible for protecting the CLIProxyAPI Management key and filesystem volume.
