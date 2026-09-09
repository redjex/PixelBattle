# Security Configuration

- `TELEGRAM_BOT_TOKEN`: required Telegram credential.
- `MINI_APP_LINK`: required `https://t.me/...` Mini App deep link.
- `TELEGRAM_ADMIN_IDS`: comma-separated positive Telegram user IDs, e.g.
  `123456789,987654321`. Empty/unset grants nobody bot administration. Use the
  same administrator configuration in coordinating services; old hardcoded
  administrators no longer receive access automatically.
- `ADMIN_API_TOKEN`: required when administrators are configured, and needed
  for map retrieval. Must match the Go service. All realtime requests use
  `Authorization: Bearer <token>`, never a query token; redirects are disabled.
- `REALTIME_URL`: defaults to `http://realtime:8080`; use a trusted private
  network or HTTPS. Do not embed credentials or query parameters in this URL.
- `REDIS_URL`: defaults to `redis://redis:6379/0`; use private authenticated Redis.
- `MINI_APP_URL`: defaults to `https://pixelbattle.redjex.bond` for assets.
- `BOT_MAP_THROTTLE_SECONDS`: positive integer, default 10, shared per-chat
  cooldown for map sends and refreshes. Also at most one render starts per
  second globally. State is capped at 4096 chats and rejects new chats when
  full until entries expire. Broadcasts are paced to preserve group delivery.

Run one polling bot instance. Limits are process-local and reset at restart.
Telegram callbacks are acknowledged even if rendering is throttled; the existing
map stays visible. Telegram image downloads stop at 20 MiB and source image
conversion rejects more than 16 million pixels. The container runs as UID 10001.

Loop failures log a fixed message, not exceptions, upstream response bodies,
URLs or tokens. Keep Requests/urllib3 debug logging disabled. Restrict access to
Redis: some administrative cooldown operations still write directly to Redis.
Existing persisted cooldown bypass entries are not automatically revoked when
administrator IDs change; audit them during deployment.

Residual limits: saved group registrations and administrator workflow state
are not globally quota-managed. Broadcasts remain synchronous and large group
lists can delay polling. General Telegram command/callback floods still consume
polling and acknowledgement capacity even when map rendering is blocked.

Tests require pytest plus requirements.txt. From this directory the shared
workspace test environment can run:
`..\api-python\.venv\Scripts\python.exe -m pytest -q tests`.
Tests mock Telegram, Redis and realtime; deploy-time integration should verify
the Go Bearer-only contract, actual Telegram delivery and non-root containers.
