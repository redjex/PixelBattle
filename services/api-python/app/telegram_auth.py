import hashlib
import hmac
import json
import os
import re
import time
from urllib.parse import parse_qsl

MAX_AGE = int(os.getenv("TELEGRAM_INIT_DATA_MAX_AGE", "86400"))
if not 1 <= MAX_AGE <= 604800:
    raise RuntimeError(
        "TELEGRAM_INIT_DATA_MAX_AGE must be between 1 and 604800 seconds"
    )


class TelegramAuthError(ValueError):
    pass


def validate_init_data(init_data: str) -> dict:
    token = os.getenv("TELEGRAM_BOT_TOKEN", "")
    if not token:
        raise TelegramAuthError("Telegram bot token is not configured")
    if not isinstance(init_data, str) or len(init_data.encode()) > 8192:
        raise TelegramAuthError("Telegram initData is too large")
    try:
        pairs = parse_qsl(
            init_data,
            keep_blank_values=True,
            strict_parsing=True,
            max_num_fields=32,
            errors="strict",
        )
    except (ValueError, UnicodeError):
        raise TelegramAuthError("Telegram initData is malformed") from None
    fields = dict(pairs)
    if len(fields) != len(pairs) or any(
        not re.fullmatch(r"[a-z_]+", key) for key in fields
    ):
        raise TelegramAuthError("Telegram initData fields are invalid")
    received_hash = fields.pop("hash", "")
    if not re.fullmatch(r"[0-9a-f]{64}", received_hash):
        raise TelegramAuthError("Telegram initData hash is missing")
    check_string = "\n".join(f"{key}={fields[key]}" for key in sorted(fields))
    secret_key = hmac.new(b"WebAppData", token.encode(), hashlib.sha256).digest()
    expected_hash = hmac.new(
        secret_key, check_string.encode(), hashlib.sha256
    ).hexdigest()
    if not hmac.compare_digest(expected_hash, received_hash):
        raise TelegramAuthError("Telegram initData hash is invalid")
    try:
        auth_date = int(fields.get("auth_date", "0"))
        user = json.loads(fields.get("user", "{}"))
    except (TypeError, ValueError, RecursionError) as exc:
        raise TelegramAuthError("Telegram initData is malformed") from exc
    age = time.time() - auth_date
    if not auth_date or age < -30 or age > MAX_AGE:
        raise TelegramAuthError("Telegram initData is expired")
    if (
        not isinstance(user, dict)
        or type(user.get("id")) is not int
        or not 0 < user["id"] < 2**53
    ):
        raise TelegramAuthError("Telegram user is missing")
    return {
        key: value
        for key, value in user.items()
        if key == "id"
        or (
            key in {"first_name", "last_name", "username", "language_code", "photo_url"}
            and isinstance(value, str)
            and len(value) <= 2048
        )
    }
