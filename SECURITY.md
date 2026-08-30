# Security policy

Report vulnerabilities privately through GitHub Security Advisories. Do not include real API keys, OAuth tokens, prompts, responses, or production databases.

The plugin treats inference keys and upstream credentials as secrets. It persists only a local HMAC of an inference key and never persists request or response content. The public resource route serves only the application shell; data routes remain behind CLIProxyAPI Management authentication.

Supported security fixes are released for the latest major version.
