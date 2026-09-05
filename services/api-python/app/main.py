from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse

from app.routes import auth, boards, health, profiles
from app.telegram_auth import TelegramAuthError, validate_init_data

app = FastAPI(title="Pixel Battle Business API", version="0.1.0")
app.add_middleware(
    CORSMiddleware,
    allow_origins=["https://pixelbattle.redjex.bond", "http://localhost:5173"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.middleware("http")
async def require_telegram_for_api(request, call_next):
    if request.url.path.startswith("/api/") and request.url.path != "/api/auth/telegram":
        init_data = request.headers.get("X-Telegram-Init-Data", "")
        try:
            validate_init_data(init_data)
        except TelegramAuthError:
            return JSONResponse(status_code=401, content={"detail": "Telegram authentication required"})
    return await call_next(request)
app.include_router(health.router)
app.include_router(auth.router, prefix="/api/auth", tags=["auth"])
app.include_router(boards.router, prefix="/api/boards", tags=["boards"])
app.include_router(profiles.router, prefix="/api/profiles", tags=["profiles"])
