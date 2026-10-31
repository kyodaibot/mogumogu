"""The agent's tools.

Query tools read data from Go's internal API (docs/adr/0001). Staging tools
write nothing: they collect changes to meal records, which Go saves as a
proposal and runs only when the user confirms (plans/change-proposals.md).
"""

import datetime
import json
from collections.abc import Iterable
from datetime import date, timedelta
from typing import Protocol

import httpx
from pydantic import BaseModel, Field, ValidationError

from .model import ToolSpec
from .schemas import MealType, ServingNutrition, SubmitReply

MAX_SPAN_DAYS = 31
MAX_RESULT_CHARS = 3000
# Changes one reply may stage; Go has the same limit.
MAX_CHANGES = 10

# Fields the model does not need; dropped to save tokens. Meals keep their id,
# which update_meal and delete_meal take.
_DROP = {"created_at", "updated_at"}


class DateRangeArgs(BaseModel):
    from_date: date = Field(description="Start date YYYY-MM-DD, inclusive")
    to_date: date = Field(description="End date YYYY-MM-DD, inclusive; at most 31 days from the start date")


GET_MEALS = ToolSpec(
    "get_meals",
    "Get the user's meal records in a date range (with any nutrition filled in). Use only for dates outside the initial context.",
    DateRangeArgs,
)
GET_EVENTS = ToolSpec(
    "get_events",
    "Get the user's schedule events in a date range. Use only for dates outside the initial context.",
    DateRangeArgs,
)
SUBMIT_REPLY = ToolSpec(
    "submit_reply", "Submit the final reply to the user. Every message must end with a call to it.", SubmitReply
)



class CreateMealArgs(BaseModel):
    # datetime.date: a field named date cannot be annotated with plain date.
    date: datetime.date = Field(description="The day the user ate it, YYYY-MM-DD")
    meal_type: MealType
    content: str = Field(min_length=1, max_length=200, description="What they ate, in the user's words")
    nutrition: ServingNutrition | None = Field(
        description="Estimate for one typical serving of it; null when the user does not want numbers"
    )


class UpdateMealArgs(BaseModel):
    meal_id: int = Field(description="id of a record from <context> meals or a get_meals result")
    date: datetime.date | None = Field(default=None, description="New date; omit when unchanged")
    meal_type: MealType | None = Field(default=None, description="New meal; omit when unchanged")
    content: str | None = Field(default=None, min_length=1, max_length=200, description="New content; omit when unchanged")
    nutrition: ServingNutrition | None = Field(
        default=None,
        description="New estimate for one typical serving; required when content changes; null removes the numbers",
    )


class DeleteMealArgs(BaseModel):
    meal_id: int = Field(description="id of a record from <context> meals or a get_meals result")


_STAGED = " Nothing is saved until the user confirms it on the card shown with your reply."
CREATE_MEAL = ToolSpec("create_meal", "Stage a new meal record for something the user ate." + _STAGED, CreateMealArgs)
UPDATE_MEAL = ToolSpec(
    "update_meal", "Stage a change to one meal record; give only the fields that change." + _STAGED, UpdateMealArgs
)
DELETE_MEAL = ToolSpec("delete_meal", "Stage deleting one meal record." + _STAGED, DeleteMealArgs)

QUERY_TOOLS = (GET_MEALS, GET_EVENTS)
STAGING_TOOLS = (CREATE_MEAL, UPDATE_MEAL, DELETE_MEAL)


def tools_for(allow_proposal: bool) -> tuple[ToolSpec, ...]:
    """The tools offered to the model; staging tools only when Go allows it."""
    return (*QUERY_TOOLS, *(STAGING_TOOLS if allow_proposal else ()), SUBMIT_REPLY)


class DataSource(Protocol):
    async def fetch(self, kind: str, start: date, end: date) -> list[dict]:
        """kind is "meals" or "events"."""
        ...


class GoClient:
    """Calls GET /internal/agent/{meals,events} with the per-request token."""

    def __init__(self, http: httpx.AsyncClient, token: str):
        self.http = http
        self.token = token

    async def fetch(self, kind: str, start: date, end: date) -> list[dict]:
        resp = await self.http.get(
            f"/internal/agent/{kind}",
            params={"from": start.isoformat(), "to": end.isoformat()},
            headers={"Authorization": f"Bearer {self.token}"},
        )
        resp.raise_for_status()
        return resp.json()["items"]


class QueryBudget:
    """Days already queried in this message, per kind.

    One message may read at most MAX_SPAN_DAYS days of each kind in total, so
    the model cannot cover two months by splitting the range into two queries.
    """

    def __init__(self):
        self.days: dict[str, set[date]] = {}

    def take(self, kind: str, start: date, end: date) -> bool:
        wanted = {start + timedelta(days=i) for i in range((end - start).days + 1)}
        seen = self.days.setdefault(kind, set())
        if len(seen | wanted) > MAX_SPAN_DAYS:
            return False
        seen |= wanted
        return True


async def run_query(
    source: DataSource, name: str, args: dict, budget: QueryBudget, seen_meals: set[int] | None = None
) -> str:
    """Runs one query tool call and returns the text sent back to the model.
    The ids of meals it returns are added to seen_meals.

    Bad arguments become an error text, so the model can correct itself; other
    failures (Go unreachable, token rejected) raise and end the request.
    """
    try:
        parsed = DateRangeArgs.model_validate(args)
    except ValidationError as e:
        return f"Invalid arguments: {e.errors()[0]['msg']}"
    start, end = parsed.from_date, parsed.to_date
    if end < start:
        return "Invalid arguments: to_date must not be before from_date"
    if (end - start).days + 1 > MAX_SPAN_DAYS:
        return f"Invalid arguments: one query covers at most {MAX_SPAN_DAYS} days"

    kind = "meals" if name == GET_MEALS.name else "events"
    if not budget.take(kind, start, end):
        return (
            f"Invalid arguments: one message may read at most {MAX_SPAN_DAYS} days of records in total. "
            f"Answer from the latest {MAX_SPAN_DAYS} days only, and mention this limit"
        )
    items = await source.fetch(kind, start, end)
    drop = _DROP if kind == "meals" else _DROP | {"id"}
    rows = [{k: v for k, v in item.items() if k not in drop} for item in items]
    if kind == "meals" and seen_meals is not None:
        seen_meals.update(item["id"] for item in items)
    text = json.dumps(
        {"from": start.isoformat(), "to": end.isoformat(), "items": rows},
        ensure_ascii=False,
        separators=(",", ":"),
    )
    if len(text) > MAX_RESULT_CHARS:
        text = text[:MAX_RESULT_CHARS] + "… (result too long, truncated; narrow the date range)"
    return text


class Staging:
    """The changes staged while answering one message.

    Only records the model has seen (the context, get_meals results) can be
    updated or deleted, so it cannot act on a made-up id. Go checks every
    change again before saving it.
    """

    def __init__(self, seen_meals: Iterable[int], allergens: list[str]):
        self.seen_meals = set(seen_meals)
        self.allergens = [a.strip() for a in allergens if a.strip()]
        self.changes: list[dict] = []
        self._staged_ids: set[int] = set()

    def stage(self, name: str, args: dict) -> str:
        """Stages one tool call and returns the text sent back to the model."""
        spec = next(t for t in STAGING_TOOLS if t.name == name)
        try:
            parsed = spec.args.model_validate(args)
        except ValidationError as e:
            errors = "; ".join(f"{'.'.join(map(str, err['loc']))}: {err['msg']}" for err in e.errors()[:3])
            return f"Not staged. Invalid arguments: {errors}"
        if len(self.changes) >= MAX_CHANGES:
            return f"Not staged: one reply can stage at most {MAX_CHANGES} changes"

        if isinstance(parsed, CreateMealArgs):
            change = {"op": "create", "meal": {
                "date": parsed.date.isoformat(), "meal_type": parsed.meal_type, "content": parsed.content,
                "nutrition": _nutrition(parsed.nutrition),
            }}
            content = parsed.content
        else:
            meal_id = parsed.meal_id
            if meal_id not in self.seen_meals:
                return (
                    f"Not staged: there is no record with id {meal_id} in <context> meals or a get_meals result. "
                    "Use the id of a record you have seen; query the date first if needed"
                )
            if meal_id in self._staged_ids:
                return f"Not staged: record {meal_id} already has a staged change; one change per record"
            change = {"op": "delete", "meal_id": meal_id}
            content = None
            if isinstance(parsed, UpdateMealArgs):
                fields = {k: v for k, v in parsed.model_dump(mode="json", exclude={"meal_id", "nutrition"}).items() if v is not None}
                if "nutrition" in parsed.model_fields_set:
                    fields["nutrition"] = _nutrition(parsed.nutrition)
                if not fields:
                    return "Not staged: give at least one field that changes"
                if "content" in fields and "nutrition" not in fields:
                    return "Not staged: the content changes, so give a new nutrition estimate too (or null for none)"
                change = {"op": "update", "meal_id": meal_id, "meal": fields}
                content = parsed.content
            self._staged_ids.add(meal_id)

        self.changes.append(change)
        text = f"Staged as change {len(self.changes)}." + _STAGED
        # Recording what the user already ate is allowed even with an
        # allergen in it, but the model should notice (it must not suggest it).
        found = [a for a in self.allergens if content and a.casefold() in content.casefold()]
        if found:
            text += f" Note: it contains the user's allergen {', '.join(found)}; mention this in the reply."
        return text


def _nutrition(n: ServingNutrition | None) -> dict | None:
    return n.rounded().model_dump() if n else None
