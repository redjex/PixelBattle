import importlib.util
import os
from pathlib import Path
from unittest.mock import Mock, patch

os.environ.update(
    TELEGRAM_BOT_TOKEN="test-secret",
    MINI_APP_LINK="https://t.me/test/app",
    TELEGRAM_ADMIN_IDS="123, 456",
    ADMIN_API_TOKEN="admin-secret",
)
spec = importlib.util.spec_from_file_location(
    "battle_bot", Path(__file__).parents[1] / "bot.py"
)
bot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bot)


def test_admin_configuration():
    assert bot.ADMIN_IDS == {123, 456}


def test_bearer_not_query():
    with patch.object(bot.requests, "request") as request:
        bot.realtime_request("POST", "/api/admin/game/pause", json={"paused": True})
    kwargs = request.call_args.kwargs
    assert kwargs["headers"]["Authorization"] == "Bearer admin-secret"
    assert "params" not in kwargs
    assert kwargs["allow_redirects"] is False
    assert "admin-secret" not in request.call_args.args[1]


def test_test_mode_uses_protected_admin_endpoint():
    response = Mock()
    with patch.object(bot, "realtime_request", return_value=response) as request:
        bot.set_test_mode(True)
    request.assert_called_once_with(
        "PUT", "/api/admin/game/test-mode", json={"enabled": True}
    )
    response.raise_for_status.assert_called_once()


def test_force_captcha_uses_protected_admin_endpoint():
    response = Mock()
    with patch.object(bot, "realtime_request", return_value=response) as request:
        bot.require_player_captcha(789)
    request.assert_called_once_with(
        "POST", "/api/admin/captcha/require", json={"userId": "789"}
    )
    response.raise_for_status.assert_called_once()


def test_captcha_status_changes_are_sent_once_to_alert_group():
    response = Mock()
    response.json.return_value = {
        "players": [
            {
                "userId": "789",
                "status": "suspicious",
                "updatedAt": "2026-09-11T10:00:00Z",
            },
            {
                "userId": "456",
                "status": "clean",
                "updatedAt": "2026-09-11T10:01:00Z",
            },
        ]
    }
    usernames = {"789": "suspect", "456": "human"}
    markers = {}

    def hget(key, field):
        if key == bot.USER_ID_KEY:
            return usernames.get(field)
        return markers.get(field)

    def hset(key, field, value):
        assert key == bot.CAPTCHA_NOTIFICATION_KEY
        markers[field] = value

    with patch.object(bot, "realtime_request", return_value=response) as request, patch.object(
        bot.database, "hget", side_effect=hget
    ), patch.object(bot.database, "hset", side_effect=hset), patch.object(
        bot, "send_message"
    ) as send_message:
        bot.notify_admins_about_captcha_statuses()
        bot.notify_admins_about_captcha_statuses()

    assert request.call_count == 2
    assert send_message.call_count == 2
    send_message.assert_any_call(
        bot.CAPTCHA_ALERT_CHAT_ID,
        "⚠️ @suspect (ID 789) не прошёл капчу — пользователь под подозрением.",
    )
    send_message.assert_any_call(
        bot.CAPTCHA_ALERT_CHAT_ID,
        "✅ @human (ID 456) прошёл капчу — человек чистый.",
    )


def test_suspicious_notification_waits_one_minute():
    response = Mock()
    response.json.return_value = {
        "players": [
            {
                "userId": "789",
                "status": "suspicious",
                "updatedAt": "1970-01-01T00:16:20Z",
            }
        ]
    }
    with patch.object(bot, "realtime_request", return_value=response), patch.object(
        bot.time, "time", return_value=1000
    ), patch.object(bot, "send_message") as send_message:
        bot.notify_admins_about_captcha_statuses()

    send_message.assert_not_called()


def test_map_throttle_bounded():
    bot.map_requests.clear()
    bot.map_global_next = 0
    with patch.object(bot.time, "monotonic", return_value=100):
        assert bot.allow_map(1)
        assert not bot.allow_map(1)
        assert not bot.allow_map(2)
        bot.map_requests.update({i: 200 for i in range(4096)})
        bot.map_global_next = 0
        assert not bot.allow_map(5000)
        assert len(bot.map_requests) == 4096
    with patch.object(bot.time, "monotonic", return_value=201):
        assert bot.allow_map(5000)
        assert len(bot.map_requests) == 1


def test_refresh_throttled_before_render():
    with patch.object(bot, "allow_map", return_value=False), patch.object(
        bot, "render_map"
    ) as render:
        bot.refresh_map({"message": {"chat": {"id": 1}, "message_id": 2}})
        render.assert_not_called()


def test_inline_query_offers_shareable_map_button():
    with patch.object(bot.time, "time", return_value=100), patch.object(
        bot, "call"
    ) as call:
        bot.handle_inline_query({"id": "inline-1", "query": ""})
    method, payload = call.call_args.args
    assert method == "answerInlineQuery"
    assert payload["inline_query_id"] == "inline-1"
    result = payload["results"][0]
    assert result["type"] == "photo"
    assert result["title"] == "PIXEL BATTLE"
    assert result["id"] == "pixel-battle-map-20"
    assert result["photo_url"] == f"{bot.APP_URL}/inline-map.jpg?v=20"
    assert result["photo_width"] == 1200
    assert result["photo_height"] == 1200
    assert result["caption"] == "Присоединяйся к битве!"
    assert result["description"] == "Актуальная карта"
    button = result["reply_markup"]["inline_keyboard"][0][0]
    assert button == {"text": "Открыть карту", "url": bot.APP_LINK}


def test_map_command_is_not_available_to_players():
    with patch.object(bot, "send_map") as send_map, patch.object(
        bot, "send_message"
    ) as send_message:
        bot.handle_message(
            {
                "message_id": 7,
                "from": {"id": 789},
                "chat": {"id": -1001, "type": "supergroup"},
                "text": "/map",
            }
        )
    send_map.assert_not_called()
    send_message.assert_not_called()


def test_non_admin_callback_cannot_mutate():
    with patch.object(bot, "call"), patch.object(
        bot, "set_game_paused"
    ) as pause, patch.object(bot, "set_test_mode") as test_mode:
        bot.handle_callback(
            {
                "id": "callback",
                "from": {"id": 789},
                "message": {"chat": {"id": 789}},
                "data": "admin:toggle_pause",
            }
        )
        pause.assert_not_called()
        bot.handle_callback(
            {
                "id": "callback",
                "from": {"id": 789},
                "message": {"chat": {"id": 789}},
                "data": "admin:toggle_test_mode",
            }
        )
        test_mode.assert_not_called()


def test_loop_does_not_log_url(capsys):
    with patch.object(bot.database, "ping"), patch.object(
        bot, "call", side_effect=RuntimeError("https://telegram/botSECRET")
    ), patch.object(bot.time, "sleep", side_effect=KeyboardInterrupt), patch.object(
        bot.threading, "Thread"
    ):
        try:
            bot.main()
        except KeyboardInterrupt:
            pass
    output = capsys.readouterr().out
    assert "SECRET" not in output
    assert "failed" in output


def test_trophy_topic_is_created_once_and_cached():
    with patch.object(bot.database, "get", return_value=None), patch.object(
        bot.database, "set"
    ) as cache, patch.object(
        bot,
        "call",
        side_effect=[
            {"result": {"is_forum": True}},
            {"result": {"message_thread_id": 77}},
        ],
    ) as call:
        assert bot.trophy_topic_id() == 77
    assert call.call_args_list[0].args == (
        "getChat",
        {"chat_id": bot.TROPHY_ALERT_CHAT_ID},
    )
    assert call.call_args_list[1].args == (
        "createForumTopic",
        {"chat_id": bot.TROPHY_ALERT_CHAT_ID, "name": "Трофеи"},
    )
    cache.assert_called_once_with(bot.TROPHY_TOPIC_KEY, "77")


def test_trophy_topic_is_not_created_until_topics_are_enabled():
    with patch.object(bot.database, "get", return_value=None), patch.object(
        bot, "call", return_value={"result": {"is_forum": False}}
    ) as call:
        try:
            bot.trophy_topic_id()
            assert False, "non-forum chat must be rejected"
        except RuntimeError:
            pass
    call.assert_called_once_with("getChat", {"chat_id": bot.TROPHY_ALERT_CHAT_ID})


def test_trophy_notification_sends_photo_to_topic_and_acknowledges():
    notification = {
        "notificationId": "winner:bear-redjex:789",
        "userId": "789",
        "displayName": "Игрок",
        "username": "player",
        "trophyId": "bear-redjex",
        "trophyName": "Мишка от redjex",
        "completedAt": "2026-09-12T12:34:00Z",
    }
    response = Mock()
    with patch.object(
        bot, "fetch_trophy_chat_notifications", return_value=[notification]
    ), patch.object(bot, "trophy_topic_id", return_value=77), patch.object(
        bot, "call"
    ) as call, patch.object(
        bot, "realtime_request", return_value=response
    ) as realtime, patch.object(bot.time, "sleep"):
        bot.notify_trophy_chat()

    method, payload = call.call_args.args
    assert method == "sendPhoto"
    assert payload["message_thread_id"] == 77
    assert payload["photo"].endswith("/assets/trophies/bear-redjex.png?v=1")
    assert "Игрок (@player)" in payload["caption"]
    assert "12.09.2026 в 17:34 (UTC+5)" in payload["caption"]
    realtime.assert_called_once_with(
        "POST",
        "/api/admin/trophy-chat-notifications/ack",
        json={"notificationId": "winner:bear-redjex:789"},
    )
    response.raise_for_status.assert_called_once()
