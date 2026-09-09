from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

from app.telegram_auth import TelegramAuthError, validate_init_data

router = APIRouter()


class TelegramAuthRequest(BaseModel):
    init_data: str = Field(min_length=1, max_length=8192)


@router.post("/telegram")
async def authenticate_telegram(payload: TelegramAuthRequest) -> dict:
    try:
        user = validate_init_data(payload.init_data)
    except TelegramAuthError as exc:
        raise HTTPException(
            status_code=401, detail="Telegram authentication failed"
        ) from exc
    return {"authenticated": True, "user": user}
