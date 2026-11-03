"""The limits the AI service shares with Go and the browser (contract/README.md).

When one fails, change contract/limits.json and the other tiers too.
"""

import json
from pathlib import Path

import annotated_types
import pytest
from pydantic import BaseModel, ValidationError

from mogumogu_ai import tools
from mogumogu_ai.schemas import (
    MAX_SOURCES,
    HistoryChange,
    HistoryDish,
    Meal,
    MealType,
    Recommendation,
    Reply,
    ReplyRequest,
    ServingNutrition,
    Source,
    SubmitReply,
    Turn,
)
from mogumogu_ai.time_budget import HARD_LIMIT_S

CONTRACT = Path(__file__).resolve().parents[2] / "contract"
LIMITS = json.loads((CONTRACT / "limits.json").read_text())


def bound(model: type[BaseModel], field: str, kind: type) -> float:
    """The max_length (MaxLen) or le (Le) of a field."""
    for m in model.model_fields[field].metadata:
        if isinstance(m, kind):
            return m.max_length if kind is annotated_types.MaxLen else m.le
    raise AssertionError(f"{model.__name__}.{field} has no {kind.__name__}")


def max_len(model: type[BaseModel], field: str) -> int:
    return bound(model, field, annotated_types.MaxLen)


@pytest.mark.parametrize("key,value", [
    ("changes_per_reply", tools.MAX_CHANGES),
    ("memories", tools.MAX_MEMORIES),
    ("memory_chars", tools.MAX_MEMORY_CHARS),
    ("memory_chars", max_len(tools.RememberArgs, "content")),
    ("query_span_days", tools.MAX_SPAN_DAYS),
    ("searches", tools.MAX_SEARCHES),
    ("search_rounds", tools.MAX_SEARCH_ROUNDS),
    ("sources", MAX_SOURCES),
    ("ai_hard_limit_s", HARD_LIMIT_S),
    # What Go sends (the message and the history it checked).
    ("message_chars", max_len(ReplyRequest, "message")),
    ("history_turns", max_len(ReplyRequest, "history")),
    ("message_chars", max_len(Turn, "message")),
    ("reply_chars", max_len(Turn, "reply")),
    ("changes_per_reply", max_len(Turn, "changes")),
    ("dish_name_chars", max_len(HistoryDish, "name")),
    ("dish_description_chars", max_len(HistoryDish, "description")),
    ("meal_content_chars", max_len(HistoryChange, "content")),
    # What the model's reply and staged records may hold.
    ("reply_chars", max_len(SubmitReply, "reply")),
    ("dish_name_chars", max_len(Recommendation, "name")),
    ("dish_description_chars", max_len(Recommendation, "description")),
    ("meal_content_chars", max_len(tools.CreateMealArgs, "content")),
    ("meal_content_chars", max_len(tools.UpdateMealArgs, "content")),
])
def test_limit_matches_the_contract(key, value):
    assert value == LIMITS[key], f"{key}: the AI service has {value}, the contract {LIMITS[key]}"


def test_meal_types_match_the_contract():
    assert list(MealType.__args__) == LIMITS["meal_types"]


def test_staged_nutrition_ranges_match_the_contract():
    for field, top in LIMITS["serving_nutrition_max"].items():
        assert bound(ServingNutrition, field, annotated_types.Le) == top, field


def test_recommendation_counts_match_the_contract():
    dish = {"name": "定食", "description": "説明"}
    for n in range(5):
        history_ok = True
        try:
            Turn.model_validate({"date": "2026-10-27", "message": "m", "reply": "r", "recommendations": [dish] * n})
        except ValidationError:
            history_ok = False
        assert history_ok == (n in LIMITS["recommendation_counts"]), n


# What the reply's changes come from: these tool calls, on the records and
# memories of contract/request.json.
MESSAGE = "辛いのは平気になった。豚骨が好き、甘いものは控えてる。"
MEAL_CALLS = [
    ("create_meal", {"date": "2026-10-27", "meal_type": "lunch", "content": "豚骨ラーメン",
                     "nutrition": {"calories": 702, "protein_g": 28.4, "fat_g": 30, "carbs_g": 79.6}}),
    ("create_meal", {"date": "2026-10-27", "meal_type": "snack", "content": "りんご", "nutrition": None}),
    ("update_meal", {"meal_id": 21, "meal_type": "lunch"}),
    ("update_meal", {"meal_id": 22, "content": "トーストと牛乳", "nutrition": None}),
    ("delete_meal", {"meal_id": 99}),
]
MEMORY_CALLS = [
    ("remember", {"content": "豚骨ラーメンが好き", "quote": "豚骨が好き"}),
    ("update_memory", {"memory_id": 3, "content": "辛いものは平気", "quote": "辛いのは平気になった"}),
    ("forget", {"memory_id": 4, "quote": "甘いものは控えてる"}),
]


def test_go_request_is_read():
    """contract/request.json is what Go sends; Go's tests keep it current."""
    req = ReplyRequest.model_validate_json((CONTRACT / "request.json").read_text())
    assert req.context.profile.allergens == ["えび", "ピーナッツ"]
    assert [m.content for m in req.context.meals] == ["カレーライス", "トーストとコーヒー"]
    assert req.history[0].changes[0].op == "create" and (req.user_id, req.tier) == (7, "pro")


def test_reply_is_what_the_tools_stage():
    """contract/reply.json is what the AI service answers; Go's tests read it."""
    req = ReplyRequest.model_validate_json((CONTRACT / "request.json").read_text())
    extra = Meal.model_validate({"id": 99, "date": "2026-10-25", "meal_type": "snack", "content": "せんべい"})
    staging = tools.Staging([*req.context.meals, extra], [])
    for name, args in MEAL_CALLS:
        assert staging.stage(name, args).startswith("Staged"), name
    memories = tools.MemoryChanges(req.context.memories, MESSAGE)
    for name, args in MEMORY_CALLS:
        assert memories.stage(name, args)[1], name

    data = json.loads((CONTRACT / "reply.json").read_text())
    # Reply keeps SubmitReply's check of the model's sources (URLs only), so
    # the titled sources are checked on their own; app.py builds it with
    # model_construct for the same reason.
    sources = [Source.model_validate(s) for s in data["sources"]]
    reply = Reply.model_validate({**data, "sources": []})
    assert reply.changes == staging.changes
    assert reply.memory_changes == memories.changes
    # Every kind of change Go must read is in it.
    assert {c["op"] for c in reply.changes} == {"create", "update", "delete"}
    assert {c["op"] for c in reply.memory_changes} == {"create", "update", "delete"}
    assert len(reply.recommendations) == 3 and sources


def test_prompt_states_the_limits_tools_enforce():
    from mogumogu_ai.prompt import SYSTEM_PROMPT

    for phrase in [
        f"at most {LIMITS['query_span_days']} days of each kind",
        f"At most {LIMITS['changes_per_reply']} changes per reply",
        f"at most {LIMITS['memory_chars']} characters",
        f"At most {LIMITS['memories']} memories in total",
        f"at most {LIMITS['searches']} searches per message, from at most {LIMITS['search_rounds']} responses",
    ]:
        assert phrase in SYSTEM_PROMPT, phrase
