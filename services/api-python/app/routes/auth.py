from fastapi import APIRouter, HTTPException
import logging
from pydantic import BaseModel

from app.telegram_auth import TelegramAuthError, validate_init_data

router = APIRouter()
logger = logging.getLogger("telegram-auth")


class TelegramAuthRequest(BaseModel):
    init_data: str


@router.post("/telegram")
async def authenticate_telegram(payload: TelegramAuthRequest) -> dict:
    try:
        user = validate_init_data(payload.init_data)
    except TelegramAuthError as exc:
        logger.warning("Telegram initData rejected: %s", exc)
        raise HTTPException(status_code=401, detail="Telegram authentication failed") from exc
    return {"authenticated": True, "user": user}
