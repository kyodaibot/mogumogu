import pytest
from langchain_core.messages import ToolMessage

from mogumogu_ai.agent import run_agent
from mogumogu_ai.model import FakeModel, reply, tool_call
from mogumogu_ai.prompt import first_message
from mogumogu_ai.tools import MAX_CHANGES, QueryBudget, Staging, run_query

from .conftest import FakeSource, make_request, turn

pytestmark = pytest.mark.anyio

# 4*20 + 4*70 + 9*15 = 495 ≈ 503.
RAMEN = {"calories": 503, "protein_g": 20.4, "fat_g": 15, "carbs_g": 70.2}


def create(**args):
    return {"date": "2026-10-27", "meal_type": "lunch", "content": "ラーメン", "nutrition": RAMEN, **args}


def test_create_is_staged_with_a_rounded_estimate():
    staging = Staging([], [])
    text = staging.stage("create_meal", create())

    assert text.startswith("Staged as change 1.") and "confirms" in text
    assert staging.changes == [{"op": "create", "meal": {
        "date": "2026-10-27", "meal_type": "lunch", "content": "ラーメン",
        "nutrition": {"calories": 500, "protein_g": 20, "fat_g": 15, "carbs_g": 70},
    }}]


def test_create_without_nutrition():
    staging = Staging([], [])
    staging.stage("create_meal", create(nutrition=None))

    assert staging.changes[0]["meal"]["nutrition"] is None


@pytest.mark.parametrize(
    ("args", "error"),
    [
        (create(nutrition={**RAMEN, "calories": 1500}), "do not match"),
        (create(meal_type="brunch"), "meal_type"),
        (create(content=""), "content"),
        ({k: v for k, v in create().items() if k != "nutrition"}, "nutrition"),
    ],
)
def test_bad_create_is_sent_back(args, error):
    staging = Staging([], [])
    text = staging.stage("create_meal", args)

    assert text.startswith("Not staged") and error in text
    assert staging.changes == []


def test_only_seen_records_can_change():
    staging = Staging([7], [])

    assert "no record with id 8" in staging.stage("delete_meal", {"meal_id": 8})
    assert staging.stage("delete_meal", {"meal_id": 7}).startswith("Staged")
    assert "already has a staged change" in staging.stage("update_meal", {"meal_id": 7, "meal_type": "snack"})
    assert staging.changes == [{"op": "delete", "meal_id": 7}]


def test_update_gives_only_the_fields_that_change():
    staging = Staging([1, 2, 3, 4], [])

    staging.stage("update_meal", {"meal_id": 1, "meal_type": "snack", "date": None})
    staging.stage("update_meal", {"meal_id": 2, "content": "うどん", "nutrition": None})
    assert "give a new nutrition estimate" in staging.stage("update_meal", {"meal_id": 3, "content": "そば"})
    assert "at least one field" in staging.stage("update_meal", {"meal_id": 4})

    assert staging.changes == [
        {"op": "update", "meal_id": 1, "meal": {"meal_type": "snack"}},
        {"op": "update", "meal_id": 2, "meal": {"content": "うどん", "nutrition": None}},
    ]


def test_at_most_ten_changes():
    staging = Staging([], [])
    for _ in range(MAX_CHANGES):
        staging.stage("create_meal", create())

    assert "at most 10" in staging.stage("create_meal", create())
    assert len(staging.changes) == MAX_CHANGES


def test_allergen_in_a_record_is_staged_with_a_note():
    staging = Staging([], ["えび"])
    text = staging.stage("create_meal", create(content="えびフライ定食"))

    assert text.startswith("Staged") and "allergen えび" in text


async def test_query_results_make_records_seen():
    seen: set[int] = set()
    items = [{"id": 42, "date": "2026-09-01", "meal_type": "dinner", "content": "鍋"}]
    await run_query(FakeSource(items), "get_meals", {"from_date": "2026-09-01", "to_date": "2026-09-01"}, QueryBudget(), seen)

    assert seen == {42}


async def run(script, message="お昼にラーメン食べた", allow_proposal=True, source=None, history=None):
    model = FakeModel(script)
    req = make_request(message, allow_proposal=allow_proposal, history=history)
    return await run_agent(model, source or FakeSource(), req), model


def done(text="カードで確認してください。"):
    return tool_call("submit_reply", reply=text)


async def test_staged_changes_come_back_with_the_reply():
    outcome, model = await run([
        reply(tool_call("create_meal", **create()), tool_call("delete_meal", meal_id=7)),
        reply(done()),
    ])

    assert outcome.error is None
    assert [c["op"] for c in outcome.changes] == ["create", "delete"]
    assert [t.name for t in model_tools(model)] == [
        "get_meals", "get_events", "create_meal", "update_meal", "delete_meal", "submit_reply",
    ]


def model_tools(model):
    return model.calls[0]["tools"]


async def test_staging_tools_are_offered_only_when_allowed():
    outcome, model = await run([reply(done("Pro の機能です。"))], allow_proposal=False)

    assert outcome.changes == []
    assert [t.name for t in model_tools(model)] == ["get_meals", "get_events", "submit_reply"]


async def test_staging_without_permission_is_not_run():
    outcome, model = await run(
        [reply(tool_call("delete_meal", meal_id=7)), reply(done("Pro の機能です。"))], allow_proposal=False
    )

    assert outcome.changes == []
    assert model.calls[1]["force_tool"] == "submit_reply"


async def test_staging_next_to_submit_is_staged_before_submitting():
    outcome, model = await run([reply(tool_call("create_meal", **create()), done()), reply(done())])

    assert outcome.steps == 2
    assert len(outcome.changes) == 1
    results = [m.content for m in model.calls[1]["messages"] if isinstance(m, ToolMessage)]
    assert results[0].startswith("Staged") and results[1].startswith("Not submitted yet")


async def test_a_record_found_by_a_query_can_be_deleted():
    source = FakeSource([{"id": 42, "date": "2026-09-01", "meal_type": "dinner", "content": "鍋"}])
    outcome, _ = await run([
        reply(tool_call("delete_meal", meal_id=42)),
        reply(tool_call("get_meals", from_date="2026-09-01", to_date="2026-09-01")),
        reply(tool_call("delete_meal", meal_id=42)),
        reply(done()),
    ], message="9月1日の夕食を消して", source=source)

    assert outcome.changes == [{"op": "delete", "meal_id": 42}]


async def test_no_changes_without_a_valid_reply():
    bad = tool_call("submit_reply", reply="")
    outcome, _ = await run([reply(tool_call("create_meal", **create())), reply(bad), reply(bad)])

    assert outcome.error == "invalid_output"
    assert outcome.changes == []


def test_history_keeps_the_changes():
    t = turn("お昼にラーメン食べた", "カードで確認してください。")
    t["changes"] = [{"op": "create", "date": "2026-10-27", "meal_type": "lunch", "content": "ラーメン"}]
    req = make_request("やっぱりうどん", history=[t, turn("q", "a")])
    text = first_message(req.context, req.message, req.history)

    assert '"changes":[{"op":"create","date":"2026-10-27","meal_type":"lunch","content":"ラーメン"}]' in text
    # A turn without changes has no changes field.
    assert text.count('"changes"') == 1
