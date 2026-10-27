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
    from_date: date = Field(description="开始日期 YYYY-MM-DD，包含")
    to_date: date = Field(description="结束日期 YYYY-MM-DD，包含；与开始日期最多相差 31 天")


GET_MEALS = ToolSpec(
    "get_meals",
    "查询用户在日期范围内的饮食记录（包括已填写的营养信息）。只在需要初始上下文以外的日期时使用。",
    DateRangeArgs,
)
GET_EVENTS = ToolSpec(
    "get_events",
    "查询用户在日期范围内的行程。只在需要初始上下文以外的日期时使用。",
    DateRangeArgs,
)
SUBMIT_REPLY = ToolSpec("submit_reply", "提交给用户的最终回复。每条消息必须以调用它结束。", SubmitReply)

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
        return f"参数错误：{e.errors()[0]['msg']}"
    start, end = parsed.from_date, parsed.to_date
    if end < start:
        return "参数错误：to_date 不能早于 from_date"
    if (end - start).days + 1 > MAX_SPAN_DAYS:
        return f"参数错误：一次最多查询 {MAX_SPAN_DAYS} 天"

    kind = "meals" if name == GET_MEALS.name else "events"
    if not budget.take(kind, start, end):
        return f"参数错误：每条消息累计最多查询 {MAX_SPAN_DAYS} 天的记录。请只根据最近 {MAX_SPAN_DAYS} 天回答，并说明这个限制"
    items = await source.fetch(kind, start, end)
    rows = [{k: v for k, v in item.items() if k not in _DROP} for item in items]
    text = json.dumps(
        {"from": start.isoformat(), "to": end.isoformat(), "items": rows},
        ensure_ascii=False,
        separators=(",", ":"),
    )
    if len(text) > MAX_RESULT_CHARS:
        text = text[:MAX_RESULT_CHARS] + "…（结果过长已截断，请缩小日期范围）"
    return text
