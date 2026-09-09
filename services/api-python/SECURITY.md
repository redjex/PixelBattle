# Security Configuration

Required: `TELEGRAM_BOT_TOKEN`, matching the Mini App bot. Missing credentials
fail authentication closed. Never send initData in query strings.

Optional settings:

| Variable | Default | Purpose |
| --- | --- | --- |
| TELEGRAM_INIT_DATA_MAX_AGE | 86400 | Signed initData lifetime, 1-604800 seconds; future timestamps tolerate only 30 seconds |
| TRUSTED_PROXY_CIDRS | empty | Comma-separated trusted socket-peer networks allowed to supply canonical `X-Real-IP` |
| API_ENABLE_DOCS | false | Explicitly enable `/docs` and `/openapi.json` |
| API_MAX_BODY_BYTES | 16384 | Maximum buffered request body, including chunked requests |
| API_REQUESTS_PER_MINUTE | 120 | Requests per source address per worker, fixed window |
| API_RATE_LIMIT_CLIENTS | 10000 | Maximum tracked addresses per worker; reject new addresses when full |
| API_WORKERS | 4 | Docker Uvicorn worker count |
| API_LIMIT_CONCURRENCY | 200 | Docker Uvicorn concurrent connection/task ceiling |

Headers are capped at 16 KiB, queries at 2 KiB, initData at 8 KiB/32 fields,
and body receipt at 10 seconds. Validation errors do not echo request input.
Responses retain the existing authentication shape but whitelist user fields.
The profile endpoint now identifies the authenticated caller rather than a
hardcoded development account; pixel statistics remain a placeholder.

The container runs as UID 10001 and disables access logs and proxy headers.
Behind a reverse proxy, the default limiter sees the proxy address. Set
`TRUSTED_PROXY_CIDRS` to the narrow ingress peer networks (for example,
`172.20.0.10/32,fd00::10/128`). nginx must overwrite `X-Real-IP` with the canonical
client address. Only a single valid IP from a configured peer is accepted;
missing, duplicate or invalid values fall back to the socket peer. IPv6 addresses
are normalized before rate limiting. `X-Forwarded-For` and `Forwarded` are ignored.
Keep Uvicorn proxy handling disabled so socket-peer verification remains reliable.
Invalid CIDR configuration is rejected; do not trust broad/uncontrolled networks.
Enforce shared per-client limits, connection/header
timeouts and body caps at the edge. Worker-local limits are not distributed and
fixed windows permit boundary bursts. Configure proxy logs not to record query
strings, auth headers or bodies. Do not enable HTTP debug logging in production.

Signed Telegram initData remains replayable until expiry; use TLS, protect it
like a bearer credential, and choose its lifetime to balance session continuity
against replay exposure. These services do not revoke individual sessions.

Tests: `.venv\Scripts\python.exe -m pytest -q`.
