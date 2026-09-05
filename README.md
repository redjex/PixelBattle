# Pixel Battle

Local-first monorepo for a realtime pixel canvas.

## Quick start

1. Copy `.env.example` to `.env`.
2. Install frontend dependencies: `npm install`.
3. Start the UI: `npm run dev`.
4. Optional full stack: `docker compose -f infrastructure/compose.yaml up --build`.

The web app works without backend services in mock mode. When the Go WebSocket is available it automatically switches to realtime events.

## Services

- `apps/web`: React, TypeScript and Canvas UI.
- `services/realtime-go`: realtime placements, validation, WebSocket fan-out and batched persistence.
- `services/api-python`: business API for boards, profiles and administration.
- `infrastructure`: PostgreSQL, Redis and service containers.

The splash screen uses `apps/web/public/assets/loader.json`.
