import asyncio
import ipaddress
import os
import time

from starlette.responses import JSONResponse


class RequestLimits:
    """Bound work before authentication; trust canonical IPs only from explicit peers."""

    def __init__(self, app):
        self.app = app
        self.body_limit = int(os.getenv("API_MAX_BODY_BYTES", "16384"))
        self.rate = int(os.getenv("API_REQUESTS_PER_MINUTE", "120"))
        self.capacity = int(os.getenv("API_RATE_LIMIT_CLIENTS", "10000"))
        if min(self.body_limit, self.rate, self.capacity) <= 0:
            raise RuntimeError("API request limits must be positive")
        raw_networks = os.getenv("TRUSTED_PROXY_CIDRS", "").strip()
        try:
            self.trusted_proxies = (
                tuple(
                    ipaddress.ip_network(value.strip())
                    for value in raw_networks.split(",")
                )
                if raw_networks
                else ()
            )
        except ValueError:
            raise RuntimeError(
                "TRUSTED_PROXY_CIDRS must contain valid comma-separated networks"
            ) from None
        self.clients = {}

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            return await self.app(scope, receive, send)

        async def reject(status, detail):
            await JSONResponse({"detail": detail}, status_code=status)(
                scope, receive, send
            )

        headers = scope.get("headers", [])
        if (
            sum(len(k) + len(v) for k, v in headers) > 16384
            or len(scope.get("query_string", b"")) > 2048
        ):
            return await reject(431, "Request headers too large")
        now = time.monotonic()
        client = (scope.get("client") or ("unknown",))[0]
        try:
            peer = ipaddress.ip_address(client)
        except ValueError:
            peer = None
        if peer is not None:
            client = str(peer)
            if any(peer in network for network in self.trusted_proxies):
                real_ips = [value for key, value in headers if key == b"x-real-ip"]
                if len(real_ips) == 1:
                    try:
                        value = real_ips[0].decode("ascii")
                        # No chains, ports, zone IDs or duplicate headers; normalize IPv6.
                        if "%" not in value:
                            client = str(ipaddress.ip_address(value))
                    except (ValueError, UnicodeError):
                        pass
        entry = self.clients.get(client)
        if entry is None or now >= entry[0]:
            if entry is None and len(self.clients) >= self.capacity:
                self.clients = {k: v for k, v in self.clients.items() if now < v[0]}
                if len(self.clients) >= self.capacity:
                    return await reject(429, "Too many requests")
            entry = self.clients[client] = [now + 60, 0]
        entry[1] += 1
        if entry[1] > self.rate:
            return await reject(429, "Too many requests")
        lengths = [v for k, v in headers if k == b"content-length"]
        try:
            if len(lengths) > 1 or (lengths and (not lengths[0].isdigit())):
                return await reject(400, "Invalid request")
            if lengths and int(lengths[0]) > self.body_limit:
                return await reject(413, "Request body too large")
        except ValueError:
            return await reject(400, "Invalid request")
        body = bytearray()
        try:
            async with asyncio.timeout(10):
                while True:
                    message = await receive()
                    if message["type"] == "http.disconnect":
                        return
                    body.extend(message.get("body", b""))
                    if len(body) > self.body_limit:
                        return await reject(413, "Request body too large")
                    if not message.get("more_body", False):
                        break
        except TimeoutError:
            return await reject(408, "Request timed out")
        consumed = False

        async def replay():
            nonlocal consumed
            if not consumed:
                consumed = True
                return {"type": "http.request", "body": bytes(body), "more_body": False}
            return await receive()

        await self.app(scope, replay, send)
