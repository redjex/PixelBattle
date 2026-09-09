# Security and deployment

This hardening is not a guarantee that every vulnerability is eliminated. Keep
dependencies patched, monitor abuse, and test the deployed ingress and storage.

## Required configuration

- Generate unique random `POSTGRES_PASSWORD` and `ADMIN_API_TOKEN` values (at least
  32 random bytes for the admin token). Never put secrets in `VITE_*` variables.
- Set `POSTGRES_DSN` to
  `postgres://pixelbattle:<URL-encoded-password>@postgres:5432/pixelbattle?sslmode=disable`.
  The supplied Compose stack keeps database traffic on an internal network.
  Use authenticated TLS for databases outside this host.
- Run Compose from the repository root using
  `docker compose --env-file .env -f infrastructure/compose.yaml up --build`.
- Changing `POSTGRES_PASSWORD` does not rotate an existing database role password.
  Rotate that role explicitly and update the DSN together; never delete the volume.
- Set `TELEGRAM_ADMIN_IDS` explicitly. No administrators are configured by default.
- Rotate previously used admin tokens and redact old URL-bearing logs. Admin
  requests now require `Authorization: Bearer <token>`; query tokens are rejected.
- Do not expose PostgreSQL or Redis ports. Redis is isolated from web/API containers,
  but is still trusted by realtime and the bot. Use Redis ACL credentials for
  deployments requiring isolation between these trusted services.

## Ingress

The included web nginx serves static files, not the API reverse proxy. The external
TLS ingress must route `/api/auth/telegram` to Python, board/profile APIs and `/ws`
to Go, and deny public `/api/admin/` and `/api/boards/main/image` access. The bot
uses the internal Go address. The static nginx denial rules alone do not protect
an external ingress that routes directly to Go.

Set `TRUSTED_PROXY_CIDRS` to only the immediate ingress IPs, and configure that
ingress to overwrite `X-Real-IP` with the verified client IP. Without this setting,
users behind a proxy share IP-based quotas. Never trust arbitrary forwarded
headers or broad public networks. With multiple proxies, resolve the chain at the
trusted edge. Do not log Authorization, Telegram initData, request bodies or
sensitive query strings. Require HTTPS/WSS externally.

## Persistence and limitations

Back up PostgreSQL and Redis before upgrading. New checkpoints include an event
watermark. Legacy snapshots load using their old snapshot-only semantics, without
overlaying potentially stale rows after clears/restores. Placements newer than a
legacy snapshot can be omitted on that first restart; preserve a final authoritative
board export/checkpoint and reconcile it before a production upgrade.

Only one realtime instance may own the board. A database advisory lock enforces
this restriction; adding replicas is not a supported scaling strategy. Pending
Redis messages are retried instead of discarded. Monitor stream backlog, disk,
database health and recovery failures; malformed entries need operator repair.

Client-request idempotency and atomicity across inventory updates and Redis enqueue
are not guaranteed. Retried bomb requests can consume inventory again, and partial
enqueue failures require reconciliation. These need a durable request ledger/outbox,
not merely a process-local deduplication cache.

Rate limits are process-local; Python workers do not share a limiter. Add coordinated
edge limits and load-test authenticated flows. Telegram initData remains replayable
until expiration; the default is 24 hours. Public profile identity fields are kept
because the application displays pixel authors; private inventory and personal
statistics remain caller-scoped.

## Verification

- `npm run typecheck` and `npm run build`
- `go test ./...` and `go vet ./...` in `services/realtime-go`
- API and bot pytest suites; see their `SECURITY.md` files
- Set isolated `GO_TEST_POSTGRES_DSN` and `GO_TEST_REDIS_URL` for storage tests.
  Never point integration tests at production data.

Also test real Telegram authentication, bot administration, proxy spoofing,
WebSocket expiry, backup restoration and storage outages in staging. Local unit
tests do not establish production readiness.
