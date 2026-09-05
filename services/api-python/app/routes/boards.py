from fastapi import APIRouter
from pydantic import BaseModel

router = APIRouter()

class Board(BaseModel):
    id: str
    title: str
    width: int
    height: int

@router.get("", response_model=list[Board])
async def list_boards() -> list[Board]:
    return [Board(id="main", title="Pixel Battle", width=64, height=64)]

