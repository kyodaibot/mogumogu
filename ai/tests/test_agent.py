import json
from datetime import date

import pytest
from langchain_core.messages import AIMessage, ToolMessage

from mogumogu_ai.agent import run_agent
from mogumogu_ai.model import FakeModel, reply, tool_call

from .conftest import FakeSource, make_request, turn

pytestmark = pytest.mark.anyio

# 4*35 + 4*85 + 9*18 = 642 ≈ 652; 4*9 + 4*21 + 9*4.4 = 159.6 ≈ 163.4; 652 / 163.4 → about 400 g.
ESTIMATE = {
    "serving": {"calories": 652, "protein_g": 35.4, "fat_g": 18, "carbs_g": 84.6},
    "per_100g": {"calories": 163.4, "protein_g": 8.5, "fat_g": 4.4, "carbs_g": 21.2},
}

RECS = [
    {"name": "鶏むね肉弁当", "description": "鶏むね肉と玄米", "reason": "午後に3時間の授業があり、腹持ちが必要",
     "estimate": ESTIMATE},
    {"name": "肉うどん", "description": "牛肉と温泉卵のうどん", "reason": "たんぱく質を補えて筋肉づくり向き",
     "estimate": ESTIMATE},
    {"name": "鮭おにぎり", "description": "おにぎり2個とサラダ", "reason": "コンビニですぐ買える",
     "estimate": ESTIMATE},
]


def submit(**args):
    args.setdefault("meal_type", "lunch")
    return tool_call("submit_reply", reply="3つの案です：", recommendations=RECS, **args)


def with_estimate(**parts):
    """RECS with the first recommendation's estimate changed."""
    estimate = {k: {**v, **parts.get(k, {})} for k, v in ESTIMATE.items()}
    return [{**RECS[0], "estimate": estimate}, RECS[1], RECS[2]]


def bad_submit():
    # Two recommendations: neither 0 nor 3.
    return tool_call("submit_reply", reply="案：", recommendations=RECS[:2], meal_type="lunch")


def query(name="get_meals", from_date="2026-10-01", to_date="2026-10-14"):
    return tool_call(name, from_date=from_date, to_date=to_date)


async def run(script, message="今日の昼食は何を食べたらいい？", allergens=None, source=None, history=None):
    model = FakeModel(script)
    source = source or FakeSource()
    outcome = await run_agent(model, source, make_request(message, allergens, history=history))
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
    # Meals keep their id (for update_meal and delete_meal); events do not.
    assert "焼き魚" in results[0].content and '"id":1' in results[0].content
    assert '"id"' not in results[1].content


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
    with_shrimp = tool_call("submit_reply", reply="案：", meal_type="lunch", recommendations=[
        {**RECS[0], "name": "えびチャーハン"}, RECS[1], RECS[2],
    ])
    outcome, model, _ = await run([reply(with_shrimp), reply(submit())], allergens=["えび"])

    assert outcome.error is None
    assert outcome.retried
    assert "えび" in model.calls[1]["messages"][-1].content


async def test_allergen_check_ignores_case():
    with_peanut = tool_call("submit_reply", reply="Ideas:", meal_type="breakfast", recommendations=[
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


async def test_estimates_are_rounded():
    outcome, _, _ = await run([reply(submit())])

    rec = outcome.result.model_dump()["recommendations"][0]
    assert rec["estimate"] == {
        "serving": {"calories": 650, "protein_g": 35, "fat_g": 18, "carbs_g": 85},
        "per_100g": {"calories": 163, "protein_g": 9, "fat_g": 4, "carbs_g": 21},
    }
    assert outcome.result.meal_type == "lunch"


@pytest.mark.parametrize(
    ("parts", "reason"),
    [
        # Out of range.
        ({"serving": {"calories": 12000}}, "serving.calories"),
        # Calories far from 4*P + 4*C + 9*F.
        ({"serving": {"calories": 1200}}, "do not match"),
        # More than 100 g of nutrients in 100 g.
        ({"per_100g": {"protein_g": 50, "fat_g": 30, "carbs_g": 40, "calories": 630}}, "more than 100 g"),
        # 652 kcal at 20 kcal per 100 g would be a 3 kg serving.
        ({"per_100g": {"calories": 20, "protein_g": 1, "fat_g": 0.5, "carbs_g": 3}}, "imply a serving"),
    ],
)
async def test_implausible_estimate_is_sent_back(parts, reason):
    bad = tool_call("submit_reply", reply="案：", recommendations=with_estimate(**parts), meal_type="lunch")
    outcome, model, _ = await run([reply(bad), reply(submit())])

    assert outcome.error is None
    assert outcome.retried
    assert reason in model.calls[1]["messages"][-1].content


async def test_low_calorie_food_passes_the_energy_check():
    # 4*1 + 4*2 + 9*0.1 = 12.9 vs 25 kcal: off by half, but within the 15 kcal floor.
    salad = {"per_100g": {"calories": 25, "protein_g": 1, "fat_g": 0.1, "carbs_g": 2}}
    recs = with_estimate(**salad, serving={"calories": 150, "protein_g": 6, "fat_g": 1, "carbs_g": 30})
    outcome, _, _ = await run([reply(tool_call("submit_reply", reply="案：", recommendations=recs, meal_type="lunch"))])

    assert outcome.error is None and not outcome.retried


async def test_recommendations_need_a_meal_type():
    outcome, model, _ = await run([reply(submit(meal_type=None)), reply(submit())])

    assert outcome.retried
    assert "meal_type is required" in model.calls[1]["messages"][-1].content


async def test_meal_type_without_recommendations_is_dropped():
    # Refusals often keep the meal they were about, or an invalid value.
    for meal_type in ("dinner", "none"):
        text = tool_call("submit_reply", reply="記録によると…", meal_type=meal_type)
        outcome, _, _ = await run([reply(text)])

        assert outcome.error is None and not outcome.retried
        assert outcome.result.meal_type is None


async def test_history_comes_first_as_data():
    history = [
        turn("明日の昼食は何を食べたらいい？", "3つの案です：", "親子丼", "焼き鮭定食", "カレー", day="2026-10-28"),
        turn("あさっては？", "朝食・昼食・夕食のどれですか？"),
    ]
    _, model, _ = await run([reply(submit())], message="ご飯なしで", history=history)

    # One user message: the history, then the fresh context, then the message.
    [system, first] = model.calls[0]["messages"]
    text = first.content
    assert text.index("<history>") < text.index("<context>") < text.index("<message>\nご飯なしで")
    block = text.split("<history>\n", 1)[1].split("\n</history>", 1)[0]
    assert json.loads(block) == [
        {"target_date": "2026-10-28", "user": "明日の昼食は何を食べたらいい？", "assistant": "3つの案です：",
         "recommendations": [{"name": n, "description": f"{n}の説明"} for n in ("親子丼", "焼き鮭定食", "カレー")]},
        {"target_date": "2026-10-27", "user": "あさっては？", "assistant": "朝食・昼食・夕食のどれですか？",
         "recommendations": []},
    ]
    # The system prompt stays the same for every request, so it can be cached.
    assert "親子丼" not in system.content


async def test_no_history_block_in_a_new_conversation():
    _, model, _ = await run([reply(submit())])

    assert "<history>" not in model.calls[0]["messages"][1].content


async def test_history_cannot_allow_an_allergen():
    # An altered history where the assistant seems to have said shrimp is fine.
    history = [turn("えびは食べられる？", "はい、えびは大丈夫です。")]
    with_shrimp = tool_call("submit_reply", reply="案：", meal_type="lunch", recommendations=[
        {**RECS[0], "name": "えびチャーハン"}, RECS[1], RECS[2],
    ])
    outcome, model, _ = await run(
        [reply(with_shrimp), reply(submit())], message="えびの料理で", allergens=["えび"], history=history,
    )

    assert outcome.error is None
    assert outcome.retried
    assert "えび" not in " ".join(r.name for r in outcome.result.recommendations)
