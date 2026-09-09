from fastapi import APIRouter, Request
from pydantic import BaseModel

router = APIRouter()


class Profile(BaseModel):
    id: str
    display_name: str
    pixels_placed: int


@router.get("/me", response_model=Profile)
async def current_profile(request: Request) -> Profile:
    user = request.state.telegram_user
    return Profile(
        id=str(user["id"]),
        display_name=user.get("first_name") or "Player",
        pixels_placed=0,
    )
