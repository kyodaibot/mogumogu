from datetime import date

import pytest
from langchain_core.messages import AIMessage, ToolMessage

from mogumogu_ai.agent import run_agent
from mogumogu_ai.model import FakeModel, reply, tool_call

from .conftest import FakeSource, make_request

pytestmark = pytest.mark.anyio

RECS = [
    {"name": "鸡胸肉便当", "description": "鸡胸肉配糙米", "reason": "下午有 3 小时的课，需要耐饿"},
    {"name": "牛肉乌冬面", "description": "牛肉片和温泉蛋", "reason": "补充蛋白质，适合增肌"},
    {"name": "三文鱼饭团", "description": "两个饭团加沙拉", "reason": "可以在便利店快速买到"},
]


def submit(**args):
    return tool_call("submit_reply", reply="这是三个方案：", recommendations=RECS, **args)


def bad_submit():
    # Two recommendations: neither 0 nor 3.
    return tool_call("submit_reply", reply="方案：", recommendations=RECS[:2])


def query(name="get_meals", from_date="2026-10-01", to_date="2026-10-14"):
    return tool_call(name, from_date=from_date, to_date=to_date)


async def run(script, message="今天午餐吃什么？", allergens=None, source=None):
    model = FakeModel(script)
    source = source or FakeSource()
    outcome = await run_agent(model, source, make_request(message, allergens))
    return outcome, model, source


async def test_submit_directly():
    outcome, model, source = await run([reply(submit())])

    assert outcome.error is None
    assert [r.name for r in outcome.result.recommendations] == [r["name"] for r in RECS]
    assert outcome.steps == 1
    assert (outcome.input_tokens, outcome.output_tokens) == (100, 20)
    assert source.calls == []
    # The first call may use any tool; the context and message are in the prompt.
    assert model.calls[0]["force_tool"] is None
    first = model.calls[0]["messages"][1].content
    assert "<message>\n今天午餐吃什么？\n</message>" in first
    assert '"target_date":"2026-10-27"' in first


async def test_query_then_submit():
    text_only = tool_call("submit_reply", reply="根据你 10/01 到 10/14 的记录，吃了 2 次鱼。")
    outcome, model, source = await run(
        [reply(query(), query("get_events")), reply(text_only)],
        message="我这两周吃了几次鱼",
        source=FakeSource([{"id": 1, "date": "2026-10-03", "meal_type": "dinner", "content": "烤鱼"}]),
    )

    assert outcome.result.recommendations == []
    assert outcome.steps == 2
    # Both queries of one step ran, and their results went back to the model.
    assert [c[0] for c in source.calls] == ["meals", "events"]
    assert source.calls[0][1:] == (date(2026, 10, 1), date(2026, 10, 14))
    results = [m for m in model.calls[1]["messages"] if isinstance(m, ToolMessage)]
    assert len(results) == 2
    assert "烤鱼" in results[0].content and '"id"' not in results[0].content


async def test_steps_used_up_goes_to_force_finish():
    script = [reply(query()), reply(query()), reply(query()), reply(query()), reply(submit())]
    outcome, model, source = await run(script)

    assert outcome.error is None
    assert outcome.steps == 5
    # The 4th agent call's query is not run; force_finish allows only submit_reply.
    assert len(source.calls) == 3
    assert [c["force_tool"] for c in model.calls] == [None] * 4 + ["submit_reply"]
    assert isinstance(model.calls[4]["messages"][-1], ToolMessage)


async def test_no_tool_call_goes_to_force_finish():
    outcome, model, _ = await run([AIMessage(content="随便说点什么"), reply(submit())])

    assert outcome.error is None
    assert model.calls[1]["force_tool"] == "submit_reply"


async def test_query_alongside_submit_is_ignored():
    outcome, _, source = await run([reply(query(), submit())])

    assert outcome.error is None
    assert outcome.steps == 1
    assert source.calls == []


async def test_invalid_reply_is_sent_back_once():
    outcome, model, _ = await run([reply(bad_submit()), reply(submit())])

    assert outcome.error is None
    assert outcome.retried
    assert outcome.steps == 2
    assert model.calls[1]["force_tool"] == "submit_reply"
    feedback = model.calls[1]["messages"][-1]
    assert isinstance(feedback, ToolMessage) and "校验" in feedback.content


async def test_second_invalid_reply_is_an_error():
    outcome, model, _ = await run([reply(bad_submit()), reply(bad_submit())])

    assert outcome.result is None
    assert outcome.error == "invalid_output"
    assert len(model.calls) == 2


async def test_recommendation_with_allergen_is_rejected():
    with_shrimp = tool_call("submit_reply", reply="方案：", recommendations=[
        {**RECS[0], "name": "虾仁炒饭"}, RECS[1], RECS[2],
    ])
    outcome, model, _ = await run([reply(with_shrimp), reply(submit())], allergens=["虾"])

    assert outcome.error is None
    assert outcome.retried
    assert "虾" in model.calls[1]["messages"][-1].content


async def test_allergen_check_ignores_case():
    with_peanut = tool_call("submit_reply", reply="方案：", recommendations=[
        {**RECS[0], "description": "Peanut butter toast"}, RECS[1], RECS[2],
    ])
    outcome, _, _ = await run([reply(with_peanut), reply(with_peanut)], allergens=["peanut"])

    assert outcome.error == "invalid_output"


async def test_ask_meal_type_without_recommendations():
    ask = tool_call("submit_reply", reply="你想问早餐、午餐还是晚餐？", ask_meal_type=True)
    outcome, _, _ = await run([reply(ask)], message="明天吃什么")

    assert outcome.result.ask_meal_type
    assert outcome.result.recommendations == []


async def test_ask_meal_type_with_recommendations_is_invalid():
    outcome, _, _ = await run([reply(submit(ask_meal_type=True)), reply(submit(ask_meal_type=True))])

    assert outcome.error == "invalid_output"


async def test_at_most_six_model_calls():
    # Queries every time, then two invalid replies: the longest possible path.
    script = [reply(query()) for _ in range(4)] + [reply(bad_submit()), reply(bad_submit())]
    outcome, model, source = await run(script)

    assert outcome.error == "invalid_output"
    assert len(model.calls) == 6
    assert outcome.steps == 6
    assert len(source.calls) == 3


async def test_bad_query_arguments_go_back_to_the_model():
    too_long = query(from_date="2026-08-01", to_date="2026-10-27")
    outcome, model, source = await run([reply(too_long), reply(submit())])

    assert outcome.error is None
    assert source.calls == []
    assert "31" in model.calls[1]["messages"][-1].content
