import json

import httpx
import pytest
from langchain_core.messages import ToolMessage

from mogumogu_ai.agent import run_agent
from mogumogu_ai.demo import demo_model
from mogumogu_ai.model import FakeModel, reply, tool_call
from mogumogu_ai.schemas import Turn
from mogumogu_ai.tools import GoClient

from .conftest import make_request
from .test_agent import RECS


def campus_request(message="今日の朝食は何を食べたらいい？", **kwargs):
    req = make_request(message, **kwargs)
    return type(req).model_validate({
        **req.model_dump(), "context": {**req.context.model_dump(), "campus": "yoshida"},
    })


def menu(items=None):
    return {
        "query_id": "q1", "status": "ready", "menu_date": "2026-10-27",
        "stores": [{"id": "650111", "name": "中央食堂", "campus": "yoshida", "hours": ["08:00–10:00"],
                    "items": items if items is not None else [
                        {"id": "breakfast_a", "name": "朝 A", "allergens": [{"ja": "卵", "en": "egg"}], "price_yen": 330,
                         "nutrition": {"calories": 525, "protein_g": None, "fat_g": 10, "carbs_g": 78}},
                        {"id": "breakfast_b", "name": "朝 B", "allergens": [], "price_yen": 330,
                         "nutrition": {"calories": 490, "protein_g": 18, "fat_g": 11, "carbs_g": 70}},
                    ]}],
    }


def selection(dish="breakfast_b"):
    return {"store_ids": ["650111"], "items": [{"id": dish, "portions": 1}], "reason": "授業前の朝食に"}


def get_menu():
    return reply(tool_call("get_cafeteria_menu", meal_type="breakfast", time="08:30"))


def submit(dish="breakfast_b", query="q1"):
    return reply(tool_call("submit_reply", reply="中央食堂の朝食です。", meal_type="breakfast",
                           menu_query_id=query, menu_selections=[selection(dish)]))


@pytest.mark.anyio
async def test_menu_uses_private_go_endpoint_and_returns_only_current_refs():
    def handler(request):
        assert request.url.path == "/internal/agent/menu"
        assert request.headers["authorization"] == "Bearer test-token"
        assert dict(request.url.params) == {"date": "2026-10-27", "meal_type": "breakfast", "time": "08:30"}
        return httpx.Response(200, json=menu())

    model = FakeModel([get_menu(), submit()])
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(handler)) as client:
        result = await run_agent(model, GoClient(client, "test-token"), campus_request(allergens=["egg"]))
    assert result.result is not None and result.result.meal_type == "breakfast"
    assert result.result.menu_selections[0].model_dump() == selection()
    data = json.loads(next(m.content for m in model.calls[1]["messages"] if isinstance(m, ToolMessage)))
    # The model reads the official labels by name, in both languages.
    assert data["stores"][0]["items"][0]["allergens"] == [{"ja": "卵", "en": "egg"}]


@pytest.mark.anyio
@pytest.mark.parametrize("script", [
    [submit(), submit()],
    [get_menu(), submit("not-in-menu"), submit("not-in-menu")],
    [get_menu(), submit(query="previous-turn"), submit(query="previous-turn")],
    [reply(tool_call("submit_reply", reply="fake", meal_type="lunch", recommendations=RECS)) for _ in range(2)],
])
async def test_fabricated_dishes_or_query_and_estimates_cannot_be_sent(script):
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(
        lambda _: httpx.Response(200, json=menu()),
    )) as client:
        result = await run_agent(FakeModel(script), GoClient(client, "test-token"), campus_request())
    assert result.result is None and result.error == "invalid_output"


@pytest.mark.anyio
async def test_demo_also_selects_real_menu_or_reports_empty():
    req = campus_request()
    for items in [menu()["stores"][0]["items"], []]:
        async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(
            lambda _: httpx.Response(200, json=menu(items)),
        )) as client:
            result = await run_agent(demo_model(), GoClient(client, "test-token"), req)
        assert result.result is not None
        cards = result.result.menu_selections
        assert len(cards) == bool(items)
        if items:
            assert [item.id for item in cards[0].items] == ["breakfast_a", "breakfast_b"]
        assert result.result.recommendations == []


def test_short_cafeteria_history_survives_followup():
    turn = Turn(date="2026-10-27", message="朝食は？", reply="中央食堂です。",
                recommendations=[{"name": "朝食プレート", "description": "中央食堂"}])
    assert len(turn.recommendations) == 1


def test_prompt_tells_the_model_to_compare_labels_with_the_users_words():
    from mogumogu_ai.prompt import SYSTEM_PROMPT

    assert "official allergen labels" in SYSTEM_PROMPT and "ja and en" in SYSTEM_PROMPT


def test_active_submit_schema_only_offers_menu_choices():
    from mogumogu_ai.tools import tools_for

    fields = tools_for(False, allow_menu=True)[-1].args.model_json_schema()["properties"]
    assert "menu_selections" in fields and "recommendations" not in fields


@pytest.mark.anyio
async def test_reply_with_two_stores_shares_one_selection():
    data = menu()
    data["stores"].append({**data["stores"][0], "id": "650113", "name": "北部食堂"})
    result_choice = {**selection(), "store_ids": ["650111", "650113"]}
    model = FakeModel([
        reply(tool_call("get_cafeteria_menu", meal_type="lunch")),
        reply(tool_call("submit_reply", reply="二つの食堂で提供されます。", meal_type="lunch",
                        menu_query_id="q1", menu_selections=[result_choice])),
    ])
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(
        lambda _: httpx.Response(200, json=data),
    )) as client:
        result = await run_agent(model, GoClient(client, "test-token"), campus_request("昼食は？"))
    assert result.result.menu_selections[0].model_dump() == result_choice


def test_menu_reply_cross_service_fixture():
    from pathlib import Path
    from mogumogu_ai.schemas import Reply

    data = json.loads((Path(__file__).resolve().parents[2] / "contract/cafeteria-reply.json").read_text())
    response = Reply.model_validate(data)
    assert response.model_dump(mode="json") == data
    assert [i.id for i in response.menu_selections[0].items] == ["breakfast_a", "breakfast_b"]


@pytest.mark.anyio
async def test_new_empty_query_invalidates_original_scope():
    results = iter([menu(), {**menu([]), "query_id": "q2", "status": "no_candidates"}])
    model = FakeModel([get_menu(), get_menu(), submit(), submit()])
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(
        lambda _: httpx.Response(200, json=next(results)),
    )) as client:
        result = await run_agent(model, GoClient(client, "test-token"), campus_request())
    assert result.result is None and result.error == "invalid_output"


@pytest.mark.anyio
async def test_explicit_tomorrow_reference_keeps_target_date_for_go():
    req = campus_request("Please use today's menu as a reference for tomorrow's lunch.", locale="en")
    req.context.target_date = req.context.target_date.replace(day=28)
    def handle(request):
        assert dict(request.url.params) == {"date": "2026-10-28", "meal_type": "lunch", "use_today": "true"}
        return httpx.Response(200, json=menu())
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(handle)) as client:
        result = await run_agent(demo_model(), GoClient(client, "test-token"), req)
    assert len(result.result.menu_selections) == 2
    assert "2026-10-27" in result.result.reply


@pytest.mark.anyio
async def test_combination_requires_overlapping_offered_windows_at_each_store():
    dishes = [
        {"id": "a", "name": "A", "windows": [{"start": "11:00", "end": "12:00"}], "allergens": []},
        {"id": "b", "name": "B", "windows": [{"start": "12:00", "end": "14:00"}], "allergens": []},
    ]
    bad_combo = {**selection(), "items": [{"id": "a", "portions": 1}, {"id": "b", "portions": 1}]}
    choice = lambda combo: reply(tool_call("submit_reply", meal_type="lunch", menu_query_id="q1",
                                           menu_selections=[combo], reply="昼食です。"))
    model = FakeModel([reply(tool_call("get_cafeteria_menu", meal_type="lunch")), choice(bad_combo),
                       choice(selection("a"))])
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(
        lambda _: httpx.Response(200, json=menu(dishes)),
    )) as client:
        result = await run_agent(model, GoClient(client, "test-token"), campus_request("昼食は？"))
    assert result.retried and result.result.menu_selections[0].model_dump() == selection("a")
