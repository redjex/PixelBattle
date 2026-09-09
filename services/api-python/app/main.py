import os
import logging
from fastapi import FastAPI
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse

from app.routes import auth, boards, health, profiles
from app.telegram_auth import TelegramAuthError, validate_init_data
from app.security import RequestLimits

docs = os.getenv("API_ENABLE_DOCS", "false").lower() == "true"
app = FastAPI(
    title="Pixel Battle Business API",
    version="0.1.0",
    docs_url="/docs" if docs else None,
    redoc_url=None,
    openapi_url="/openapi.json" if docs else None,
)


@app.exception_handler(RequestValidationError)
async def validation_error(request, exc):
    return JSONResponse(status_code=422, content={"detail": "Invalid request"})


app.add_middleware(
    CORSMiddleware,
    allow_origins=["https://pixelbattle.redjex.bond", "http://localhost:5173"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.middleware("http")
async def require_telegram_for_api(request, call_next):
    if (
        request.method != "OPTIONS"
        and request.url.path.startswith("/api/")
        and request.url.path != "/api/auth/telegram"
    ):
        init_data = request.headers.get("X-Telegram-Init-Data", "")
        try:
            if len(request.headers.getlist("X-Telegram-Init-Data")) != 1:
                raise TelegramAuthError("Invalid authentication headers")
            request.state.telegram_user = validate_init_data(init_data)
        except TelegramAuthError:
            return JSONResponse(
                status_code=401, content={"detail": "Telegram authentication required"}
            )
    try:
        response = await call_next(request)
    except Exception:
        logging.getLogger("api").error("Request processing failed")
        return JSONResponse(
            status_code=500, content={"detail": "Internal server error"}
        )
    response.headers["Cache-Control"] = "no-store"
    response.headers["X-Content-Type-Options"] = "nosniff"
    return response


app.add_middleware(RequestLimits)
app.include_router(health.router)
app.include_router(auth.router, prefix="/api/auth", tags=["auth"])
app.include_router(boards.router, prefix="/api/boards", tags=["boards"])
app.include_router(profiles.router, prefix="/api/profiles", tags=["profiles"])
