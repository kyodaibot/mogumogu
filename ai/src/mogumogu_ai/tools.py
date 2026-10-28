"""Read-only query tools. Data comes from Go's internal API (docs/adr/0001)."""

import json
from datetime import date, timedelta
from typing import Protocol

import httpx
from pydantic import BaseModel, Field, ValidationError

from .model import ToolSpec
from .schemas import SubmitReply

MAX_SPAN_DAYS = 31
MAX_RESULT_CHARS = 3000

# Fields the model does not need; dropped to save tokens.
_DROP = {"id", "created_at", "updated_at"}


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

QUERY_TOOLS = (GET_MEALS, GET_EVENTS)
ALL_TOOLS = (*QUERY_TOOLS, SUBMIT_REPLY)


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


async def run_query(source: DataSource, name: str, args: dict, budget: QueryBudget) -> str:
    """Runs one query tool call and returns the text sent back to the model.

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
    rows = [{k: v for k, v in item.items() if k not in _DROP} for item in items]
    text = json.dumps(
        {"from": start.isoformat(), "to": end.isoformat(), "items": rows},
        ensure_ascii=False,
        separators=(",", ":"),
    )
    if len(text) > MAX_RESULT_CHARS:
        text = text[:MAX_RESULT_CHARS] + "… (result too long, truncated; narrow the date range)"
    return text
