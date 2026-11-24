import json

import httpx2 as httpx
import pytest
from langchain_core.messages import HumanMessage, ToolMessage

from mogumogu_ai.agent import SYSTEM
from mogumogu_ai.cafeteria import GET_MENU
from evals.claude_api import ClaudeModel
from mogumogu_ai.tools import SUBMIT_REPLY


@pytest.mark.anyio
async def test_direct_messages_api_menu_lookup_then_forced_submission(monkeypatch):
    monkeypatch.setenv("ANTHROPIC_API_KEY", "local-test")
    monkeypatch.setenv("ANTHROPIC_API_URL", "https://api.anthropic.com")
    monkeypatch.setenv("AWS_PROFILE", "no-such-eval-profile")
    requests = []

    async def send(client, request, **kwargs):
        assert request.url.host == "api.anthropic.com"
        assert request.url.path == "/v1/messages"
        data = json.loads(request.content)
        requests.append(data)
        first = len(requests) == 1
        tool = {"type": "tool_use", "id": f"toolu_{len(requests)}",
                "name": "get_cafeteria_menu" if first else "submit_reply",
                "input": {"meal_type": "lunch"} if first else {"reply": "中央食堂です。"}}
        return httpx.Response(200, request=request, json={
            "id": "msg_test", "type": "message", "role": "assistant", "model": data["model"],
            "content": [tool], "stop_reason": "tool_use", "stop_sequence": None,
            "usage": {"input_tokens": 30, "output_tokens": 10},
        })

    monkeypatch.setattr(httpx.AsyncClient, "send", send)
    model = ClaudeModel()
    prompts = [SYSTEM, HumanMessage("今日の昼は？")]
    first = await model.ainvoke(prompts, [GET_MENU, SUBMIT_REPLY])
    assert first.tool_calls[0]["name"] == GET_MENU.name
    prompts.extend([first, ToolMessage("中央食堂", tool_call_id=first.tool_calls[0]["id"])])
    last = await model.ainvoke(prompts, [SUBMIT_REPLY], "submit_reply")
    assert last.tool_calls[0]["args"] == {"reply": "中央食堂です。"}
    assert requests[0]["tool_choice"] == {"type": "any"}
    assert requests[1]["tool_choice"] == {"type": "tool", "name": "submit_reply"}
    assert requests[0]["system"][-1]["cache_control"] == {"type": "ephemeral"}
    assert requests[1]["messages"][-1]["content"][0]["tool_use_id"] == "toolu_1"
