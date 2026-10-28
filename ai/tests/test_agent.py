from datetime import date

import pytest
from langchain_core.messages import AIMessage, ToolMessage

from mogumogu_ai.agent import run_agent
from mogumogu_ai.model import FakeModel, reply, tool_call

from .conftest import FakeSource, make_request

pytestmark = pytest.mark.anyio

RECS = [
    {"name": "鶏むね肉弁当", "description": "鶏むね肉と玄米", "reason": "午後に3時間の授業があり、腹持ちが必要"},
    {"name": "肉うどん", "description": "牛肉と温泉卵のうどん", "reason": "たんぱく質を補えて筋肉づくり向き"},
    {"name": "鮭おにぎり", "description": "おにぎり2個とサラダ", "reason": "コンビニですぐ買える"},
]


def submit(**args):
    return tool_call("submit_reply", reply="3つの案です：", recommendations=RECS, **args)


def bad_submit():
    # Two recommendations: neither 0 nor 3.
    return tool_call("submit_reply", reply="案：", recommendations=RECS[:2])


def query(name="get_meals", from_date="2026-10-01", to_date="2026-10-14"):
    return tool_call(name, from_date=from_date, to_date=to_date)


async def run(script, message="今日の昼食は何を食べたらいい？", allergens=None, source=None):
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
    assert "<message>\n今日の昼食は何を食べたらいい？\n</message>" in first
    assert '"target_date":"2026-10-27"' in first and '"locale":"ja"' in first


async def test_query_then_submit():
    text_only = tool_call("submit_reply", reply="10/01〜10/14の記録では、魚を2回食べています。")
    outcome, model, source = await run(
        [reply(query(), query("get_events")), reply(text_only)],
        message="この2週間で魚を何回食べた？",
        source=FakeSource([{"id": 1, "date": "2026-10-03", "meal_type": "dinner", "content": "焼き魚"}]),
    )

    assert outcome.result.recommendations == []
    assert outcome.steps == 2
    # Both queries of one step ran, and their results went back to the model.
    assert [c[0] for c in source.calls] == ["meals", "events"]
    assert source.calls[0][1:] == (date(2026, 10, 1), date(2026, 10, 14))
    results = [m for m in model.calls[1]["messages"] if isinstance(m, ToolMessage)]
    assert len(results) == 2
    assert "焼き魚" in results[0].content and '"id"' not in results[0].content


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
    outcome, model, _ = await run([AIMessage(content="なんとなく話す"), reply(submit())])

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
    assert isinstance(feedback, ToolMessage) and "failed validation" in feedback.content


async def test_second_invalid_reply_is_an_error():
    outcome, model, _ = await run([reply(bad_submit()), reply(bad_submit())])

    assert outcome.result is None
    assert outcome.error == "invalid_output"
    assert len(model.calls) == 2


async def test_recommendation_with_allergen_is_rejected():
    with_shrimp = tool_call("submit_reply", reply="案：", recommendations=[
        {**RECS[0], "name": "えびチャーハン"}, RECS[1], RECS[2],
    ])
    outcome, model, _ = await run([reply(with_shrimp), reply(submit())], allergens=["えび"])

    assert outcome.error is None
    assert outcome.retried
    assert "えび" in model.calls[1]["messages"][-1].content


async def test_allergen_check_ignores_case():
    with_peanut = tool_call("submit_reply", reply="Ideas:", recommendations=[
        {**RECS[0], "description": "Peanut butter toast"}, RECS[1], RECS[2],
    ])
    outcome, _, _ = await run([reply(with_peanut), reply(with_peanut)], allergens=["peanut"])

    assert outcome.error == "invalid_output"


async def test_ask_meal_type_without_recommendations():
    ask = tool_call("submit_reply", reply="朝食・昼食・夕食のどれですか？", ask_meal_type=True)
    outcome, _, _ = await run([reply(ask)], message="明日は何を食べたらいい？")

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
