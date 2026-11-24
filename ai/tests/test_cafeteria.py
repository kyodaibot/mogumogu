import json
from datetime import date

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
    return reply(tool_call("get_cafeteria_menu", meal_type="breakfast"))


def submit(dish="breakfast_b", query="q1"):
    return reply(tool_call("submit_reply", reply="中央食堂の朝食です。", meal_type="breakfast",
                           menu_query_id=query, menu_selections=[selection(dish)]))


@pytest.mark.anyio
async def test_menu_uses_private_go_endpoint_and_returns_only_current_refs():
    def handler(request):
        assert request.url.path == "/internal/agent/menu"
        assert request.headers["authorization"] == "Bearer test-token"
        assert dict(request.url.params) == {"date": "2026-10-27", "meal_type": "breakfast"}
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
async def test_tomorrow_follows_the_time_of_asking_and_is_answered_from_todays_menu():
    # The calendar shows today (10/27); "tomorrow" is 10/28 from context.now.
    req = campus_request("明日の昼食は？")
    def handle(request):
        assert dict(request.url.params) == {"date": "2026-10-28", "meal_type": "lunch"}
        data = menu()
        data["menu_date"] = "2026-10-28"
        data["stores"][0]["menu_date"] = "2026-10-27"
        return httpx.Response(200, json=data)
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(handle)) as client:
        result = await run_agent(demo_model(), GoClient(client, "test-token"), req)
    assert len(result.result.menu_selections) == 2
    assert "2026-10-27" in result.result.reply and "まだ更新されていません" in result.result.reply


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


@pytest.mark.anyio
@pytest.mark.parametrize("text, added", [
    ("中央食堂の朝食です。", True),
    ("10/26 のメニューです。本日の提供は未確認です。", False),
    ("10月26日のメニューです。", False),
])
async def test_an_earlier_menus_date_is_added_not_rejected(text, added):
    from mogumogu_ai.cafeteria import MenuQueries
    from mogumogu_ai.schemas import SubmitReply

    data = menu()
    data["stores"][0]["menu_date"] = "2026-10-26"

    class Source:
        async def fetch_menu(self, params):
            return data

    queries = MenuQueries(date(2026, 10, 27), "ja")
    await queries.fetch(Source(), {"meal_type": "breakfast"})
    submitted = SubmitReply.model_validate({"reply": text, "meal_type": "breakfast", "menu_query_id": "q1",
                                            "menu_selections": [selection()]})
    assert queries.check(submitted) is None
    out = queries.disclose(submitted).reply
    assert out == (text + "\n\n※ 10/26 のメニューです。10/27 のメニューはまだ更新されていません。" if added else text)
    # The day's own menu needs no note.
    data["stores"][0]["menu_date"] = "2026-10-27"
    assert queries.disclose(submitted).reply == text


# Fix-ups instead of retries (plans/menu-reply-autofix.md).
def submit_cards(*cards, query="q1", meal="breakfast"):
    args = {"reply": "朝食です。", "meal_type": meal, "menu_selections": list(cards)}
    if query:
        args["menu_query_id"] = query
    return reply(tool_call("submit_reply", **args))


async def run_with(script, data=None):
    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(
        lambda _: httpx.Response(200, json=data or menu()),
    )) as client:
        model = FakeModel(script)
        return await run_agent(model, GoClient(client, "test-token"), campus_request()), model


@pytest.mark.anyio
async def test_missing_query_id_is_filled_from_the_latest_query():
    result, model = await run_with([get_menu(), submit_cards(selection(), query=None)])
    assert len(model.calls) == 2 and not result.retried
    assert result.result.menu_query_id == "q1"


@pytest.mark.anyio
async def test_missing_query_id_without_a_query_is_still_rejected():
    result, _ = await run_with([submit_cards(selection(), query=None) for _ in range(2)])
    assert result.result is None and result.error == "invalid_output"


@pytest.mark.anyio
async def test_breakfast_split_by_plate_is_merged_into_one_card():
    result, model = await run_with([get_menu(), submit_cards(selection("breakfast_a"), selection("breakfast_b"))])
    assert len(model.calls) == 2 and not result.retried
    [card] = result.result.menu_selections
    assert [i.id for i in card.items] == ["breakfast_a", "breakfast_b"] and card.store_ids == ["650111"]


@pytest.mark.anyio
async def test_merged_breakfast_keeps_only_stores_that_serve_every_plate():
    other = {**selection("breakfast_b"), "store_ids": ["650113"]}
    result, model = await run_with([get_menu(), submit_cards(selection("breakfast_a"), other)])
    [card] = result.result.menu_selections
    assert len(model.calls) == 2 and card.store_ids == ["650111"] and len(card.items) == 2


def test_lunch_cards_are_not_merged():
    from mogumogu_ai.cafeteria import MenuQueries
    from mogumogu_ai.schemas import SubmitReply

    lunch = SubmitReply.model_validate({"reply": "昼食です。", "meal_type": "lunch", "menu_query_id": "q1",
                                        "menu_selections": [selection("a"), selection("b")]})
    assert MenuQueries(date(2026, 10, 27)).merge_breakfast(lunch) is lunch


@pytest.mark.anyio
async def test_query_id_without_cards_is_dropped():
    text_only_reply = reply(tool_call("submit_reply", reply="今日は朝食の提供がありません。", menu_query_id="q1"))
    result, model = await run_with([get_menu(), text_only_reply])
    assert len(model.calls) == 2 and result.result.menu_selections == [] and result.result.menu_query_id is None


@pytest.mark.anyio
async def test_a_store_that_lacks_a_dish_is_dropped_from_the_card():
    data = menu()
    data["stores"].append({**data["stores"][0], "id": "650113", "name": "北部食堂", "items": data["stores"][0]["items"][:1]})
    card = {**selection("breakfast_b"), "store_ids": ["650111", "650113", "made-up"]}
    result, model = await run_with([get_menu(), submit_cards(card)], data)
    assert len(model.calls) == 2 and result.result.menu_selections[0].store_ids == ["650111"]


@pytest.mark.anyio
async def test_a_card_no_listed_store_serves_is_still_rejected():
    card = {**selection("breakfast_b"), "store_ids": ["650113"]}
    result, _ = await run_with([get_menu()] + [submit_cards(card) for _ in range(2)])
    assert result.result is None and result.error == "invalid_output"
