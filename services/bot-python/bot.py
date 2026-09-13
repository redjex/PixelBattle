import json
import hashlib
import os
import threading
import time
import colorsys
import re
from datetime import datetime, timedelta, timezone
from typing import Any

import redis
import requests
from io import BytesIO
from PIL import Image


TOKEN = os.environ["TELEGRAM_BOT_TOKEN"].strip()
API = f"https://api.telegram.org/bot{TOKEN}"
APP_URL = os.getenv("MINI_APP_URL", "https://pixelbattle.redjex.bond")
APP_LINK = os.environ["MINI_APP_LINK"].strip()


if not APP_LINK.startswith("https://t.me/"):
    raise RuntimeError(
        "MINI_APP_LINK must be a Telegram Mini App deep link starting with https://t.me/"
    )
REALTIME_URL = os.getenv("REALTIME_URL", "http://realtime:8080").rstrip("/")
ADMIN_API_TOKEN = os.getenv("ADMIN_API_TOKEN", "")
raw_admin_ids = os.getenv("TELEGRAM_ADMIN_IDS", "").strip()
if raw_admin_ids and not re.fullmatch(r"[0-9]+(?:\s*,\s*[0-9]+)*", raw_admin_ids):
    raise RuntimeError(
        "TELEGRAM_ADMIN_IDS must be comma-separated positive Telegram IDs"
    )
ADMIN_IDS = (
    {int(value.strip()) for value in raw_admin_ids.split(",")}
    if raw_admin_ids
    else set()
)
if any(not 0 < value < 2**53 for value in ADMIN_IDS):
    raise RuntimeError("TELEGRAM_ADMIN_IDS contains an invalid ID")
if ADMIN_IDS and not ADMIN_API_TOKEN.strip():
    raise RuntimeError(
        "ADMIN_API_TOKEN is required when bot administrators are configured"
    )
MAP_THROTTLE_SECONDS = int(os.getenv("BOT_MAP_THROTTLE_SECONDS", "10"))
if MAP_THROTTLE_SECONDS < 1:
    raise RuntimeError("BOT_MAP_THROTTLE_SECONDS must be positive")
map_requests: dict[int, float] = {}
map_global_next = 0.0
map_lock = threading.Lock()

BYPASS_KEY = "pixelbattle:cooldown:bypass"
RATE_LIMIT_KEY = "pixelbattle:cooldown:seconds"
GLOBAL_RATE_LIMIT_KEY = "pixelbattle:cooldown:global_seconds"
GAME_PAUSED_KEY = "pixelbattle:game:paused"
TEST_MODE_KEY = "pixelbattle:game:test_mode"
USERNAME_KEY = "pixelbattle:users:username"
USER_ID_KEY = "pixelbattle:users:id"
MAP_CHATS_KEY = "pixelbattle:map:chats"
MAP_MESSAGE_KEY_PREFIX = "pixelbattle:map:message:"
ADMIN_MENU_MESSAGE_KEY_PREFIX = "pixelbattle:admin:menu:message:"
MAP_CLEAR_BACKUP_KEY = "pixelbattle:map:last_clear_backup"
CAPTCHA_NOTIFICATION_KEY = "pixelbattle:captcha:admin_notifications"
CAPTCHA_NOTIFICATION_MESSAGE_KEY = "pixelbattle:captcha:admin_message_ids"
CAPTCHA_PENALTY_KEY = "pixelbattle:captcha:penalty_strikes"
CAPTCHA_TOPIC_ANNOUNCEMENT_KEY = "pixelbattle:captcha:topic:announcement:v2"
CAPTCHA_TOPIC_POINTER_KEY = "pixelbattle:captcha:topic:pointer:v1"
CAPTCHA_NOTIFICATION_POLL_SECONDS = 5
CAPTCHA_SUSPICION_GRACE_SECONDS = 60
CAPTCHA_ALERT_CHAT_ID = int(os.getenv("CAPTCHA_ALERT_CHAT_ID", "-1004326871238"))
CAPTCHA_TOPIC_NAME = os.getenv("CAPTCHA_TOPIC_NAME", "Боты").strip() or "Боты"
CONFIRMED_BOT_USERNAMES = {
    "613263066": "volks_bagged",
    "773016303": "devconfig",
    "796632015": "mrchopra10",
    "820593275": "",
    "882199385": "dotj2",
    "972232344": "kavikavon",
    "991531836": "d2rok",
}
REWARD_REQUEST_POLL_SECONDS = 5
TROPHY_ALERT_CHAT_ID = int(os.getenv("TROPHY_ALERT_CHAT_ID", str(CAPTCHA_ALERT_CHAT_ID)))
TROPHY_TOPIC_NAME = os.getenv("TROPHY_TOPIC_NAME", "Трофеи").strip() or "Трофеи"
COMMON_REWARDS_TOPIC_NAME = (
    os.getenv("COMMON_REWARDS_TOPIC_NAME", "Обычные награды").strip()
    or "Обычные награды"
)
TROPHY_NOTIFICATION_INTERVAL_SECONDS = float(
    os.getenv("TROPHY_NOTIFICATION_INTERVAL_SECONDS", "3.5")
)
if TROPHY_NOTIFICATION_INTERVAL_SECONDS < 3:
    raise RuntimeError("TROPHY_NOTIFICATION_INTERVAL_SECONDS must be at least 3")
TROPHY_NOTIFICATION_POLL_SECONDS = 5
NOTIFICATION_TOPICS = {
    "captcha": (
        CAPTCHA_TOPIC_NAME,
        f"pixelbattle:captcha:topic:{CAPTCHA_ALERT_CHAT_ID}",
    ),
    "trophy": (
        TROPHY_TOPIC_NAME,
        f"pixelbattle:trophies:topic:{TROPHY_ALERT_CHAT_ID}",
    ),
    "common": (
        COMMON_REWARDS_TOPIC_NAME,
        f"pixelbattle:common-rewards:topic:{TROPHY_ALERT_CHAT_ID}",
    ),
}
TROPHY_IMAGE_PATHS = {
    "experience": "/assets/trophy-notifications/experience.png",
    "bomb": "/assets/trophy-notifications/bomb.png",
    "ice": "/assets/trophy-notifications/ice.png",
    "yng-explrz": "/assets/trophy-notifications/yng-explrz.png",
    "besigned": "/assets/trophy-notifications/besigned.png",
    "stickers": "/assets/trophy-notifications/stickers.png",
    "stikidbot": "/assets/trophies/mars.png?v=1",
    "stashvpn": "/assets/trophies/stash.png?v=1",
    "bear": "/assets/trophy-notifications/bear.png",
    "bear-redjex": "/assets/trophies/bear-redjex.png?v=1",
    "liberty-figure-252202": "/assets/trophies/png/5.png",
    "candy-cane-162605": "/assets/trophies/png/6.png",
    "vice-cream-227533": "/assets/trophies/png/7.png",
    "vice-cream-428029": "/assets/trophies/png/8.png",
    "chill-flame-303522": "/assets/trophies/png/9.png",
    "vice-cream-10": "/assets/trophies/png/10.png",
}

FILL_COLORS = [
    "#FF8080",
    "#FFCA73",
    "#FBFFA5",
    "#7CFF80",
    "#7EFFF2",
    "#84D0FF",
    "#8290FF",
    "#CD81FF",
    "#FF80D0",
    "#FDFDFD",
    "#FF0000",
    "#FF9D00",
    "#F2FF00",
    "#00FF07",
    "#00FFE6",
    "#009DFF",
    "#001EFF",
    "#9900FF",
    "#FF00A1",
    "#8A8A8A",
    "#870000",
    "#8D4E00",
    "#B6A700",
    "#009904",
    "#009687",
    "#00568C",
    "#001194",
    "#53008A",
    "#8E005A",
    "#000000",
]

database = redis.from_url(
    os.getenv("REDIS_URL", "redis://redis:6379/0"), decode_responses=True
)
pending_actions: dict[tuple[int, int], Any] = {}
pending_prompt_messages: dict[tuple[int, int], int] = {}


class TelegramAPIError(RuntimeError):
    def __init__(self, retry_after: float = 0, description: str = "") -> None:
        super().__init__(description or "Telegram request failed")
        self.retry_after = retry_after
        self.description = description


def call(method: str, payload: dict[str, Any]) -> dict[str, Any]:
    response = requests.post(f"{API}/{method}", json=payload, timeout=40)
    try:
        result = response.json()
    except ValueError as exc:
        raise RuntimeError("Telegram returned an invalid response") from None
    if not response.ok or not result.get("ok"):
        parameters = result.get("parameters", {})
        retry_after = parameters.get("retry_after", 0) if isinstance(parameters, dict) else 0
        raise TelegramAPIError(
            float(retry_after) if retry_after else 0,
            str(result.get("description", "")),
        )
    return result


def send_message(
    chat_id: int,
    text: str,
    reply_markup: dict | None = None,
    delete_after: float | None = None,
    message_thread_id: int | None = None,
) -> int | None:
    payload: dict[str, Any] = {"chat_id": chat_id, "text": text}
    if reply_markup:
        payload["reply_markup"] = reply_markup
    if message_thread_id:
        payload["message_thread_id"] = message_thread_id
    result = call("sendMessage", payload)
    message_id = result.get("result", {}).get("message_id")
    if message_id and delete_after:
        schedule_delete(chat_id, int(message_id), delete_after)
    return int(message_id) if message_id else None


def send_photo(
    chat_id: int,
    image_url: str,
    filename: str,
    caption: str,
    reply_markup: dict | None = None,
) -> dict[str, Any]:
    image_response = requests.get(image_url, timeout=40)
    image_response.raise_for_status()
    data = {
        "chat_id": str(chat_id),
        "caption": caption,
    }
    if reply_markup:
        data["reply_markup"] = json.dumps(reply_markup, ensure_ascii=False)
    response = requests.post(
        f"{API}/sendPhoto",
        data=data,
        files={"photo": (filename, image_response.content, "image/png")},
        timeout=40,
    )
    try:
        result = response.json()
    except ValueError as exc:
        raise RuntimeError("Telegram returned an invalid response") from None
    if not response.ok or not result.get("ok"):
        raise RuntimeError("Telegram request failed")
    return result


def send_document(chat_id: int, filename: str, content: bytes, caption: str = "") -> None:
    response = requests.post(
        f"{API}/sendDocument",
        data={"chat_id": str(chat_id), "caption": caption},
        files={"document": (filename, content, "video/mp4")},
        timeout=120,
    )
    try:
        result = response.json()
    except ValueError:
        raise RuntimeError("Telegram returned an invalid response") from None
    if not response.ok or not result.get("ok"):
        raise RuntimeError("Telegram request failed")


def send_welcome(chat_id: int) -> None:
    call(
        "sendPhoto",
        {
            "chat_id": chat_id,
            "photo": f"{APP_URL}/assets/main.png",
            "caption": "Присоединяйся к битве!",
            "reply_markup": {
                "inline_keyboard": [
                    [{"text": "Открыть Pixel Battle", "web_app": {"url": APP_URL}}]
                ]
            },
        },
    )


def send_welcome(chat_id: int) -> None:
    markup = {
        "inline_keyboard": [
            [
                {
                    "text": "Открыть Pixel Battle",
                    "url": APP_LINK,
                }
            ]
        ]
    }
    send_photo(
        chat_id,
        f"{APP_URL}/assets/main.png",
        "main.png",
        "Присоединяйся к битве!",
        markup,
    )


def admins_bypass_enabled() -> bool:
    return all(database.sismember(BYPASS_KEY, str(admin_id)) for admin_id in ADMIN_IDS)


def admin_markup() -> dict:
    admins_button = (
        "Вернуть КД админам" if admins_bypass_enabled() else "Убрать КД у админов"
    )
    pause_button = (
        "Продолжить игру" if database.get(GAME_PAUSED_KEY) else "Приостановить игру"
    )
    return {
        "inline_keyboard": [
            [{"text": pause_button, "callback_data": "admin:toggle_pause"}],
            [{"text": admins_button, "callback_data": "admin:toggle_admin_cooldown"}],
            [{"text": "Персональный рейтлимит", "callback_data": "admin:rate_limit"}],
            [{"text": "Общий рейтлимит", "callback_data": "admin:global_rate_limit"}],
            [{"text": "Онлайн и пик", "callback_data": "admin:online"}],
            [{"text": "Убрать задержку", "callback_data": "admin:grant"}],
            [{"text": "Вернуть задержку", "callback_data": "admin:revoke"}],
            [{"text": "Список исключений", "callback_data": "admin:list"}],
            [{"text": "Изменить размер карты", "callback_data": "admin:resize"}],
            [{"text": "Очистить карту", "callback_data": "admin:clear"}],
            [{"text": "Опубликовать карту в группе", "callback_data": "admin:map"}],
        ]
    }


def admin_menu(chat_id: int) -> None:
    send_message(chat_id, "Админ-панель Pixel Battle", admin_markup())


def send_admin_photo(
    chat_id: int, image: str, caption: str, reply_markup: dict
) -> None:
    menu_key = f"{ADMIN_MENU_MESSAGE_KEY_PREFIX}{chat_id}"
    previous_message_id = database.get(menu_key)
    if previous_message_id:
        delete_message(chat_id, int(previous_message_id))
    result = send_photo(
        chat_id, f"{APP_URL}/assets/{image}", image, caption, reply_markup
    )
    message_id = result.get("result", {}).get("message_id")
    if message_id:
        database.set(menu_key, str(message_id))


def admin_markup() -> dict:
    return {
        "inline_keyboard": [
            [{"text": "Лимит", "callback_data": "admin:category:limit"}],
            [{"text": "Карта", "callback_data": "admin:category:map"}],
            [{"text": "Игра", "callback_data": "admin:category:game"}],
        ]
    }


def admin_menu(chat_id: int) -> None:
    send_admin_photo(chat_id, "main.png", "Админ-панель Pixel Battle", admin_markup())


def admin_category(chat_id: int, category: str, notice: str | None = None) -> None:
    back = {"text": "Назад", "callback_data": "admin:menu"}
    suffix = f"\n\n{notice}" if notice else ""
    if category == "limit":
        markup = {
            "inline_keyboard": [
                [
                    {
                        "text": "Персональный рейтлимит",
                        "callback_data": "admin:rate_limit",
                    }
                ],
                [
                    {
                        "text": "Общий рейтлимит",
                        "callback_data": "admin:global_rate_limit",
                    }
                ],
                [
                    {
                        "text": "Лимит администраторов",
                        "callback_data": "admin:toggle_admin_cooldown",
                    }
                ],
                [{"text": "Список исключений", "callback_data": "admin:list"}],
                [back],
            ]
        }
        send_admin_photo(chat_id, "limit.png", f"Настройки лимитов{suffix}", markup)
    elif category == "map":
        rows = [
            [{"text": "Изменить размер карты", "callback_data": "admin:resize"}],
            [{"text": "Заполнить область", "callback_data": "admin:fill"}],
            [{"text": "Очистить карту", "callback_data": "admin:clear"}],
            [{"text": "Опубликовать карту в группе", "callback_data": "admin:map"}],
        ]
        rows.insert(
            2,
            [{"text": "Добавить изображение на карту", "callback_data": "admin:image"}],
        )
        if database.get(MAP_CLEAR_BACKUP_KEY):
            rows.append(
                [
                    {
                        "text": "Вернуть очищенную карту",
                        "callback_data": "admin:clear_restore",
                    }
                ]
            )
        rows.append([back])
        markup = {"inline_keyboard": rows}
        send_admin_photo(chat_id, "map.png", f"Настройки карты{suffix}", markup)
    elif category == "game":
        pause_button = (
            "Продолжить игру" if database.get(GAME_PAUSED_KEY) else "Приостановить игру"
        )
        test_mode_button = (
            "Выключить режим теста"
            if database.get(TEST_MODE_KEY)
            else "Включить режим теста"
        )
        markup = {
            "inline_keyboard": [
                [{"text": pause_button, "callback_data": "admin:toggle_pause"}],
                [{"text": test_mode_button, "callback_data": "admin:toggle_test_mode"}],
                [{"text": "Онлайн и пик", "callback_data": "admin:online"}],
                [{"text": "Запись игры", "callback_data": "admin:recording"}],
                [{"text": "Сбросить весь прогресс", "callback_data": "admin:reset_all"}],
                [{"text": "Включить капчу игроку", "callback_data": "admin:captcha:user"}],
                [{"text": "Трофеи", "callback_data": "admin:trophies"}],
                [{"text": "Выдать бомбы", "callback_data": "admin:items:bomb"}],
                [{"text": "Выдать заморозки", "callback_data": "admin:items:ice"}],
                [
                    {
                        "text": "Сбросить квесты игроку",
                        "callback_data": "admin:quests:user",
                    }
                ],
                [{"text": "Сбросить квесты всем", "callback_data": "admin:quests:all"}],
                [back],
            ]
        }
        send_admin_photo(chat_id, "game.png", f"Настройки игры{suffix}", markup)


def admin_trophies(chat_id: int, notice: str | None = None) -> None:
    try:
        response = realtime_request("GET", "/api/admin/trophies/drop")
        response.raise_for_status()
        state = response.json()
        online = max(0, int(state["online"]))
        chance = max(0.0, float(state["chancePercent"]))
        nft_claimed = max(0, int(state["nftClaimed"]))
        nft_planned = max(0, int(state["nftPlanned"]))
        plan_remaining = max(0, int(state["planRemainingSeconds"]))
        days, day_remainder = divmod(plan_remaining, 86400)
        hours = day_remainder // 3600
        force_status = (
            "Следующий подходящий пиксель гарантированно выдаст часть."
            if bool(state.get("forcedNext"))
            else "Принудительный дроп не включён."
        )
        status = (
            f"Шанс на текущий пиксель: {chance:.3f}%\n"
            f"Онлайн: {online}\n"
            f"NFT-план: {nft_claimed}/{nft_planned}\n"
            f"До завершения плана: {days} д. {hours} ч.\n"
            f"{force_status}"
        )
    except (requests.RequestException, KeyError, TypeError, ValueError):
        status = "Не удалось получить состояние дропа."
    suffix = f"\n\n{notice}" if notice else ""
    markup = {
        "inline_keyboard": [
            [
                {
                    "text": "Гарантировать дроп (1 раз)",
                    "callback_data": "admin:trophies:ready",
                }
            ],
            [
                {
                    "text": "Обнулить трофеи игрока",
                    "callback_data": "admin:trophies:reset",
                }
            ],
            [{"text": "Обновить", "callback_data": "admin:trophies"}],
            [{"text": "Назад", "callback_data": "admin:category:game"}],
        ]
    }
    send_admin_photo(
        chat_id,
        "game.png",
        f"Настройки трофеев\n\n{status}{suffix}",
        markup,
    )


def recording_status() -> dict[str, Any]:
    response = realtime_request("GET", "/api/admin/recording")
    response.raise_for_status()
    payload = response.json()
    return payload if isinstance(payload, dict) else {}


def admin_recording(chat_id: int, notice: str | None = None) -> None:
    try:
        state = recording_status()
        active = bool(state.get("active"))
        events = int(state.get("events", 0))
        status = f"Статус: {'идёт запись' if active else 'запись выключена'}\nКадровых событий: {events}"
    except (requests.RequestException, TypeError, ValueError):
        active = False
        status = "Не удалось получить состояние записи."
    suffix = f"\n\n{notice}" if notice else ""
    action = (
        {"text": "Остановить и получить MP4", "callback_data": "admin:recording:stop"}
        if active
        else {"text": "Начать запись", "callback_data": "admin:recording:start"}
    )
    markup = {
        "inline_keyboard": [
            [action],
            [{"text": "Обновить", "callback_data": "admin:recording"}],
            [{"text": "Назад", "callback_data": "admin:category:game"}],
        ]
    }
    send_admin_photo(chat_id, "game.png", f"Запись игры\n\n{status}{suffix}", markup)


def start_recording(chat_id: int) -> None:
    try:
        response = realtime_request("POST", "/api/admin/recording/start")
        response.raise_for_status()
        admin_recording(chat_id, "Запись поля начата.")
    except requests.RequestException:
        admin_recording(chat_id, "Не удалось начать запись.")

def confirm_reset_all(chat_id: int) -> None:
    send_admin_photo(
        chat_id,
        "game.png",
        "ВНИМАНИЕ\n\nЭто обнулит рейтинг, опыт, бомбы, заморозки и все трофеи у всех пользователей, а карту сделает белой. Действие необратимо.",
        {"inline_keyboard": [[{"text": "Да, сбросить всё", "callback_data": "admin:reset_all_confirm"}], [{"text": "Отмена", "callback_data": "admin:category:game"}]]},
    )

def reset_all_progress(chat_id: int) -> None:
    try:
        response = realtime_request("POST", "/api/admin/reset-all")
        response.raise_for_status()
        admin_category(chat_id, "game", "Весь прогресс пользователей обнулён, карта очищена.")
    except requests.RequestException:
        admin_category(chat_id, "game", "Не удалось выполнить полный сброс.")


def stop_recording(chat_id: int) -> None:
    try:
        response = realtime_request("POST", "/api/admin/recording/stop", timeout=600)
        response.raise_for_status()
        try:
            send_document(chat_id, "pixelbattle-recording.mp4", response.content, "Запись поля PixelBattle")
        except RuntimeError:
            retry = realtime_request("POST", "/api/admin/recording/stop", timeout=600)
            retry.raise_for_status()
            send_document(chat_id, "pixelbattle-recording.mp4", retry.content, "Запись поля PixelBattle")
        admin_recording(chat_id, "Запись остановлена, MP4 отправлен.")
    except (requests.RequestException, RuntimeError):
        admin_recording(chat_id, "Не удалось экспортировать запись в MP4.")


def realtime_request(method: str, path: str, **kwargs: Any) -> requests.Response:
    if not ADMIN_API_TOKEN:
        raise requests.RequestException("Admin API credentials are not configured")
    headers = dict(kwargs.pop("headers", {}))
    headers["Authorization"] = f"Bearer {ADMIN_API_TOKEN}"
    timeout = kwargs.pop("timeout", 30)
    return requests.request(
        method,
        f"{REALTIME_URL}{path}",
        headers=headers,
        timeout=timeout,
        allow_redirects=False,
        **kwargs,
    )


def reset_daily_quests(user_id: str | None = None) -> None:
    payload = {"userId": user_id} if user_id else {"all": True}
    response = realtime_request("POST", "/api/admin/quests/reset", json=payload)
    response.raise_for_status()


def reset_player_trophies(user_id: int) -> bool:
    response = realtime_request(
        "POST", "/api/admin/trophies/reset", json={"userId": str(user_id)}
    )
    if response.status_code == 404:
        return False
    response.raise_for_status()
    return True


def grant_item(user_id: int, item: str, amount: int) -> dict[str, Any]:
    response = realtime_request(
        "POST",
        "/api/admin/items/grant",
        json={"userId": str(user_id), "item": item, "amount": amount},
    )
    response.raise_for_status()
    return response.json()


def require_player_captcha(user_id: int) -> None:
    response = realtime_request(
        "POST", "/api/admin/captcha/require", json={"userId": str(user_id)}
    )
    response.raise_for_status()


def fetch_captcha_statuses() -> list[dict[str, Any]]:
    response = realtime_request("GET", "/api/admin/captcha/statuses")
    response.raise_for_status()
    payload = response.json()
    players = payload.get("players", []) if isinstance(payload, dict) else []
    return players if isinstance(players, list) else []


def penalize_pending_captcha(user_id: str) -> tuple[int, int]:
    response = realtime_request(
        "POST", "/api/admin/captcha/penalize", json={"userId": user_id}
    )
    response.raise_for_status()
    payload = response.json()
    return int(payload.get("strikeCount", 0)), int(payload.get("penaltySeconds", 0))


def edit_captcha_message(message_id: int, text: str) -> None:
    call(
        "editMessageText",
        {"chat_id": CAPTCHA_ALERT_CHAT_ID, "message_id": message_id, "text": text},
    )


def send_captcha_topic_message(text: str) -> int | None:
    topic_key = NOTIFICATION_TOPICS["captcha"][1]
    for attempt in range(2):
        try:
            return send_message(
                CAPTCHA_ALERT_CHAT_ID,
                text,
                message_thread_id=notification_topic_id("captcha"),
            )
        except TelegramAPIError as exc:
            description = exc.description.lower()
            if attempt == 0 and ("thread" in description or "topic" in description):
                database.delete(topic_key)
                continue
            raise
    return None


def ensure_captcha_topic_visible() -> None:
    topic_id = notification_topic_id("captcha")
    if not database.get(CAPTCHA_TOPIC_POINTER_KEY):
        internal_chat_id = str(abs(CAPTCHA_ALERT_CHAT_ID)).removeprefix("100")
        pointer_id = send_message(
            CAPTCHA_ALERT_CHAT_ID,
            "🤖 Античит Pixel Battle: подтверждённые боты и их штрафы находятся в отдельном топике.",
            reply_markup={
                "inline_keyboard": [[{
                    "text": "Открыть топик «Боты»",
                    "url": f"https://t.me/c/{internal_chat_id}/{topic_id}",
                }]]
            },
        )
        if pointer_id is not None:
            database.set(CAPTCHA_TOPIC_POINTER_KEY, str(pointer_id))
    if database.get(CAPTCHA_TOPIC_ANNOUNCEMENT_KEY):
        return
    message_id = send_captcha_topic_message(
        "🤖 Подтверждённые боты Pixel Battle отслеживаются в этом топике."
    )
    if message_id is not None:
        database.set(CAPTCHA_TOPIC_ANNOUNCEMENT_KEY, str(message_id))


def notify_admins_about_captcha_statuses() -> None:
    try:
        ensure_captcha_topic_visible()
    except (RuntimeError, requests.RequestException):
        print("captcha topic announcement failed", flush=True)
    players = fetch_captcha_statuses()
    reported_ids = {
        str(player.get("userId", ""))
        for player in players
        if isinstance(player, dict)
    }
    # Confirmed bots must remain visible after a realtime restart even though
    # live CAPTCHA review state is intentionally in-memory.
    for confirmed_id in CONFIRMED_BOT_USERNAMES:
        if confirmed_id in reported_ids:
            continue
        raw_strikes = database.hget(CAPTCHA_PENALTY_KEY, confirmed_id)
        strikes = int(raw_strikes) if raw_strikes and str(raw_strikes).isdigit() else 0
        players.append({
            "userId": confirmed_id,
            "status": "clean",
            "updatedAt": "confirmed-bot",
            "strikeCount": strikes,
            "penaltySeconds": strikes * 5,
            "online": False,
        })
    for player in players:
        if not isinstance(player, dict):
            continue
        user_id = str(player.get("userId", ""))
        status = player.get("status")
        updated_at = str(player.get("updatedAt", ""))
        if not user_id.isdigit() or status not in {"clean", "suspicious"} or not updated_at:
            continue
        if status == "suspicious":
            try:
                status_changed_at = datetime.fromisoformat(
                    updated_at.replace("Z", "+00:00")
                ).timestamp()
            except ValueError:
                continue
            if time.time() - status_changed_at < CAPTCHA_SUSPICION_GRACE_SECONDS:
                continue
            if not player.get("online"):
                continue
        confirmed_bot = user_id in CONFIRMED_BOT_USERNAMES
        username = database.hget(USER_ID_KEY, user_id) or CONFIRMED_BOT_USERNAMES.get(user_id)
        label = f"@{username} (ID {user_id})" if username else f"ID {user_id}"
        marker = f"v2:{status}:{updated_at}"
        notification_id = f"{CAPTCHA_ALERT_CHAT_ID}:{user_id}"
        if database.hget(CAPTCHA_NOTIFICATION_KEY, notification_id) == marker:
            continue
        stored_message_id = database.hget(CAPTCHA_NOTIFICATION_MESSAGE_KEY, notification_id)
        message_id = int(stored_message_id) if stored_message_id and str(stored_message_id).isdigit() else None

        if status == "clean" and not confirmed_bot:
            if message_id is None:
                database.hset(CAPTCHA_NOTIFICATION_KEY, notification_id, marker)
                continue
            message = f"✅ {label} прошёл капчу."
        elif status == "clean":
            strike_count = max(0, int(player.get("strikeCount", 0)))
            penalty_seconds = max(0, int(player.get("penaltySeconds", 0)))
            multiplier = f" x{strike_count}" if strike_count > 1 else ""
            message = f"🤖 {label} — подтверждённый бот{multiplier}\n+{penalty_seconds} секунд к задержке"
        else:
            try:
                strike_count, penalty_seconds = penalize_pending_captcha(user_id)
            except (RuntimeError, requests.RequestException, ValueError, TypeError):
                print(
                    f"captcha penalty failed: user={user_id}",
                    flush=True,
                )
                continue
            multiplier = f" x{strike_count}" if strike_count > 1 else ""
            message = f"🤖 {label} — бот{multiplier}\n+{penalty_seconds} секунд к задержке"
        try:
            if message_id is not None:
                try:
                    edit_captcha_message(message_id, message)
                except TelegramAPIError as exc:
                    if "message is not modified" not in exc.description.lower():
                        message_id = send_captcha_topic_message(message)
            else:
                message_id = send_captcha_topic_message(message)
        except (RuntimeError, requests.RequestException):
            print(
                f"captcha notification failed: chat={CAPTCHA_ALERT_CHAT_ID} user={user_id} status={status}",
                flush=True,
            )
            continue
        if message_id is not None:
            database.hset(CAPTCHA_NOTIFICATION_MESSAGE_KEY, notification_id, str(message_id))
        database.hset(CAPTCHA_NOTIFICATION_KEY, notification_id, marker)
        print(
            f"captcha notification sent: chat={CAPTCHA_ALERT_CHAT_ID} user={user_id} status={status}",
            flush=True,
        )


def captcha_notification_loop() -> None:
    while True:
        try:
            notify_admins_about_captcha_statuses()
        except Exception as exc:
            print(
                f"captcha notification loop error: {type(exc).__name__}", flush=True
            )
        time.sleep(CAPTCHA_NOTIFICATION_POLL_SECONDS)


def fetch_trophy_reward_requests() -> list[dict[str, Any]]:
    response = realtime_request("GET", "/api/admin/trophy-reward-requests")
    response.raise_for_status()
    payload = response.json()
    requests_list = payload.get("requests", []) if isinstance(payload, dict) else []
    return requests_list if isinstance(requests_list, list) else []


def notify_admins_about_trophy_reward_requests() -> None:
    for request in fetch_trophy_reward_requests():
        if not isinstance(request, dict):
            continue
        request_id = request.get("requestId")
        user_id = str(request.get("userId", ""))
        trophy_id = str(request.get("trophyId", ""))
        trophy_name = str(request.get("trophyName", "трофей"))
        source = str(request.get("source", ""))
        if not isinstance(request_id, int) or request_id <= 0 or not user_id.isdigit() or not trophy_id:
            continue
        username = database.hget(USER_ID_KEY, user_id)
        label = f"@{username} (ID {user_id})" if username else f"ID {user_id}"
        if source == "NFT":
            message = f"🏆 {label} запросил получение NFT."
        elif trophy_id == "bear-redjex":
            message = f"🐻 {label} запросил мишку-redjex от redjex."
        elif trophy_id == "bear":
            message = f"🐻 {label} запросил мишку от Идейного Аниматора."
        else:
            message = f"🎁 {label} запросил получение: {trophy_name}."
        delivered = True
        for admin_id in ADMIN_IDS:
            try:
                send_message(admin_id, message)
            except (RuntimeError, requests.RequestException):
                delivered = False
                print(f"trophy reward notification failed: request={request_id} admin={admin_id}", flush=True)
        if delivered:
            try:
                response = realtime_request("POST", "/api/admin/trophy-reward-requests/ack", json={"requestId": request_id})
                response.raise_for_status()
            except requests.RequestException:
                print(f"trophy reward notification ack failed: request={request_id}", flush=True)


def trophy_reward_notification_loop() -> None:
    while True:
        try:
            notify_admins_about_trophy_reward_requests()
        except Exception as exc:
            print(f"trophy reward notification loop error: {type(exc).__name__}", flush=True)
        time.sleep(REWARD_REQUEST_POLL_SECONDS)


def notification_topic_id(category: str) -> int:
    topic = NOTIFICATION_TOPICS.get(category)
    if not topic:
        raise RuntimeError("Unknown trophy notification category")
    topic_name, topic_key = topic
    chat_id = CAPTCHA_ALERT_CHAT_ID if category == "captcha" else TROPHY_ALERT_CHAT_ID
    cached = database.get(topic_key)
    if cached and str(cached).isdigit():
        return int(cached)
    chat = call("getChat", {"chat_id": chat_id}).get("result", {})
    if not chat.get("is_forum"):
        raise RuntimeError("Notification chat is not a forum")
    result = call(
        "createForumTopic",
        {"chat_id": chat_id, "name": topic_name},
    )
    topic_id = result.get("result", {}).get("message_thread_id")
    if not isinstance(topic_id, int) or topic_id <= 0:
        raise RuntimeError("Telegram returned an invalid forum topic")
    database.set(topic_key, str(topic_id))
    print(
        f"notification forum topic created: category={category} chat={chat_id} topic={topic_id}",
        flush=True,
    )
    return topic_id


def trophy_topic_id() -> int:
    return notification_topic_id("trophy")


def fetch_trophy_chat_notifications() -> list[dict[str, Any]]:
    response = realtime_request(
        "GET", "/api/admin/trophy-chat-notifications?limit=20"
    )
    response.raise_for_status()
    payload = response.json()
    notifications = payload.get("notifications", []) if isinstance(payload, dict) else []
    return notifications if isinstance(notifications, list) else []


def trophy_completed_time(value: str) -> str:
    try:
        completed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        local = completed.astimezone(timezone(timedelta(hours=5)))
        return local.strftime("%d.%m.%Y в %H:%M (UTC+5)")
    except (ValueError, OSError, OverflowError):
        return value


def trophy_notification_caption(notification: dict[str, Any]) -> str:
    display_name = str(notification.get("displayName", "")).strip()
    username = str(notification.get("username", "")).strip().lstrip("@")
    player = display_name or "Игрок"
    if username:
        player = f"{player} (@{username})"
    trophy_name = str(notification.get("trophyName", "Трофей")).strip() or "Трофей"
    completed_at = trophy_completed_time(str(notification.get("completedAt", "")))
    return f"🏆 {trophy_name}\nПолучил: {player}\nВремя: {completed_at}"


def notify_trophy_chat() -> None:
    notifications = fetch_trophy_chat_notifications()
    if not notifications:
        return
    for notification in notifications:
        if not isinstance(notification, dict):
            continue
        notification_id = str(notification.get("notificationId", ""))
        category = str(notification.get("category", ""))
        trophy_id = str(notification.get("trophyId", ""))
        user_id = str(notification.get("userId", ""))
        if category not in NOTIFICATION_TOPICS or not notification_id or not trophy_id or not user_id:
            continue
        topic_id = notification_topic_id(category)
        image_path = TROPHY_IMAGE_PATHS.get(trophy_id, "/assets/main.png")
        try:
            call(
                "sendPhoto",
                {
                    "chat_id": TROPHY_ALERT_CHAT_ID,
                    "message_thread_id": topic_id,
                    "photo": f"{APP_URL}{image_path}",
                    "caption": trophy_notification_caption(notification),
                },
            )
            response = realtime_request(
                "POST",
                "/api/admin/trophy-chat-notifications/ack",
                json={"notificationId": notification_id},
            )
            response.raise_for_status()
            print(
                f"trophy chat notification sent: trophy={trophy_id} user={user_id}",
                flush=True,
            )
        except TelegramAPIError as exc:
            if exc.retry_after:
                time.sleep(exc.retry_after + 1)
            raise
        time.sleep(TROPHY_NOTIFICATION_INTERVAL_SECONDS)


def trophy_chat_notification_loop() -> None:
    while True:
        retry_delay = TROPHY_NOTIFICATION_POLL_SECONDS
        try:
            notify_trophy_chat()
        except (RuntimeError, requests.RequestException) as exc:
            print(
                f"trophy chat notification loop error: {type(exc).__name__}",
                flush=True,
            )
            retry_delay = 300
        time.sleep(retry_delay)


def render_map() -> bytes:
    response = realtime_request("GET", "/api/boards/main/image")
    response.raise_for_status()
    return response.content


def map_markup() -> dict:
    return {
        "inline_keyboard": [
            [
                {"text": "Обновить", "callback_data": "map:refresh"},
                # Telegram does not allow web_app buttons in group messages.
                {"text": "Открыть карту", "url": APP_LINK},
            ]
        ]
    }


def inline_map_result() -> dict[str, Any]:
    image_version = int(time.time() // 10)
    image_url = f"{APP_URL}/inline-map.jpg?v={image_version}"
    return {
        "type": "photo",
        "id": f"pixel-battle-map-{image_version}",
        "photo_url": image_url,
        "thumbnail_url": image_url,
        "photo_width": 1200,
        "photo_height": 1200,
        "title": "PIXEL BATTLE",
        "description": "Актуальная карта",
        "caption": "Присоединяйся к битве!",
        "reply_markup": {
            "inline_keyboard": [
                [{"text": "Открыть карту", "url": APP_LINK}],
            ]
        },
    }


def handle_inline_query(inline_query: dict[str, Any]) -> None:
    inline_query_id = inline_query.get("id")
    if not inline_query_id:
        return
    call(
        "answerInlineQuery",
        {
            "inline_query_id": inline_query_id,
            "results": [inline_map_result()],
            "cache_time": 3,
            "is_personal": False,
        },
    )


def delete_message(chat_id: int, message_id: int) -> None:
    try:
        call("deleteMessage", {"chat_id": chat_id, "message_id": message_id})
    except Exception:
        pass


def schedule_delete(chat_id: int, message_id: int, delay: float = 5) -> None:
    timer = threading.Timer(delay, delete_message, args=(chat_id, message_id))
    timer.daemon = True
    timer.start()


def send_admin_prompt(admin_id: int, chat_id: int, text: str) -> None:
    previous_message_id = pending_prompt_messages.pop((admin_id, chat_id), None)
    if previous_message_id:
        schedule_delete(chat_id, previous_message_id)
    message_id = send_message(chat_id, text)
    if message_id:
        pending_prompt_messages[(admin_id, chat_id)] = message_id


def allow_map(chat_id: int) -> bool:
    global map_global_next
    now = time.monotonic()
    with map_lock:
        if now < map_global_next or now < map_requests.get(chat_id, 0):
            return False
        if len(map_requests) >= 4096:
            expired = [key for key, expiry in map_requests.items() if expiry <= now]
            for key in expired:
                del map_requests[key]
            if len(map_requests) >= 4096:
                return False
        map_requests[chat_id] = now + MAP_THROTTLE_SECONDS
        map_global_next = now + 1
        return True


def send_map(chat_id: int) -> bool:
    if not allow_map(chat_id):
        return False
    previous_message_id = database.get(f"{MAP_MESSAGE_KEY_PREFIX}{chat_id}")
    if previous_message_id:
        delete_message(chat_id, int(previous_message_id))
    image = render_map()
    response = requests.post(
        f"{API}/sendPhoto",
        data={
            "chat_id": str(chat_id),
            "caption": "Pixel Battle — актуальная карта",
            "reply_markup": json.dumps(map_markup(), ensure_ascii=False),
        },
        files={"photo": ("pixelbattle.png", image, "image/png")},
        timeout=40,
    )
    response.raise_for_status()
    result = response.json()
    message_id = result.get("result", {}).get("message_id")
    if message_id:
        database.set(f"{MAP_MESSAGE_KEY_PREFIX}{chat_id}", str(message_id))
    return True


def edit_map_message(chat_id: int, message_id: int, image: bytes) -> None:
    media = json.dumps(
        {
            "type": "photo",
            "media": "attach://map",
            "caption": "Pixel Battle — актуальная карта",
        },
        ensure_ascii=False,
    )
    response = requests.post(
        f"{API}/editMessageMedia",
        data={
            "chat_id": str(chat_id),
            "message_id": str(message_id),
            "media": media,
            "reply_markup": json.dumps(map_markup(), ensure_ascii=False),
        },
        files={"map": ("pixelbattle.png", image, "image/png")},
        timeout=40,
    )
    response.raise_for_status()


def send_map_to_saved_groups() -> int:
    sent = 0
    for raw_chat_id in database.smembers(MAP_CHATS_KEY):
        try:
            # Pace broadcasts rather than silently dropping groups at the global limit.
            time.sleep(1)
            if send_map(int(raw_chat_id)):
                sent += 1
        except requests.RequestException:
            continue
    return sent


def refresh_map(callback: dict[str, Any]) -> None:
    message = callback.get("message", {})
    chat_id = message.get("chat", {}).get("id")
    message_id = message.get("message_id")
    if not chat_id or not message_id:
        return
    if not allow_map(chat_id):
        return
    image = render_map()
    edit_map_message(chat_id, int(message_id), image)


def is_group_admin(chat_id: int, user_id: int) -> bool:
    result = call("getChatMember", {"chat_id": chat_id, "user_id": user_id})
    return result.get("result", {}).get("status") in {"creator", "administrator"}


def resolve_user(raw: str) -> tuple[int | None, str]:
    value = raw.strip().split()[0] if raw.strip() else ""
    if value.startswith("@"):
        value = value[1:]
    if value.isdigit():
        username = database.hget(USER_ID_KEY, value)
        return int(value), f"@{username}" if username else value
    normalized = value.lower()
    user_id = database.hget(USERNAME_KEY, normalized)
    return (int(user_id), f"@{normalized}") if user_id else (None, f"@{normalized}")


def set_board_size(text: str) -> tuple[int, int] | None:
    parts = text.lower().replace("×", "x").split("x")
    if len(parts) != 2 or not all(part.strip().isdigit() for part in parts):
        return None
    width, height = (int(part.strip()) for part in parts)
    if not 16 <= width <= 500 or not 16 <= height <= 500:
        return None
    response = realtime_request(
        "PUT", "/api/admin/boards/main/size", json={"width": width, "height": height}
    )
    response.raise_for_status()
    return width, height


def clear_board() -> str:
    response = realtime_request("POST", "/api/admin/boards/main/clear")
    response.raise_for_status()
    backup_id = response.json().get("backupId", "")
    if not backup_id:
        raise RuntimeError("Realtime service did not return a backup ID")
    return backup_id


def restore_board(backup_id: str) -> None:
    response = realtime_request(
        "POST", "/api/admin/boards/main/restore", json={"backupId": backup_id}
    )
    response.raise_for_status()


def begin_fill(admin_id: int, chat_id: int, error: str | None = None) -> None:
    pending_actions[(admin_id, chat_id)] = "fill"
    caption = "Палитра цветов\n\nОтправь две координаты и номер цвета в формате:\n10,20 30,40 5\n\nКоординаты считаются от 0. Область заполняется включительно."
    if error:
        caption = f"{error}\n\n{caption}"
    send_admin_photo(
        chat_id,
        "palette.png",
        caption,
        {
            "inline_keyboard": [
                [{"text": "Отмена", "callback_data": "admin:fill_cancel"}]
            ]
        },
    )


def fill_board(admin_id: int, text: str) -> bool:
    parts = text.replace(";", " ").split()
    if len(parts) != 3:
        return False
    try:
        first = [int(value.strip()) for value in parts[0].split(",")]
        second = [int(value.strip()) for value in parts[1].split(",")]
        color_number = int(parts[2])
    except ValueError:
        return False
    if len(first) != 2 or len(second) != 2 or not 1 <= color_number <= len(FILL_COLORS):
        return False
    response = realtime_request(
        "POST",
        "/api/admin/boards/main/fill",
        json={
            "x1": first[0],
            "y1": first[1],
            "x2": second[0],
            "y2": second[1],
            "color": FILL_COLORS[color_number - 1],
            "adminId": admin_id,
        },
    )
    response.raise_for_status()
    return True


def download_telegram_photo(message: dict[str, Any]) -> bytes | None:
    photos = message.get("photo") or (
        [] if not message.get("document") else [message["document"]]
    )
    if not photos:
        return None
    file_id = photos[-1].get("file_id")
    file_path = call("getFile", {"file_id": file_id}).get("result", {}).get("file_path")
    if not file_path:
        return None
    with requests.get(
        f"https://api.telegram.org/file/bot{TOKEN}/{file_path}",
        timeout=40,
        stream=True,
        allow_redirects=False,
    ) as response:
        response.raise_for_status()
        image = bytearray()
        for chunk in response.iter_content(65536):
            image.extend(chunk)
            if len(image) > 20 * 1024 * 1024:
                return None
        return bytes(image)


def import_image(admin_id: int, text: str, raw_image: bytes) -> int | None:
    parts = text.lower().replace("×", "x").replace(",", " ").split()
    if len(parts) not in {2, 3} or not all(part.isdigit() for part in parts[:2]):
        return None
    x, y = int(parts[0]), int(parts[1])
    image = Image.open(BytesIO(raw_image))
    if image.width * image.height > 16000000:
        return None
    image = image.convert("RGBA")
    if len(parts) == 3:
        size = parts[2].split("x")
        if len(size) != 2 or not all(value.isdigit() for value in size):
            return None
        width, height = int(size[0]), int(size[1])
        if not 1 <= width <= 500 or not 1 <= height <= 500:
            return None
        # Treat every output pixel as one map-grid cell and average the whole
        # source-image area covered by that cell. Unlike Lanczos, BOX does not
        # create coloured ringing around high-contrast black-and-white edges.
        image = image.resize((width, height), Image.Resampling.BOX)
    elif image.width > 500 or image.height > 500:
        return None
    palette = []
    for value in FILL_COLORS:
        red, green, blue = (int(value[index : index + 2], 16) for index in (1, 3, 5))
        hue, saturation, brightness = colorsys.rgb_to_hsv(
            red / 255, green / 255, blue / 255
        )
        palette.append((red, green, blue, value, hue, saturation, brightness))
    neutral_palette = [
        item
        for item in palette
        if max(item[0], item[1], item[2]) - min(item[0], item[1], item[2]) <= 8
    ]
    pixels = []
    for py in range(image.height):
        for px in range(image.width):
            red, green, blue, alpha = image.getpixel((px, py))
            if alpha < 32:
                continue
            hue, saturation, brightness = colorsys.rgb_to_hsv(
                red / 255, green / 255, blue / 255
            )

            def color_distance(
                item: tuple[int, int, int, str, float, float, float],
            ) -> float:
                hue_delta = abs(hue - item[4])
                hue_delta = min(hue_delta, 1 - hue_delta)
                # Preserve hue so a non-purple source cannot become purple just
                # because that RGB value happens to be numerically close.
                hue_penalty = (
                    hue_delta * 180 if saturation > 0.18 and item[5] > 0.18 else 0
                )
                return (
                    (red - item[0]) ** 2
                    + (green - item[1]) ** 2
                    + (blue - item[2]) ** 2
                ) + hue_penalty**2

            # JPEG compression often adds tiny coloured fringes to otherwise
            # black-and-white artwork. Keep those pixels achromatic instead of
            # turning medium grey into a pastel purple or another accent colour.
            chroma = max(red, green, blue) - min(red, green, blue)
            candidates = (
                neutral_palette if chroma <= 24 or saturation <= 0.12 else palette
            )
            nearest = min(candidates, key=color_distance)
            pixels.append({"x": px, "y": py, "color": nearest[3]})
    if not pixels or len(pixels) > 250000:
        return None
    response = realtime_request(
        "POST",
        "/api/admin/boards/main/image",
        json={"x": x, "y": y, "adminId": admin_id, "pixels": pixels},
    )
    response.raise_for_status()
    return int(response.json().get("placed", 0))


def import_image_to_full_board(admin_id: int, raw_image: bytes) -> int | None:
    response = realtime_request("GET", "/api/admin/boards/main/size")
    response.raise_for_status()
    size = response.json()
    return import_image(admin_id, f"0 0 {size['width']}x{size['height']}", raw_image)


def confirm_clear_board(chat_id: int) -> None:
    markup = {
        "inline_keyboard": [
            [{"text": "Да, очистить карту", "callback_data": "admin:clear_confirm"}],
            [{"text": "Отмена", "callback_data": "admin:clear_cancel"}],
        ]
    }
    send_admin_photo(
        chat_id,
        "map.png",
        "Точно очистить всю карту? Перед очисткой будет создана резервная копия.",
        markup,
    )


def set_game_paused(paused: bool) -> None:
    response = realtime_request("PUT", "/api/admin/game/pause", json={"paused": paused})
    response.raise_for_status()


def set_test_mode(enabled: bool) -> None:
    response = realtime_request(
        "PUT", "/api/admin/game/test-mode", json={"enabled": enabled}
    )
    response.raise_for_status()


def send_bypass_list(chat_id: int) -> None:
    ids = sorted(database.smembers(BYPASS_KEY), key=int)
    if not ids:
        send_message(chat_id, "Дополнительных исключений нет.")
        return
    lines = []
    for user_id in ids:
        username = database.hget(USER_ID_KEY, user_id)
        lines.append(f"• {'@' + username if username else 'ID ' + user_id}")
    send_message(chat_id, "Без задержки:\n" + "\n".join(lines))


def set_global_rate_limit(chat_id: int, raw: str) -> bool:
    value = raw.strip().lower()
    if value in {"default", "по умолчанию", "обычный"}:
        database.delete(GLOBAL_RATE_LIMIT_KEY)
        send_message(
            chat_id,
            "Общий рейтлимит возвращён к стандартным 5 секундам.",
            delete_after=5,
        )
        return True
    if not value.isdigit() or not 0 <= int(value) <= 3600:
        send_message(
            chat_id,
            "Формат: /cooldown число от 0 до 3600. 0 — убрать КД, default — вернуть 5 секунд.",
        )
        return False
    seconds = int(value)
    database.set(GLOBAL_RATE_LIMIT_KEY, seconds)
    send_message(chat_id, f"Общий рейтлимит установлен: {seconds} сек.", delete_after=5)
    return True


def set_personal_rate_limit(user_id: str, seconds: int) -> bool:
    pipe = database.pipeline(transaction=True)
    pipe.srem(BYPASS_KEY, user_id)
    pipe.hset(RATE_LIMIT_KEY, user_id, seconds)
    pipe.execute()
    return database.hget(RATE_LIMIT_KEY, user_id) == str(seconds)


def apply_admin_action(admin_id: int, chat_id: int, text: str) -> None:
    pending_key = (admin_id, chat_id)
    action = pending_actions.pop(pending_key, None)
    if isinstance(action, dict) and action.get("action") == "image_coords":
        try:
            placed = import_image(admin_id, text, action["image"])
        except (OSError, ValueError, requests.RequestException):
            placed = None
        if placed is not None:
            admin_category(chat_id, "map", f"На карту добавлено пикселей: {placed}.")
        else:
            pending_actions[pending_key] = action
            send_admin_prompt(
                admin_id,
                chat_id,
                "Не удалось добавить рисунок. Проверь формат x,y ширинаxвысота и границы карты.",
            )
        return
    if action == "global_rate_seconds":
        set_global_rate_limit(chat_id, text)
    elif action == "rate_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            send_message(
                chat_id,
                "Пользователь не найден. Он должен хотя бы один раз открыть Mini App.",
            )
            return
        pending_actions[pending_key] = {
            "action": "rate_seconds",
            "user_id": user_id,
            "label": label,
        }
        send_admin_prompt(
            admin_id,
            chat_id,
            "Отправь кулдаун в секундах от 0 до 3600. 0 — без КД, default — вернуть общий КД.",
        )
    elif isinstance(action, dict) and action.get("action") == "rate_seconds":
        raw = text.strip().lower()
        target_id = str(action["user_id"])
        label = action["label"]
        if raw in {"default", "по умолчанию", "обычный"}:
            database.hdel(RATE_LIMIT_KEY, target_id)
            database.srem(BYPASS_KEY, target_id)
            send_message(
                chat_id, f"Для {label} возвращён общий рейтлимит.", delete_after=5
            )
            return
        if not raw.isdigit() or not 0 <= int(raw) <= 3600:
            send_message(
                chat_id, "Нужно отправить целое число от 0 до 3600 либо default."
            )
            return
        seconds = int(raw)
        set_personal_rate_limit(target_id, seconds)
        send_message(chat_id, f"Рейтлимит для {label}: {seconds} сек.", delete_after=5)
    elif action == "captcha_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            pending_actions[pending_key] = "captcha_user"
            send_admin_prompt(
                admin_id,
                chat_id,
                "Пользователь не найден. Отправь @username или Telegram ID ещё раз.",
            )
            return
        try:
            require_player_captcha(user_id)
            admin_category(chat_id, "game", f"Капча включена для {label}.")
        except requests.RequestException:
            admin_category(
                chat_id,
                "game",
                "Не удалось включить капчу. Проверь доступность сервера.",
            )
    elif action == "quest_reset_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            send_admin_prompt(
                admin_id,
                chat_id,
                "Пользователь не найден. Отправь @username или Telegram ID ещё раз.",
            )
            pending_actions[pending_key] = "quest_reset_user"
            return
        try:
            reset_daily_quests(str(user_id))
            admin_category(chat_id, "game", f"Дневные квесты сброшены для {label}.")
        except requests.RequestException:
            admin_category(
                chat_id,
                "game",
                "Не удалось сбросить квесты. Проверь доступность сервера.",
            )
    elif action == "trophy_reset_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            pending_actions[pending_key] = "trophy_reset_user"
            send_admin_prompt(
                admin_id,
                chat_id,
                "Пользователь не найден. Отправь @username или Telegram ID ещё раз.",
            )
            return
        try:
            if not reset_player_trophies(user_id):
                pending_actions[pending_key] = "trophy_reset_user"
                send_admin_prompt(
                    admin_id,
                    chat_id,
                    "Профиль не найден. Игрок должен хотя бы один раз открыть Mini App. Отправь другого игрока.",
                )
                return
            admin_trophies(chat_id, f"Все трофеи и части обнулены для {label}.")
        except requests.RequestException:
            admin_trophies(
                chat_id, "Не удалось обнулить трофеи. Проверь доступность сервера."
            )
    elif isinstance(action, dict) and action.get("action") == "item_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            pending_actions[pending_key] = action
            send_admin_prompt(
                admin_id,
                chat_id,
                "Пользователь не найден. Отправь @username или Telegram ID ещё раз.",
            )
            return
        pending_actions[pending_key] = {
            "action": "item_amount",
            "item": action["item"],
            "user_id": user_id,
            "label": label,
        }
        send_admin_prompt(
            admin_id, chat_id, "Отправь количество предметов от 1 до 100000."
        )
    elif isinstance(action, dict) and action.get("action") == "item_amount":
        raw = text.strip()
        if not raw.isdigit() or not 1 <= int(raw) <= 100000:
            pending_actions[pending_key] = action
            send_admin_prompt(
                admin_id, chat_id, "Нужно отправить целое число от 1 до 100000."
            )
            return
        amount = int(raw)
        item = action["item"]
        try:
            grant_item(action["user_id"], item, amount)
            item_label = "бомб" if item == "bomb" else "заморозок"
            admin_category(
                chat_id, "game", f"Для {action['label']} выдано {amount} {item_label}."
            )
        except requests.RequestException:
            admin_category(
                chat_id,
                "game",
                "Не удалось выдать предметы. Проверь доступность сервера.",
            )
    elif action == "grant" or action == "revoke":
        user_id, label = resolve_user(text)
        if user_id is None:
            send_message(chat_id, "Пользователь не найден.")
            return
        (database.sadd if action == "grant" else database.srem)(
            BYPASS_KEY, str(user_id)
        )
        send_message(
            chat_id,
            f"Задержка {'отключена' if action == 'grant' else 'возвращена'} для {label}.",
            delete_after=5,
        )
    elif action == "resize":
        try:
            size = set_board_size(text)
        except requests.RequestException:
            size = None
        send_message(
            chat_id,
            (
                f"Размер карты установлен: {size[0]}×{size[1]}."
                if size
                else "Формат: ширинаxвысота, например 200x150. Допустимо от 16 до 500."
            ),
            delete_after=5 if size else None,
        )
    elif action == "fill":
        try:
            filled = fill_board(admin_id, text)
        except requests.RequestException:
            filled = False
        if filled:
            admin_category(chat_id, "map")
        else:
            begin_fill(
                admin_id,
                chat_id,
                "Неверный формат, номер цвета или координаты выходят за границы карты.",
            )


def handle_callback(callback: dict[str, Any]) -> None:
    user_id = callback.get("from", {}).get("id")
    chat_id = callback.get("message", {}).get("chat", {}).get("id")
    call("answerCallbackQuery", {"callback_query_id": callback["id"]})
    action = callback.get("data", "")
    if action == "map:refresh":
        refresh_map(callback)
        return
    if not user_id or not chat_id or user_id not in ADMIN_IDS:
        return
    if action in {"admin:items:bomb", "admin:items:ice"}:
        item = action.rsplit(":", 1)[1]
        pending_actions[(user_id, chat_id)] = {"action": "item_user", "item": item}
        send_admin_prompt(user_id, chat_id, "Отправь @username или Telegram ID игрока.")
        return
    if action == "admin:captcha:user":
        pending_actions[(user_id, chat_id)] = "captcha_user"
        send_admin_prompt(
            user_id,
            chat_id,
            "Отправь @username или Telegram ID игрока, которому нужно включить капчу.",
        )
        return
    if action == "admin:quests:user":
        pending_actions[(user_id, chat_id)] = "quest_reset_user"
        send_admin_prompt(
            user_id,
            chat_id,
            "Отправь @username или Telegram ID игрока, которому нужно сбросить дневные квесты.",
        )
        return
    if action == "admin:quests:all":
        markup = {
            "inline_keyboard": [
                [
                    {
                        "text": "Да, сбросить всем",
                        "callback_data": "admin:quests:all_confirm",
                    }
                ],
                [{"text": "Отмена", "callback_data": "admin:quests:all_cancel"}],
            ]
        }
        send_admin_photo(
            chat_id,
            "game.png",
            "Сбросить дневные квесты абсолютно всем игрокам? Общая статистика сохранится.",
            markup,
        )
        return
    if action == "admin:quests:all_cancel":
        admin_category(chat_id, "game")
        return
    if action == "admin:quests:all_confirm":
        try:
            reset_daily_quests()
            admin_category(chat_id, "game", "Дневные квесты сброшены для всех игроков.")
        except requests.RequestException:
            admin_category(
                chat_id,
                "game",
                "Не удалось сбросить квесты. Проверь доступность сервера.",
            )
        return
    if action == "admin:toggle_pause":
        paused = not bool(database.get(GAME_PAUSED_KEY))
        try:
            set_game_paused(paused)
        except requests.RequestException:
            pass
        admin_category(chat_id, "game")
        return
    if action == "admin:toggle_test_mode":
        enabled = not bool(database.get(TEST_MODE_KEY))
        try:
            set_test_mode(enabled)
            notice = (
                "Режим теста включён. Приложение доступно только администраторам."
                if enabled
                else "Режим теста выключен. Приложение снова доступно всем."
            )
        except requests.RequestException:
            notice = "Не удалось изменить режим теста."
        admin_category(chat_id, "game", notice)
        return
    if action == "admin:trophies":
        admin_trophies(chat_id)
        return
    if action == "admin:trophies:ready":
        try:
            response = realtime_request("POST", "/api/admin/trophies/drop")
            response.raise_for_status()
            admin_trophies(
                chat_id,
                "Следующий подходящий пиксель гарантированно выдаст случайную часть.",
            )
        except requests.RequestException:
            admin_trophies(chat_id, "Не удалось включить гарантированный дроп.")
        return
    if action == "admin:trophies:reset":
        pending_actions[(user_id, chat_id)] = "trophy_reset_user"
        send_admin_prompt(
            user_id,
            chat_id,
            "Отправь @username или Telegram ID игрока, которому нужно обнулить все трофеи.",
        )
        return
    if action == "admin:toggle_admin_cooldown":
        admin_ids = [str(admin_id) for admin_id in ADMIN_IDS]
        if admins_bypass_enabled():
            database.srem(BYPASS_KEY, *admin_ids)
        else:
            database.sadd(BYPASS_KEY, *admin_ids)
        admin_category(chat_id, "limit")
        return
    if action == "admin:online":
        try:
            response = realtime_request("GET", "/api/admin/stats")
            response.raise_for_status()
            stats = response.json()
            notice = f"Сейчас онлайн: {stats['currentOnline']}\nПиковый онлайн: {stats['peakOnline']}"
        except (requests.RequestException, KeyError, ValueError):
            notice = "Не удалось получить статистику онлайна."
        admin_category(chat_id, "game", notice)
        return
    if action == "admin:recording":
        admin_recording(chat_id)
        return
    if action == "admin:reset_all":
        confirm_reset_all(chat_id)
        return
    if action == "admin:reset_all_confirm":
        reset_all_progress(chat_id)
        return
    if action == "admin:recording:start":
        start_recording(chat_id)
        return
    if action == "admin:recording:stop":
        stop_recording(chat_id)
        return
    if action == "admin:list":
        ids = sorted(database.smembers(BYPASS_KEY), key=int)
        if ids:
            labels = []
            for target_id in ids:
                username = database.hget(USER_ID_KEY, target_id)
                labels.append(f"@{username}" if username else f"ID {target_id}")
            notice = "Без задержки:\n" + "\n".join(labels)
        else:
            notice = "Дополнительных исключений нет."
        admin_category(chat_id, "limit", notice)
        return
    if action == "admin:clear":
        confirm_clear_board(chat_id)
        return
    if action == "admin:fill":
        begin_fill(user_id, chat_id)
        return
    if action == "admin:image":
        pending_actions[(user_id, chat_id)] = "image_upload"
        send_admin_prompt(
            user_id,
            chat_id,
            "Отправь изображение сообщением. Затем я попрошу координаты и размер в формате: 10,20 40x30",
        )
        return
    if action == "admin:fill_cancel":
        pending_actions.pop((user_id, chat_id), None)
        admin_category(chat_id, "map")
        return
    if action == "admin:clear_cancel":
        admin_category(chat_id, "map")
        return
    if action == "admin:clear_confirm":
        try:
            backup_id = clear_board()
            database.set(MAP_CLEAR_BACKUP_KEY, backup_id)
            admin_category(chat_id, "map")
        except (requests.RequestException, RuntimeError, ValueError):
            admin_category(
                chat_id, "map", "Не удалось создать резервную копию и очистить карту."
            )
        return
    if action == "admin:clear_restore":
        backup_id = database.get(MAP_CLEAR_BACKUP_KEY)
        if not backup_id:
            admin_category(chat_id, "map")
            return
        try:
            restore_board(backup_id)
            database.delete(MAP_CLEAR_BACKUP_KEY)
            admin_category(chat_id, "map")
        except requests.RequestException:
            admin_category(chat_id, "map", "Не удалось восстановить карту.")
        return
    if action == "admin:map":
        send_map_to_saved_groups()
        admin_category(chat_id, "map")
        return
    if action == "admin:menu":
        admin_menu(chat_id)
    elif action.startswith("admin:category:"):
        admin_category(chat_id, action.rsplit(":", 1)[1])
    elif action == "admin:list":
        send_bypass_list(chat_id)
    elif action == "admin:toggle_pause":
        paused = not bool(database.get(GAME_PAUSED_KEY))
        try:
            set_game_paused(paused)
            send_message(
                chat_id, "Игра приостановлена." if paused else "Игра продолжена."
            )
            admin_menu(chat_id)
        except requests.RequestException:
            send_message(chat_id, "Не удалось изменить состояние игры.")
    elif action == "admin:global_rate_limit":
        pending_actions[(user_id, chat_id)] = "global_rate_seconds"
        current = database.get(GLOBAL_RATE_LIMIT_KEY) or "5"
        send_admin_prompt(
            user_id,
            chat_id,
            f"Сейчас общий КД: {current} сек. Отправь новое значение 0–3600 или default.",
        )
    elif action == "admin:rate_limit":
        pending_actions[(user_id, chat_id)] = "rate_user"
        send_admin_prompt(
            user_id, chat_id, "Отправь @username или Telegram ID пользователя."
        )
    elif action == "admin:online":
        try:
            response = realtime_request("GET", "/api/admin/stats")
            response.raise_for_status()
            stats = response.json()
            send_message(
                chat_id,
                f"Сейчас онлайн: {stats['currentOnline']}\nПиковый онлайн: {stats['peakOnline']}",
            )
        except (requests.RequestException, KeyError, ValueError):
            send_message(chat_id, "Не удалось получить статистику онлайна.")
    elif action == "admin:toggle_admin_cooldown":
        admin_ids = [str(admin_id) for admin_id in ADMIN_IDS]
        if admins_bypass_enabled():
            database.srem(BYPASS_KEY, *admin_ids)
            send_message(chat_id, "КД возвращён всем администраторам.")
        else:
            database.sadd(BYPASS_KEY, *admin_ids)
            send_message(chat_id, "КД отключён для всех администраторов.")
        admin_menu(chat_id)
    elif action in {"admin:grant", "admin:revoke"}:
        pending_actions[(user_id, chat_id)] = action.split(":", 1)[1]
        send_admin_prompt(
            user_id, chat_id, "Отправь @username или Telegram ID пользователя."
        )
    elif action == "admin:resize":
        pending_actions[(user_id, chat_id)] = "resize"
        send_admin_prompt(
            user_id,
            chat_id,
            "Отправь размер карты в формате ширинаxвысота, например 200x150.",
        )
    elif action == "admin:clear":
        clear_board()
        send_message(chat_id, "Карта очищена.")
    elif action == "admin:map":
        sent = send_map_to_saved_groups()
        send_message(chat_id, f"Карта отправлена в групп: {sent}.")


def handle_message(message: dict[str, Any]) -> None:
    chat = message.get("chat", {})
    chat_id = chat.get("id")
    user_id = message.get("from", {}).get("id")
    text = message.get("text", "")
    if not chat_id or not user_id:
        return
    command = text.split()[0] if text.split() else ""
    base_command = command.split("@", 1)[0].lower()
    if user_id not in ADMIN_IDS and base_command != "/start":
        return
    pending_key = (user_id, chat_id)
    if (
        user_id in ADMIN_IDS
        and (
            message.get("photo")
            or (message.get("document", {}).get("mime_type", "").startswith("image/"))
        )
        and pending_actions.get(pending_key) == "image_upload"
    ):
        try:
            raw_image = download_telegram_photo(message)
        except requests.RequestException:
            raw_image = None
        if raw_image:
            try:
                placed = import_image_to_full_board(user_id, raw_image)
            except (OSError, ValueError, KeyError, requests.RequestException):
                placed = None
            if placed is not None:
                pending_actions.pop(pending_key, None)
                send_message(
                    chat_id,
                    f"Рисунок добавлен на всю карту: {placed} пикселей.",
                    delete_after=6,
                )
                admin_category(chat_id, "map")
            else:
                send_admin_prompt(
                    user_id,
                    chat_id,
                    "Не удалось добавить рисунок на всю карту. Проверь, что карта доступна, и отправь изображение ещё раз.",
                )
        else:
            send_admin_prompt(
                user_id,
                chat_id,
                "Не удалось получить изображение. Отправь PNG/JPG ещё раз или нажми отмену в админке.",
            )
        return
    if base_command in {
        "/start",
        "/app",
        "/admin",
        "/pause",
        "/resume",
        "/cooldown",
        "/fill",
    }:
        pending_actions.pop(pending_key, None)
    if text.startswith("/start") or text == "/app":
        send_welcome(chat_id)
    elif text.startswith("/admin"):
        if user_id in ADMIN_IDS:
            admin_menu(chat_id)
        else:
            send_message(chat_id, "Нет доступа к админ-панели.")
    elif base_command == "/fill":
        begin_fill(user_id, chat_id)
    elif command in {"/pause", "/resume"}:
        if user_id not in ADMIN_IDS:
            send_message(chat_id, "Нет доступа к управлению игрой.")
            return
        paused = command == "/pause"
        try:
            set_game_paused(paused)
            send_message(
                chat_id,
                (
                    "Игра приостановлена. Пиксели ставить нельзя."
                    if paused
                    else "Игра продолжена. Пиксели снова можно ставить."
                ),
            )
        except requests.RequestException:
            send_message(chat_id, "Не удалось изменить состояние игры.")
    elif command == "/cooldown":
        if user_id not in ADMIN_IDS:
            send_message(chat_id, "Нет доступа к управлению рейтлимитом.")
            return
        parts = text.split(maxsplit=1)
        if len(parts) != 2:
            current = database.get(GLOBAL_RATE_LIMIT_KEY) or "5"
            send_message(
                chat_id, f"Сейчас общий КД: {current} сек. Использование: /cooldown 10"
            )
            return
        set_global_rate_limit(chat_id, parts[1])
    elif user_id in ADMIN_IDS and pending_key in pending_actions:
        apply_admin_action(user_id, chat_id, text)
        if pending_key not in pending_actions:
            if message.get("message_id"):
                schedule_delete(chat_id, int(message["message_id"]))
            prompt_message_id = pending_prompt_messages.pop(pending_key, None)
            if prompt_message_id:
                schedule_delete(chat_id, prompt_message_id)


def main() -> None:
    if ADMIN_IDS:
        threading.Thread(
            target=captcha_notification_loop,
            name="captcha-admin-notifications",
            daemon=True,
        ).start()
        threading.Thread(
            target=trophy_reward_notification_loop,
            name="trophy-reward-admin-notifications",
            daemon=True,
        ).start()
        threading.Thread(
            target=trophy_chat_notification_loop,
            name="trophy-chat-notifications",
            daemon=True,
        ).start()
    offset = 0
    while True:
        try:
            result = call(
                "getUpdates",
                {
                    "offset": offset,
                    "timeout": 25,
                    "allowed_updates": ["message", "callback_query", "inline_query"],
                },
            )
            for update in result.get("result", []):
                offset = update["update_id"] + 1
                if "callback_query" in update:
                    handle_callback(update["callback_query"])
                elif "inline_query" in update:
                    handle_inline_query(update["inline_query"])
                elif "message" in update:
                    handle_message(update["message"])
        except Exception:
            # Requests exceptions can contain Telegram token-bearing URLs.
            print("bot loop error: update processing failed", flush=True)
            time.sleep(5)


if __name__ == "__main__":
    main()
