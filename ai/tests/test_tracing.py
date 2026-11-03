import time

import pytest
from fastapi.testclient import TestClient
from langchain_core.callbacks import AsyncCallbackHandler

from mogumogu_ai.agent import run_agent
from mogumogu_ai.app import app
from mogumogu_ai.model import FakeModel, reply, tool_call
from mogumogu_ai.schemas import ReplyRequest
from mogumogu_ai.tracing import make_langfuse, trace_config

from .conftest import FakeSource, make_request

pytestmark = pytest.mark.anyio


def answer():
    return reply(tool_call("submit_reply", reply="北部食堂の定食がおすすめです。"))


def traced_request(**kw) -> ReplyRequest:
    return make_request().model_copy(update={"user_id": 42, "tier": "pro", **kw})


@pytest.fixture
def unreachable_langfuse(monkeypatch):
    """Keys set, but Langfuse at a closed port: every export fails."""
    monkeypatch.setenv("LANGFUSE_PUBLIC_KEY", "pk-lf-test")
    monkeypatch.setenv("LANGFUSE_SECRET_KEY", "sk-lf-test")
    monkeypatch.setenv("LANGFUSE_BASE_URL", "http://127.0.0.1:9")
    monkeypatch.setenv("LANGFUSE_TIMEOUT", "1")
    monkeypatch.delenv("LANGFUSE_TRACING_ENVIRONMENT", raising=False)
    client = make_langfuse()
    yield client
    client.shutdown()


def test_environment_is_local_unless_set(monkeypatch, unreachable_langfuse):
    assert unreachable_langfuse._environment == "local"


def test_off_without_keys(monkeypatch):
    monkeypatch.delenv("LANGFUSE_PUBLIC_KEY", raising=False)
    monkeypatch.setenv("LANGFUSE_SECRET_KEY", "sk-lf-test")
    assert make_langfuse() is None


def test_go_sends_who_asked():
    req = ReplyRequest.model_validate({
        **make_request().model_dump(mode="json", by_alias=True), "user_id": 42, "tier": "pro",
    })
    assert (req.user_id, req.tier) == (42, "pro")
    # Older Go builds send neither.
    assert (make_request().user_id, make_request().tier) == (None, None)


def test_trace_has_user_and_tags(unreachable_langfuse):
    config = trace_config(traced_request(), name="eval: a case", tags=["eval-run"])

    assert config["run_name"] == "eval: a case"
    assert config["metadata"] == {
        "langfuse_trace_name": "eval: a case",
        "langfuse_tags": ["eval-run", "ja", "pro"],
        "langfuse_user_id": "42",
    }
    assert type(config["callbacks"][0]).__name__ == "LangchainCallbackHandler"


async def test_run_agent_passes_the_trace_config_to_the_graph():
    seen = []

    class Recorder(AsyncCallbackHandler):
        async def on_chain_start(self, serialized, inputs, *, run_id, parent_run_id=None, **kw):
            if parent_run_id is None:
                seen.append((kw.get("name"), kw.get("metadata")))

    trace = {"callbacks": [Recorder()], "run_name": "reply", "metadata": {"langfuse_user_id": "42"}}
    outcome = await run_agent(FakeModel([answer()]), FakeSource(), traced_request(), None, None, trace)

    assert outcome.result is not None
    ((name, metadata),) = seen
    assert name == "reply" and metadata["langfuse_user_id"] == "42"


def test_reply_works_when_langfuse_is_down(unreachable_langfuse):
    app.state.model = FakeModel([answer()])
    app.state.langfuse = unreachable_langfuse
    try:
        with TestClient(app) as c:
            req = traced_request().model_dump(mode="json", by_alias=True)
            resp = c.post("/internal/reply", json=req)
            assert resp.status_code == 200
            started = time.monotonic()
        # Shutdown gave up on the queued trace instead of hanging.
        assert time.monotonic() - started < 10
    finally:
        del app.state.model, app.state.langfuse
