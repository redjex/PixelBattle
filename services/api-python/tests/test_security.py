import hashlib
import hmac
import json
import runpy
import time
from urllib.parse import urlencode

import pytest
from fastapi.testclient import TestClient

from app.main import app
from app.security import RequestLimits
from app import telegram_auth
from app.telegram_auth import TelegramAuthError, validate_init_data


def signed(user=None, auth_date=None):
    fields = {
        "auth_date": str(auth_date or int(time.time())),
        "user": json.dumps(
            user if user is not None else {"id": 123, "first_name": "Player"}
        ),
    }
    key = hmac.new(b"WebAppData", b"test-token", hashlib.sha256).digest()
    digest = hmac.new(
        key,
        "\n".join(f"{k}={fields[k]}" for k in sorted(fields)).encode(),
        hashlib.sha256,
    ).hexdigest()
    return urlencode({**fields, "hash": digest})


@pytest.fixture(autouse=True)
def token(monkeypatch):
    monkeypatch.setenv("TELEGRAM_BOT_TOKEN", "test-token")


def test_valid_auth_and_profile():
    client = TestClient(app)
    data = signed({"id": 123, "first_name": "Player", "private_field": "secret"})
    result = client.post("/api/auth/telegram", json={"init_data": data})
    assert result.status_code == 200
    assert "private_field" not in result.json()["user"]
    profile = client.get("/api/profiles/me", headers={"X-Telegram-Init-Data": data})
    assert profile.json()["id"] == "123"


@pytest.mark.parametrize(
    "suffix", ["&user=%7B%7D", "&auth_date=1", "&hash=" + "a" * 64]
)
def test_duplicate_fields(suffix):
    with pytest.raises(TelegramAuthError):
        validate_init_data(signed() + suffix)


@pytest.mark.parametrize(
    "user", [[], None, "user", {"id": True}, {"id": -1}, {"id": "123"}, {"id": 2**53}]
)
def test_invalid_users(user):
    if user is None:
        user = {}
    with pytest.raises(TelegramAuthError):
        validate_init_data(signed(user))


@pytest.mark.parametrize(
    "data",
    [
        "x" * 8193,
        "hash=%FF",
        "&".join(f"x{i}=1" for i in range(33)),
        "hash=" + "z" * 64,
    ],
)
def test_malformed(data):
    with pytest.raises(TelegramAuthError):
        validate_init_data(data)


def test_dates():
    for timestamp in [int(time.time()) + 120, int(time.time()) - 86401]:
        with pytest.raises(TelegramAuthError):
            validate_init_data(signed(auth_date=timestamp))


def test_safe_responses():
    client = TestClient(app)
    assert client.get("/docs").status_code == 404
    assert client.get("/openapi.json").status_code == 404
    assert client.get("/api/profiles/me").status_code == 401
    result = client.post(
        "/api/auth/telegram", json={"init_data": {"secret": "sensitive"}}
    )
    assert result.status_code == 422
    assert "sensitive" not in result.text
    assert client.post("/api/auth/telegram", content=b"x" * 16385).status_code == 413
    assert client.get("/health", headers={"Large": "x" * 17000}).status_code == 431
    assert (
        client.get(
            "/api/profiles/me",
            headers=[
                ("X-Telegram-Init-Data", signed()),
                ("X-Telegram-Init-Data", signed()),
            ],
        ).status_code
        == 401
    )


def test_bounded_rate_limit(monkeypatch):
    monkeypatch.setenv("API_REQUESTS_PER_MINUTE", "2")
    monkeypatch.setenv("API_RATE_LIMIT_CLIENTS", "1")
    limited = RequestLimits(app)
    client = TestClient(limited)
    assert client.get("/health").status_code == 200
    assert client.get("/health").status_code == 200
    assert (
        client.get("/health", headers={"X-Forwarded-For": "new-client"}).status_code
        == 429
    )
    assert (
        TestClient(limited, client=("another", 123)).get("/health").status_code == 429
    )
    assert len(limited.clients) == 1


def test_chunked_body_cap():
    client = TestClient(app)
    assert (
        client.post(
            "/api/auth/telegram", content=iter([b"x" * 9000, b"y" * 9000])
        ).status_code
        == 413
    )


def test_internal_failure_is_generic(monkeypatch, caplog):
    import asyncio
    import app.main as main
    from starlette.requests import Request

    async def fail(*args, **kwargs):
        raise RuntimeError("https://example.test/?token=secret")

    request = Request(
        {"type": "http", "method": "GET", "path": "/health", "headers": []}
    )
    response = asyncio.run(main.require_telegram_for_api(request, fail))
    assert response.status_code == 500
    assert "secret" not in response.body.decode() + caplog.text


@pytest.mark.parametrize(
    "networks,peer", [("", "10.0.0.2"), ("10.0.0.0/24", "192.0.2.1")]
)
def test_untrusted_real_ip_cannot_evade_limit(monkeypatch, networks, peer):
    monkeypatch.setenv("TRUSTED_PROXY_CIDRS", networks)
    monkeypatch.setenv("API_REQUESTS_PER_MINUTE", "1")
    limited = RequestLimits(app)
    client = TestClient(limited, client=(peer, 123))
    assert (
        client.get("/health", headers={"X-Real-IP": "203.0.113.1"}).status_code == 200
    )
    assert (
        client.get("/health", headers={"X-Real-IP": "203.0.113.2"}).status_code == 429
    )
    assert set(limited.clients) == {peer}


@pytest.mark.parametrize("peer", ["10.0.0.2", "fd00::2"])
def test_trusted_real_ip_and_ipv6_normalization(monkeypatch, peer):
    monkeypatch.setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/24, fd00::/64")
    monkeypatch.setenv("API_REQUESTS_PER_MINUTE", "1")
    limited = RequestLimits(app)
    client = TestClient(limited, client=(peer, 123))
    assert (
        client.get("/health", headers={"X-Real-IP": "203.0.113.1"}).status_code == 200
    )
    assert (
        client.get(
            "/health", headers={"X-Real-IP": "2001:0db8:0:0:0:0:0:1"}
        ).status_code
        == 200
    )
    assert (
        client.get(
            "/health",
            headers={"X-Real-IP": "2001:db8::1", "X-Forwarded-For": "203.0.113.99"},
        ).status_code
        == 429
    )
    assert set(limited.clients) == {"203.0.113.1", "2001:db8::1"}


@pytest.mark.parametrize(
    "headers",
    [
        {},
        {"X-Real-IP": "invalid"},
        {"X-Real-IP": "203.0.113.1, 203.0.113.2"},
        {"X-Real-IP": "203.0.113.1:80"},
        {"X-Real-IP": "fe80::1%eth0"},
        {"X-Real-IP": ""},
        {"X-Forwarded-For": "203.0.113.1"},
        [("X-Real-IP", "203.0.113.1"), ("X-Real-IP", "203.0.113.2")],
        [("X-Real-IP", b"\xff")],
    ],
)
def test_invalid_trusted_header_falls_back_to_peer(monkeypatch, headers):
    monkeypatch.setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/24")
    monkeypatch.setenv("API_REQUESTS_PER_MINUTE", "1")
    limited = RequestLimits(app)
    client = TestClient(limited, client=("10.0.0.2", 123))
    assert client.get("/health", headers=headers).status_code == 200
    assert client.get("/health", headers=headers).status_code == 429
    assert set(limited.clients) == {"10.0.0.2"}


@pytest.mark.parametrize(
    "networks", ["invalid", "10.0.0.0/99", "10.0.0.0/24,", "10.0.0.1/24"]
)
def test_invalid_proxy_configuration(monkeypatch, networks):
    monkeypatch.setenv("TRUSTED_PROXY_CIDRS", networks)
    with pytest.raises(RuntimeError, match="TRUSTED_PROXY_CIDRS"):
        RequestLimits(app)


@pytest.mark.parametrize("age", ["0", "-1", "604801"])
def test_max_age_configuration_rejected(monkeypatch, age):
    monkeypatch.setenv("TELEGRAM_INIT_DATA_MAX_AGE", age)
    with pytest.raises(RuntimeError, match="604800"):
        runpy.run_path(telegram_auth.__file__)


def test_seven_day_auth_boundary(monkeypatch):
    monkeypatch.setenv("TELEGRAM_INIT_DATA_MAX_AGE", "604800")
    module = runpy.run_path(telegram_auth.__file__)
    now = 1800000000
    monkeypatch.setattr(time, "time", lambda: now)
    assert module["validate_init_data"](signed(auth_date=now - 604800))["id"] == 123
    with pytest.raises(module["TelegramAuthError"]):
        module["validate_init_data"](signed(auth_date=now - 604801))
