import json

import httpx
import pytest
from fastapi.testclient import TestClient

from mogumogu_ai.app import app
from mogumogu_ai.demo import demo_model
from mogumogu_ai.model import FakeModel, reply, tool_call

from .conftest import make_request, turn


@pytest.fixture
def client():
    def use(model, go_handler=None):
        app.state.model = model
        c = TestClient(app)
        c.__enter__()
        if go_handler:
            app.state.http = httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(go_handler))
        clients.append(c)
        return c

    clients = []
    yield use
    for c in clients:
        c.__exit__(None, None, None)
    del app.state.model


def body(message="今日の昼食は何を食べたらいい？"):
    return make_request(message).model_dump(mode="json", by_alias=True)


def test_demo_model_recommends(client):
    resp = client(demo_model()).post("/internal/reply", json=body())

    assert resp.status_code == 200
    data = resp.json()
    assert len(data["recommendations"]) == 3
    assert data["ask_meal_type"] is False
    assert data["meal_type"] == "lunch"
    assert data["recommendations"][0]["estimate"]["serving"]["calories"] == 650


def test_demo_model_asks_for_the_meal(client):
    resp = client(demo_model()).post("/internal/reply", json=body("明日は何を食べたらいい？"))

    assert resp.json()["ask_meal_type"] is True
    assert resp.json()["recommendations"] == []


def test_demo_model_answers_in_the_requested_language(client):
    req = make_request("What should I eat for dinner today?", locale="en").model_dump(mode="json", by_alias=True)
    resp = client(demo_model()).post("/internal/reply", json=req)

    data = resp.json()
    assert len(data["recommendations"]) == 3
    assert data["reply"].startswith("(Test reply)")


def test_demo_model_follows_up_on_the_history(client):
    req = make_request("ご飯なしで", history=[
        turn("明日の夕食は何を食べたらいい？", "3つの案です：", "照り焼きチキン丼", "鮭おにぎりセット", "豆腐と野菜の鍋"),
    ]).model_dump(mode="json", by_alias=True)
    resp = client(demo_model()).post("/internal/reply", json=req)

    data = resp.json()
    assert "照り焼きチキン丼" in data["reply"]
    assert data["meal_type"] == "dinner"
    assert len(data["recommendations"]) == 3


def test_rejects_bad_history(client):
    c = client(demo_model())
    ok = turn("q", "a")
    for history in ([ok] * 10, [turn("q", "a", "x", "y")], [{**ok, "reply": "長" * 1001}]):
        resp = c.post("/internal/reply", json={**body(), "history": history})
        assert resp.status_code == 422, history


def test_demo_model_text_only(client):
    resp = client(demo_model()).post("/internal/reply", json=body("こんにちは"))

    assert resp.status_code == 200
    assert resp.json()["recommendations"] == []


def test_invalid_output_is_503(client):
    bad = lambda: reply(tool_call("submit_reply", reply=""))
    resp = client(FakeModel([bad(), bad()])).post("/internal/reply", json=body())

    assert resp.status_code == 503
    assert resp.json() == {"error": "invalid_output"}


def test_go_failure_is_503_and_not_logged_with_content(client, caplog):
    model = FakeModel([reply(tool_call("get_meals", from_date="2026-10-01", to_date="2026-10-14"))])
    resp = client(model, go_handler=lambda r: httpx.Response(500)).post("/internal/reply", json=body("秘密のメッセージ"))

    assert resp.status_code == 503
    assert resp.json() == {"error": "upstream_error"}
    assert reply_line(caplog)["result"] == "error"
    assert "秘密のメッセージ" not in caplog.text


def reply_line(caplog) -> dict:
    """The one JSON line logged for the message."""
    (line,) = [r.getMessage() for r in caplog.records if r.name == "mogumogu_ai.reply"]
    return json.loads(line)


def test_log_is_one_json_line_without_content(client, caplog):
    client(demo_model()).post("/internal/reply", json=body("秘密のメッセージ 何を食べたらいい？"))

    line = reply_line(caplog)
    assert line["msg"] == "reply" and line["result"] == "ok" and line["finish"] == "submitted"
    assert line["model_calls"] == 1 and line["input_tokens"] == 100
    assert line["model_timeouts"] == line["model_retries"] == line["go_timeouts"] == 0
    assert line["cache_read_tokens"] == line["cache_write_tokens"] == 0
    assert "秘密のメッセージ" not in caplog.text and "照り焼き" not in caplog.text


def test_rejects_long_message(client):
    resp = client(demo_model()).post("/internal/reply", json={**body(), "message": "長" * 501})

    assert resp.status_code == 422


def test_locale_defaults_to_english_and_rejects_others(client):
    c = client(demo_model())
    req = body()
    del req["context"]["locale"]
    assert c.post("/internal/reply", json=req).json()["reply"].startswith("(Test reply)")

    req["context"]["locale"] = "zh"
    assert c.post("/internal/reply", json=req).status_code == 422


def test_demo_model_stages_a_record_for_pro(client):
    req = make_request("昼食にラーメンを食べた", allow_proposal=True).model_dump(mode="json", by_alias=True)
    data = client(demo_model()).post("/internal/reply", json=req).json()

    assert data["reply"].startswith("（テスト回答）変更を用意しました")
    assert data["changes"] == [{"op": "create", "meal": {
        "date": "2026-10-27", "meal_type": "lunch", "content": "ラーメン",
        "nutrition": {"calories": 500, "protein_g": 20, "fat_g": 15, "carbs_g": 70},
    }}]


def test_demo_model_says_recording_is_pro_only(client):
    data = client(demo_model()).post("/internal/reply", json=body("昼食にラーメンを食べた")).json()

    assert "Pro" in data["reply"]
    assert data["changes"] == []


def test_demo_model_remembers_for_pro(client):
    req = make_request("辛いものが苦手って覚えて", allow_memory=True).model_dump(mode="json", by_alias=True)
    data = client(demo_model()).post("/internal/reply", json=req).json()

    assert data["reply"] == "（テスト回答）覚えておきます。"
    assert data["memory_changes"] == [{"op": "create", "content": "辛いものが苦手"}]


def test_demo_model_forgets_every_memory(client):
    memories = [{"id": 3, "content": "辛いものが苦手"}, {"id": 4, "content": "魚が好き"}]
    req = make_request("全部忘れて", allow_memory=True, memories=memories).model_dump(mode="json", by_alias=True)
    data = client(demo_model()).post("/internal/reply", json=req).json()

    assert data["memory_changes"] == [{"op": "delete", "memory_id": 3}, {"op": "delete", "memory_id": 4}]


def test_demo_model_says_memory_is_pro_only(client):
    data = client(demo_model()).post("/internal/reply", json=body("辛いものが苦手って覚えて")).json()

    assert "Pro" in data["reply"]
    assert data["memory_changes"] == []


def test_demo_model_records_a_meal_and_remembers_together(client):
    req = make_request("昼食にラーメンを食べた、辛いものが苦手って覚えて", allow_proposal=True, allow_memory=True)
    data = client(demo_model()).post("/internal/reply", json=req.model_dump(mode="json", by_alias=True)).json()

    assert [c["op"] for c in data["changes"]] == ["create"]
    assert data["memory_changes"] == [{"op": "create", "content": "辛いものが苦手"}]


@pytest.mark.anyio
async def test_dropped_request_stops_the_time_budget(monkeypatch):
    from mogumogu_ai import app as app_module
    from mogumogu_ai.time_budget import TimeBudget

    class Request:
        def __init__(self):
            self.checks = iter([False, False, True])

        async def is_disconnected(self):
            return next(self.checks)

    monkeypatch.setattr(app_module, "DISCONNECT_POLL_S", 0)
    time = TimeBudget()
    await app_module._watch_disconnect(Request(), time)
    assert time.timeout_for("final") is None
