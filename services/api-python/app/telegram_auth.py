import hashlib
import hmac
import json
import os
import time
from urllib.parse import parse_qsl


class TelegramAuthError(ValueError):
    pass


def validate_init_data(init_data: str) -> dict:
    token = os.getenv("TELEGRAM_BOT_TOKEN", "")
    if not token:
        raise TelegramAuthError("Telegram bot token is not configured")
    fields = dict(parse_qsl(init_data, keep_blank_values=True))
    received_hash = fields.pop("hash", "")
    if not received_hash:
        raise TelegramAuthError("Telegram initData hash is missing")
    check_string = "\n".join(f"{key}={fields[key]}" for key in sorted(fields))
    secret_key = hmac.new(b"WebAppData", token.encode(), hashlib.sha256).digest()
    expected_hash = hmac.new(secret_key, check_string.encode(), hashlib.sha256).hexdigest()
    if not hmac.compare_digest(expected_hash, received_hash):
        raise TelegramAuthError("Telegram initData hash is invalid")
    try:
        auth_date = int(fields.get("auth_date", "0"))
        user = json.loads(fields.get("user", "{}"))
    except (TypeError, ValueError, json.JSONDecodeError) as exc:
        raise TelegramAuthError("Telegram initData is malformed") from exc
    max_age = int(os.getenv("TELEGRAM_INIT_DATA_MAX_AGE", "86400"))
    if not auth_date or abs(time.time() - auth_date) > max_age:
        raise TelegramAuthError("Telegram initData is expired")
    if not user.get("id"):
        raise TelegramAuthError("Telegram user is missing")
    return user
