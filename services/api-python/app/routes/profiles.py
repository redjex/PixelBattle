from fastapi import APIRouter
from pydantic import BaseModel

router = APIRouter()

class Profile(BaseModel):
    id: str
    display_name: str
    pixels_placed: int

@router.get("/me", response_model=Profile)
async def current_profile() -> Profile:
    return Profile(id="local-user", display_name="redjex", pixels_placed=0)

