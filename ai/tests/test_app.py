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
    assert "result=error" in caplog.text
    assert "秘密のメッセージ" not in caplog.text


def test_log_has_steps_and_tokens_but_no_content(client, caplog):
    client(demo_model()).post("/internal/reply", json=body("秘密のメッセージ 何を食べたらいい？"))

    assert "result=ok" in caplog.text
    assert "steps=1" in caplog.text and "input_tokens=100" in caplog.text
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
