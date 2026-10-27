import httpx
import pytest
from fastapi.testclient import TestClient

from mogumogu_ai.app import app
from mogumogu_ai.demo import demo_model
from mogumogu_ai.model import FakeModel, reply, tool_call

from .conftest import make_request


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


def body(message="今天午餐吃什么？"):
    return make_request(message).model_dump(mode="json", by_alias=True)


def test_demo_model_recommends(client):
    resp = client(demo_model()).post("/internal/reply", json=body())

    assert resp.status_code == 200
    data = resp.json()
    assert len(data["recommendations"]) == 3
    assert data["ask_meal_type"] is False


def test_demo_model_asks_for_the_meal(client):
    resp = client(demo_model()).post("/internal/reply", json=body("明天吃什么？"))

    assert resp.json()["ask_meal_type"] is True
    assert resp.json()["recommendations"] == []


def test_demo_model_text_only(client):
    resp = client(demo_model()).post("/internal/reply", json=body("你好"))

    assert resp.status_code == 200
    assert resp.json()["recommendations"] == []


def test_invalid_output_is_503(client):
    bad = lambda: reply(tool_call("submit_reply", reply=""))
    resp = client(FakeModel([bad(), bad()])).post("/internal/reply", json=body())

    assert resp.status_code == 503
    assert resp.json() == {"error": "invalid_output"}


def test_go_failure_is_503_and_not_logged_with_content(client, caplog):
    model = FakeModel([reply(tool_call("get_meals", from_date="2026-10-01", to_date="2026-10-14"))])
    resp = client(model, go_handler=lambda r: httpx.Response(500)).post("/internal/reply", json=body("秘密消息"))

    assert resp.status_code == 503
    assert resp.json() == {"error": "upstream_error"}
    assert "result=error" in caplog.text
    assert "秘密消息" not in caplog.text


def test_log_has_steps_and_tokens_but_no_content(client, caplog):
    client(demo_model()).post("/internal/reply", json=body("秘密消息吃什么"))

    assert "result=ok" in caplog.text
    assert "steps=1" in caplog.text and "input_tokens=100" in caplog.text
    assert "秘密消息" not in caplog.text and "照烧" not in caplog.text


def test_rejects_long_message(client):
    resp = client(demo_model()).post("/internal/reply", json={**body(), "message": "长" * 501})

    assert resp.status_code == 422
