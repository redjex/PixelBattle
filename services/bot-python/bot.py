import json
import hashlib
import os
import threading
import time
import colorsys
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
    raise RuntimeError("MINI_APP_LINK must be a Telegram Mini App deep link starting with https://t.me/")
REALTIME_URL = os.getenv("REALTIME_URL", "http://realtime:8080").rstrip("/")
ADMIN_API_TOKEN = os.getenv("ADMIN_API_TOKEN", "")
ADMIN_IDS = {743086174, 6997207264}
BYPASS_KEY = "pixelbattle:cooldown:bypass"
RATE_LIMIT_KEY = "pixelbattle:cooldown:seconds"
GLOBAL_RATE_LIMIT_KEY = "pixelbattle:cooldown:global_seconds"
GAME_PAUSED_KEY = "pixelbattle:game:paused"
USERNAME_KEY = "pixelbattle:users:username"
USER_ID_KEY = "pixelbattle:users:id"
MAP_CHATS_KEY = "pixelbattle:map:chats"
MAP_MESSAGE_KEY_PREFIX = "pixelbattle:map:message:"
ADMIN_MENU_MESSAGE_KEY_PREFIX = "pixelbattle:admin:menu:message:"
MAP_CLEAR_BACKUP_KEY = "pixelbattle:map:last_clear_backup"
FILL_COLORS = [
    "#FF8080", "#FFCA73", "#FBFFA5", "#7CFF80", "#7EFFF2", "#84D0FF", "#8290FF", "#CD81FF", "#FF80D0", "#FDFDFD",
    "#FF0000", "#FF9D00", "#F2FF00", "#00FF07", "#00FFE6", "#009DFF", "#001EFF", "#9900FF", "#FF00A1", "#8A8A8A",
    "#870000", "#8D4E00", "#B6A700", "#009904", "#009687", "#00568C", "#001194", "#53008A", "#8E005A", "#000000",
]

database = redis.from_url(os.getenv("REDIS_URL", "redis://redis:6379/0"), decode_responses=True)
pending_actions: dict[tuple[int, int], Any] = {}
pending_prompt_messages: dict[tuple[int, int], int] = {}


def call(method: str, payload: dict[str, Any]) -> dict[str, Any]:
    response = requests.post(f"{API}/{method}", json=payload, timeout=40)
    try:
        result = response.json()
    except ValueError as exc:
        raise RuntimeError(f"Telegram {method}: HTTP {response.status_code}: {response.text}") from exc
    if not response.ok or not result.get("ok"):
        raise RuntimeError(f"Telegram {method}: HTTP {response.status_code}: {result.get('description', response.text)}")
    return result


def send_message(chat_id: int, text: str, reply_markup: dict | None = None, delete_after: float | None = None) -> int | None:
    payload: dict[str, Any] = {"chat_id": chat_id, "text": text}
    if reply_markup:
        payload["reply_markup"] = reply_markup
    result = call("sendMessage", payload)
    message_id = result.get("result", {}).get("message_id")
    if message_id and delete_after:
        schedule_delete(chat_id, int(message_id), delete_after)
    return int(message_id) if message_id else None


def send_photo(chat_id: int, image_url: str, filename: str, caption: str, reply_markup: dict | None = None) -> dict[str, Any]:
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
        raise RuntimeError(f"Telegram sendPhoto: HTTP {response.status_code}: {response.text}") from exc
    if not response.ok or not result.get("ok"):
        raise RuntimeError(f"Telegram sendPhoto: HTTP {response.status_code}: {result.get('description', response.text)}")
    return result


def send_welcome(chat_id: int) -> None:
    call("sendPhoto", {"chat_id": chat_id, "photo": f"{APP_URL}/assets/main.png", "caption": "Pixel Battle — присоединяйся к битве!", "reply_markup": {"inline_keyboard": [[{"text": "Открыть Pixel Battle", "web_app": {"url": APP_URL}}]]}})


def send_welcome(chat_id: int) -> None:
    markup = {"inline_keyboard": [[{
        "text": "Открыть Pixel Battle",
        "url": APP_LINK,
    }]]}
    send_photo(
        chat_id,
        f"{APP_URL}/assets/main.png",
        "main.png",
        "Pixel Battle — присоединяйся к битве!",
        markup,
    )


def admins_bypass_enabled() -> bool:
    return all(database.sismember(BYPASS_KEY, str(admin_id)) for admin_id in ADMIN_IDS)


def admin_markup() -> dict:
    admins_button = "Вернуть КД админам" if admins_bypass_enabled() else "Убрать КД у админов"
    pause_button = "Продолжить игру" if database.get(GAME_PAUSED_KEY) else "Приостановить игру"
    return {"inline_keyboard": [
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
    ]}


def admin_menu(chat_id: int) -> None:
    send_message(chat_id, "Админ-панель Pixel Battle", admin_markup())


def send_admin_photo(chat_id: int, image: str, caption: str, reply_markup: dict) -> None:
    menu_key = f"{ADMIN_MENU_MESSAGE_KEY_PREFIX}{chat_id}"
    previous_message_id = database.get(menu_key)
    if previous_message_id:
        delete_message(chat_id, int(previous_message_id))
    result = send_photo(chat_id, f"{APP_URL}/assets/{image}", image, caption, reply_markup)
    message_id = result.get("result", {}).get("message_id")
    if message_id:
        database.set(menu_key, str(message_id))


def admin_markup() -> dict:
    return {"inline_keyboard": [
        [{"text": "Лимит", "callback_data": "admin:category:limit"}],
        [{"text": "Карта", "callback_data": "admin:category:map"}],
        [{"text": "Игра", "callback_data": "admin:category:game"}],
    ]}


def admin_menu(chat_id: int) -> None:
    send_admin_photo(chat_id, "main.png", "Админ-панель Pixel Battle", admin_markup())


def admin_category(chat_id: int, category: str, notice: str | None = None) -> None:
    back = {"text": "Назад", "callback_data": "admin:menu"}
    suffix = f"\n\n{notice}" if notice else ""
    if category == "limit":
        markup = {"inline_keyboard": [
            [{"text": "Персональный рейтлимит", "callback_data": "admin:rate_limit"}],
            [{"text": "Общий рейтлимит", "callback_data": "admin:global_rate_limit"}],
            [{"text": "Лимит администраторов", "callback_data": "admin:toggle_admin_cooldown"}],
            [{"text": "Список исключений", "callback_data": "admin:list"}],
            [back],
        ]}
        send_admin_photo(chat_id, "limit.png", f"Настройки лимитов{suffix}", markup)
    elif category == "map":
        rows = [
            [{"text": "Изменить размер карты", "callback_data": "admin:resize"}],
            [{"text": "Заполнить область", "callback_data": "admin:fill"}],
            [{"text": "Очистить карту", "callback_data": "admin:clear"}],
            [{"text": "Опубликовать карту в группе", "callback_data": "admin:map"}],
        ]
        rows.insert(2, [{"text": "Добавить изображение на карту", "callback_data": "admin:image"}])
        if database.get(MAP_CLEAR_BACKUP_KEY):
            rows.append([{"text": "Вернуть очищенную карту", "callback_data": "admin:clear_restore"}])
        rows.append([back])
        markup = {"inline_keyboard": rows}
        send_admin_photo(chat_id, "map.png", f"Настройки карты{suffix}", markup)
    elif category == "game":
        pause_button = "Продолжить игру" if database.get(GAME_PAUSED_KEY) else "Приостановить игру"
        markup = {"inline_keyboard": [
            [{"text": pause_button, "callback_data": "admin:toggle_pause"}],
            [{"text": "Онлайн и пик", "callback_data": "admin:online"}],
            [{"text": "Выдать бомбы", "callback_data": "admin:items:bomb"}],
            [{"text": "Выдать заморозки", "callback_data": "admin:items:ice"}],
            [{"text": "Сбросить квесты игроку", "callback_data": "admin:quests:user"}],
            [{"text": "Сбросить квесты всем", "callback_data": "admin:quests:all"}],
            [back],
        ]}
        send_admin_photo(chat_id, "game.png", f"Настройки игры{suffix}", markup)


def realtime_request(method: str, path: str, **kwargs: Any) -> requests.Response:
    params = kwargs.pop("params", {})
    params["adminToken"] = ADMIN_API_TOKEN
    return requests.request(method, f"{REALTIME_URL}{path}", params=params, timeout=30, **kwargs)


def reset_daily_quests(user_id: str | None = None) -> None:
    payload = {"userId": user_id} if user_id else {"all": True}
    response = realtime_request("POST", "/api/admin/quests/reset", json=payload)
    response.raise_for_status()


def grant_item(user_id: int, item: str, amount: int) -> dict[str, Any]:
    response = realtime_request("POST", "/api/admin/items/grant", json={"userId": str(user_id), "item": item, "amount": amount})
    response.raise_for_status()
    return response.json()


def render_map() -> bytes:
    response = realtime_request("GET", "/api/boards/main/image")
    response.raise_for_status()
    return response.content


def map_markup() -> dict:
    return {"inline_keyboard": [[
        {"text": "Обновить", "callback_data": "map:refresh"},
        # Telegram does not allow web_app buttons in group messages.
        {"text": "Открыть карту", "url": APP_LINK},
    ]]}


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


def send_map(chat_id: int) -> None:
    previous_message_id = database.get(f"{MAP_MESSAGE_KEY_PREFIX}{chat_id}")
    if previous_message_id:
        delete_message(chat_id, int(previous_message_id))
    image = render_map()
    response = requests.post(f"{API}/sendPhoto", data={"chat_id": str(chat_id), "caption": "Pixel Battle — актуальная карта", "reply_markup": json.dumps(map_markup(), ensure_ascii=False)}, files={"photo": ("pixelbattle.png", image, "image/png")}, timeout=40)
    response.raise_for_status()
    result = response.json()
    message_id = result.get("result", {}).get("message_id")
    if message_id:
        database.set(f"{MAP_MESSAGE_KEY_PREFIX}{chat_id}", str(message_id))


def edit_map_message(chat_id: int, message_id: int, image: bytes) -> None:
    media = json.dumps({"type": "photo", "media": "attach://map", "caption": "Pixel Battle — актуальная карта"}, ensure_ascii=False)
    response = requests.post(f"{API}/editMessageMedia", data={"chat_id": str(chat_id), "message_id": str(message_id), "media": media, "reply_markup": json.dumps(map_markup(), ensure_ascii=False)}, files={"map": ("pixelbattle.png", image, "image/png")}, timeout=40)
    response.raise_for_status()


def send_map_to_saved_groups() -> int:
    sent = 0
    for raw_chat_id in database.smembers(MAP_CHATS_KEY):
        try:
            send_map(int(raw_chat_id))
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
    image = render_map()
    edit_map_message(chat_id, int(message_id), image)


def is_group_admin(chat_id: int, user_id: int) -> bool:
    result = call("getChatMember", {"chat_id": chat_id, "user_id": user_id})
    return result.get("result", {}).get("status") in {"creator", "administrator"}


def resolve_user(raw: str) -> tuple[int | None, str]:
    value = raw.strip().split()[0] if raw.strip() else ""
    if value.startswith("@"): value = value[1:]
    if value.isdigit():
        username = database.hget(USER_ID_KEY, value)
        return int(value), f"@{username}" if username else value
    normalized = value.lower()
    user_id = database.hget(USERNAME_KEY, normalized)
    return (int(user_id), f"@{normalized}") if user_id else (None, f"@{normalized}")


def set_board_size(text: str) -> tuple[int, int] | None:
    parts = text.lower().replace("×", "x").split("x")
    if len(parts) != 2 or not all(part.strip().isdigit() for part in parts): return None
    width, height = (int(part.strip()) for part in parts)
    if not 16 <= width <= 500 or not 16 <= height <= 500: return None
    response = realtime_request("PUT", "/api/admin/boards/main/size", json={"width": width, "height": height})
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
    response = realtime_request("POST", "/api/admin/boards/main/restore", json={"backupId": backup_id})
    response.raise_for_status()


def begin_fill(admin_id: int, chat_id: int, error: str | None = None) -> None:
    pending_actions[(admin_id, chat_id)] = "fill"
    caption = "Палитра цветов\n\nОтправь две координаты и номер цвета в формате:\n10,20 30,40 5\n\nКоординаты считаются от 0. Область заполняется включительно."
    if error:
        caption = f"{error}\n\n{caption}"
    send_admin_photo(chat_id, "palette.png", caption, {"inline_keyboard": [[{"text": "Отмена", "callback_data": "admin:fill_cancel"}]]})


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
    response = realtime_request("POST", "/api/admin/boards/main/fill", json={
        "x1": first[0], "y1": first[1], "x2": second[0], "y2": second[1],
        "color": FILL_COLORS[color_number - 1], "adminId": admin_id,
    })
    response.raise_for_status()
    return True


def download_telegram_photo(message: dict[str, Any]) -> bytes | None:
    photos = message.get("photo") or ([] if not message.get("document") else [message["document"]])
    if not photos:
        return None
    file_id = photos[-1].get("file_id")
    file_path = call("getFile", {"file_id": file_id}).get("result", {}).get("file_path")
    if not file_path:
        return None
    response = requests.get(f"https://api.telegram.org/file/bot{TOKEN}/{file_path}", timeout=40)
    response.raise_for_status()
    return response.content if len(response.content) <= 20 * 1024 * 1024 else None


def import_image(admin_id: int, text: str, raw_image: bytes) -> int | None:
    parts = text.lower().replace("×", "x").replace(",", " ").split()
    if len(parts) not in {2, 3} or not all(part.isdigit() for part in parts[:2]):
        return None
    x, y = int(parts[0]), int(parts[1])
    image = Image.open(BytesIO(raw_image)).convert("RGBA")
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
        red, green, blue = (int(value[index:index + 2], 16) for index in (1, 3, 5))
        hue, saturation, brightness = colorsys.rgb_to_hsv(red / 255, green / 255, blue / 255)
        palette.append((red, green, blue, value, hue, saturation, brightness))
    neutral_palette = [item for item in palette if max(item[0], item[1], item[2]) - min(item[0], item[1], item[2]) <= 8]
    pixels = []
    for py in range(image.height):
        for px in range(image.width):
            red, green, blue, alpha = image.getpixel((px, py))
            if alpha < 32:
                continue
            hue, saturation, brightness = colorsys.rgb_to_hsv(red / 255, green / 255, blue / 255)
            def color_distance(item: tuple[int, int, int, str, float, float, float]) -> float:
                hue_delta = abs(hue - item[4])
                hue_delta = min(hue_delta, 1 - hue_delta)
                # Preserve hue so a non-purple source cannot become purple just
                # because that RGB value happens to be numerically close.
                hue_penalty = hue_delta * 180 if saturation > 0.18 and item[5] > 0.18 else 0
                return ((red-item[0])**2 + (green-item[1])**2 + (blue-item[2])**2) + hue_penalty**2
            # JPEG compression often adds tiny coloured fringes to otherwise
            # black-and-white artwork. Keep those pixels achromatic instead of
            # turning medium grey into a pastel purple or another accent colour.
            chroma = max(red, green, blue) - min(red, green, blue)
            candidates = neutral_palette if chroma <= 24 or saturation <= 0.12 else palette
            nearest = min(candidates, key=color_distance)
            pixels.append({"x": px, "y": py, "color": nearest[3]})
    if not pixels or len(pixels) > 250000:
        return None
    response = realtime_request("POST", "/api/admin/boards/main/image", json={"x": x, "y": y, "adminId": admin_id, "pixels": pixels})
    response.raise_for_status()
    return int(response.json().get("placed", 0))


def import_image_to_full_board(admin_id: int, raw_image: bytes) -> int | None:
    response = realtime_request("GET", "/api/admin/boards/main/size")
    response.raise_for_status()
    size = response.json()
    return import_image(admin_id, f"0 0 {size['width']}x{size['height']}", raw_image)


def confirm_clear_board(chat_id: int) -> None:
    markup = {"inline_keyboard": [
        [{"text": "Да, очистить карту", "callback_data": "admin:clear_confirm"}],
        [{"text": "Отмена", "callback_data": "admin:clear_cancel"}],
    ]}
    send_admin_photo(chat_id, "map.png", "Точно очистить всю карту? Перед очисткой будет создана резервная копия.", markup)


def set_game_paused(paused: bool) -> None:
    response = realtime_request("PUT", "/api/admin/game/pause", json={"paused": paused})
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
        send_message(chat_id, "Общий рейтлимит возвращён к стандартным 5 секундам.", delete_after=5)
        return True
    if not value.isdigit() or not 0 <= int(value) <= 3600:
        send_message(chat_id, "Формат: /cooldown число от 0 до 3600. 0 — убрать КД, default — вернуть 5 секунд.")
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
            send_admin_prompt(admin_id, chat_id, "Не удалось добавить рисунок. Проверь формат x,y ширинаxвысота и границы карты.")
        return
    if action == "global_rate_seconds":
        set_global_rate_limit(chat_id, text)
    elif action == "rate_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            send_message(chat_id, "Пользователь не найден. Он должен хотя бы один раз открыть Mini App.")
            return
        pending_actions[pending_key] = {"action": "rate_seconds", "user_id": user_id, "label": label}
        send_admin_prompt(admin_id, chat_id, "Отправь кулдаун в секундах от 0 до 3600. 0 — без КД, default — вернуть общий КД.")
    elif isinstance(action, dict) and action.get("action") == "rate_seconds":
        raw = text.strip().lower()
        target_id = str(action["user_id"])
        label = action["label"]
        if raw in {"default", "по умолчанию", "обычный"}:
            database.hdel(RATE_LIMIT_KEY, target_id)
            database.srem(BYPASS_KEY, target_id)
            send_message(chat_id, f"Для {label} возвращён общий рейтлимит.", delete_after=5)
            return
        if not raw.isdigit() or not 0 <= int(raw) <= 3600:
            send_message(chat_id, "Нужно отправить целое число от 0 до 3600 либо default.")
            return
        seconds = int(raw)
        set_personal_rate_limit(target_id, seconds)
        send_message(chat_id, f"Рейтлимит для {label}: {seconds} сек.", delete_after=5)
    elif action == "quest_reset_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            send_admin_prompt(admin_id, chat_id, "Пользователь не найден. Отправь @username или Telegram ID ещё раз.")
            pending_actions[pending_key] = "quest_reset_user"
            return
        try:
            reset_daily_quests(str(user_id))
            admin_category(chat_id, "game", f"Дневные квесты сброшены для {label}.")
        except requests.RequestException:
            admin_category(chat_id, "game", "Не удалось сбросить квесты. Проверь доступность сервера.")
    elif isinstance(action, dict) and action.get("action") == "item_user":
        user_id, label = resolve_user(text)
        if user_id is None:
            pending_actions[pending_key] = action
            send_admin_prompt(admin_id, chat_id, "Пользователь не найден. Отправь @username или Telegram ID ещё раз.")
            return
        pending_actions[pending_key] = {"action": "item_amount", "item": action["item"], "user_id": user_id, "label": label}
        send_admin_prompt(admin_id, chat_id, "Отправь количество предметов от 1 до 100000.")
    elif isinstance(action, dict) and action.get("action") == "item_amount":
        raw = text.strip()
        if not raw.isdigit() or not 1 <= int(raw) <= 100000:
            pending_actions[pending_key] = action
            send_admin_prompt(admin_id, chat_id, "Нужно отправить целое число от 1 до 100000.")
            return
        amount = int(raw)
        item = action["item"]
        try:
            grant_item(action["user_id"], item, amount)
            item_label = "бомб" if item == "bomb" else "заморозок"
            admin_category(chat_id, "game", f"Для {action['label']} выдано {amount} {item_label}.")
        except requests.RequestException:
            admin_category(chat_id, "game", "Не удалось выдать предметы. Проверь доступность сервера.")
    elif action == "grant" or action == "revoke":
        user_id, label = resolve_user(text)
        if user_id is None:
            send_message(chat_id, "Пользователь не найден.")
            return
        (database.sadd if action == "grant" else database.srem)(BYPASS_KEY, str(user_id))
        send_message(chat_id, f"Задержка {'отключена' if action == 'grant' else 'возвращена'} для {label}.", delete_after=5)
    elif action == "resize":
        try: size = set_board_size(text)
        except requests.RequestException: size = None
        send_message(chat_id, f"Размер карты установлен: {size[0]}×{size[1]}." if size else "Формат: ширинаxвысота, например 200x150. Допустимо от 16 до 500.", delete_after=5 if size else None)
    elif action == "fill":
        try:
            filled = fill_board(admin_id, text)
        except requests.RequestException:
            filled = False
        if filled:
            admin_category(chat_id, "map")
        else:
            begin_fill(admin_id, chat_id, "Неверный формат, номер цвета или координаты выходят за границы карты.")


def handle_callback(callback: dict[str, Any]) -> None:
    user_id = callback.get("from", {}).get("id")
    chat_id = callback.get("message", {}).get("chat", {}).get("id")
    call("answerCallbackQuery", {"callback_query_id": callback["id"]})
    action = callback.get("data", "")
    if action == "map:refresh":
        refresh_map(callback)
        return
    if not user_id or not chat_id or user_id not in ADMIN_IDS: return
    if action in {"admin:items:bomb", "admin:items:ice"}:
        item = action.rsplit(":", 1)[1]
        pending_actions[(user_id, chat_id)] = {"action": "item_user", "item": item}
        send_admin_prompt(user_id, chat_id, "Отправь @username или Telegram ID игрока.")
        return
    if action == "admin:quests:user":
        pending_actions[(user_id, chat_id)] = "quest_reset_user"
        send_admin_prompt(user_id, chat_id, "Отправь @username или Telegram ID игрока, которому нужно сбросить дневные квесты.")
        return
    if action == "admin:quests:all":
        markup = {"inline_keyboard": [
            [{"text": "Да, сбросить всем", "callback_data": "admin:quests:all_confirm"}],
            [{"text": "Отмена", "callback_data": "admin:quests:all_cancel"}],
        ]}
        send_admin_photo(chat_id, "game.png", "Сбросить дневные квесты абсолютно всем игрокам? Общая статистика сохранится.", markup)
        return
    if action == "admin:quests:all_cancel":
        admin_category(chat_id, "game")
        return
    if action == "admin:quests:all_confirm":
        try:
            reset_daily_quests()
            admin_category(chat_id, "game", "Дневные квесты сброшены для всех игроков.")
        except requests.RequestException:
            admin_category(chat_id, "game", "Не удалось сбросить квесты. Проверь доступность сервера.")
        return
    if action == "admin:toggle_pause":
        paused = not bool(database.get(GAME_PAUSED_KEY))
        try:
            set_game_paused(paused)
        except requests.RequestException:
            pass
        admin_category(chat_id, "game")
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
        send_admin_prompt(user_id, chat_id, "Отправь изображение сообщением. Затем я попрошу координаты и размер в формате: 10,20 40x30")
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
            admin_category(chat_id, "map", "Не удалось создать резервную копию и очистить карту.")
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
    if action == "admin:menu": admin_menu(chat_id)
    elif action.startswith("admin:category:"): admin_category(chat_id, action.rsplit(":", 1)[1])
    elif action == "admin:list": send_bypass_list(chat_id)
    elif action == "admin:toggle_pause":
        paused = not bool(database.get(GAME_PAUSED_KEY))
        try:
            set_game_paused(paused)
            send_message(chat_id, "Игра приостановлена." if paused else "Игра продолжена.")
            admin_menu(chat_id)
        except requests.RequestException:
            send_message(chat_id, "Не удалось изменить состояние игры.")
    elif action == "admin:global_rate_limit":
        pending_actions[(user_id, chat_id)] = "global_rate_seconds"
        current = database.get(GLOBAL_RATE_LIMIT_KEY) or "5"
        send_admin_prompt(user_id, chat_id, f"Сейчас общий КД: {current} сек. Отправь новое значение 0–3600 или default.")
    elif action == "admin:rate_limit":
        pending_actions[(user_id, chat_id)] = "rate_user"
        send_admin_prompt(user_id, chat_id, "Отправь @username или Telegram ID пользователя.")
    elif action == "admin:online":
        try:
            response = realtime_request("GET", "/api/admin/stats")
            response.raise_for_status()
            stats = response.json()
            send_message(chat_id, f"Сейчас онлайн: {stats['currentOnline']}\nПиковый онлайн: {stats['peakOnline']}")
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
        send_admin_prompt(user_id, chat_id, "Отправь @username или Telegram ID пользователя.")
    elif action == "admin:resize":
        pending_actions[(user_id, chat_id)] = "resize"
        send_admin_prompt(user_id, chat_id, "Отправь размер карты в формате ширинаxвысота, например 200x150.")
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
    if not chat_id or not user_id: return
    command = text.split()[0] if text.split() else ""
    base_command = command.split("@", 1)[0].lower()
    # Everyone may request the current map; management commands stay admin-only.
    if user_id not in ADMIN_IDS and base_command not in {"/start", "/map"}:
        return
    pending_key = (user_id, chat_id)
    if user_id in ADMIN_IDS and (message.get("photo") or (message.get("document", {}).get("mime_type", "").startswith("image/"))) and pending_actions.get(pending_key) == "image_upload":
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
                send_message(chat_id, f"Рисунок добавлен на всю карту: {placed} пикселей.", delete_after=6)
                admin_category(chat_id, "map")
            else:
                send_admin_prompt(user_id, chat_id, "Не удалось добавить рисунок на всю карту. Проверь, что карта доступна, и отправь изображение ещё раз.")
        else:
            send_admin_prompt(user_id, chat_id, "Не удалось получить изображение. Отправь PNG/JPG ещё раз или нажми отмену в админке.")
        return
    if base_command in {"/start", "/app", "/map", "/admin", "/pause", "/resume", "/cooldown", "/fill"}:
        pending_actions.pop(pending_key, None)
    if text.startswith("/start") or text == "/app": send_welcome(chat_id)
    elif base_command == "/map":
        if chat.get("type") == "private" and user_id in ADMIN_IDS:
            sent = send_map_to_saved_groups()
            send_message(chat_id, f"Карта отправлена в групп: {sent}.")
            return
        if chat.get("type") in {"group", "supergroup"}: database.sadd(MAP_CHATS_KEY, str(chat_id))
        if chat.get("type") in {"group", "supergroup"} and message.get("message_id"):
            delete_message(chat_id, int(message["message_id"]))
        try: send_map(chat_id)
        except requests.RequestException: send_message(chat_id, "Не удалось получить карту с сервера.")
    elif text.startswith("/admin"):
        if user_id in ADMIN_IDS: admin_menu(chat_id)
        else: send_message(chat_id, "Нет доступа к админ-панели.")
    elif base_command == "/fill":
        begin_fill(user_id, chat_id)
    elif command in {"/pause", "/resume"}:
        if user_id not in ADMIN_IDS:
            send_message(chat_id, "Нет доступа к управлению игрой.")
            return
        paused = command == "/pause"
        try:
            set_game_paused(paused)
            send_message(chat_id, "Игра приостановлена. Пиксели ставить нельзя." if paused else "Игра продолжена. Пиксели снова можно ставить.")
        except requests.RequestException:
            send_message(chat_id, "Не удалось изменить состояние игры.")
    elif command == "/cooldown":
        if user_id not in ADMIN_IDS:
            send_message(chat_id, "Нет доступа к управлению рейтлимитом.")
            return
        parts = text.split(maxsplit=1)
        if len(parts) != 2:
            current = database.get(GLOBAL_RATE_LIMIT_KEY) or "5"
            send_message(chat_id, f"Сейчас общий КД: {current} сек. Использование: /cooldown 10")
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
    database.ping()
    offset = 0
    while True:
        try:
            result = call("getUpdates", {"offset": offset, "timeout": 25, "allowed_updates": ["message", "callback_query"]})
            for update in result.get("result", []):
                offset = update["update_id"] + 1
                if "callback_query" in update: handle_callback(update["callback_query"])
                elif "message" in update: handle_message(update["message"])
        except Exception as exc:
            print(f"bot loop error: {exc}", flush=True)
            time.sleep(5)


if __name__ == "__main__": main()
