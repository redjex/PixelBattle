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
    with patch.object(bot, "call") as call:
        bot.handle_inline_query({"id": "inline-1", "query": ""})
    method, payload = call.call_args.args
    assert method == "answerInlineQuery"
    assert payload["inline_query_id"] == "inline-1"
    result = payload["results"][0]
    assert result["type"] == "photo"
    assert result["title"] == "PIXEL BATTLE"
    assert result["photo_url"] == f"{bot.APP_URL}/assets/main-inline.jpg"
    assert "caption" not in result
    assert "description" not in result
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
    ), patch.object(bot.time, "sleep", side_effect=KeyboardInterrupt):
        try:
            bot.main()
        except KeyboardInterrupt:
            pass
    output = capsys.readouterr().out
    assert "SECRET" not in output
    assert "failed" in output
