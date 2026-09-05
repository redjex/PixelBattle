# Architecture

The placement path is deliberately small:

1. React sends a placement over WebSocket.
2. Go validates coordinates, colour, identity and cooldown.
3. The accepted event is appended to Redis Streams.
4. Go broadcasts the accepted event to connected users.
5. Workers flush up to 500 events or every 200 ms to PostgreSQL.
6. Events are acknowledged only after the database transaction commits.

Python is outside this latency-sensitive path. It owns profiles, board management, moderation, analytics and future integrations.

For the first local iteration `GO_DEV_IN_MEMORY=true` removes the Redis/PostgreSQL requirement. Docker Compose enables the infrastructure-backed mode.

