from datetime import date

import httpx
import pytest

from mogumogu_ai.tools import MAX_RESULT_CHARS, GoClient, QueryBudget, run_query

from .conftest import FakeSource

pytestmark = pytest.mark.anyio


async def test_go_client_sends_token_and_range():
    seen = {}

    def handler(request: httpx.Request):
        seen["path"] = request.url.path
        seen["params"] = dict(request.url.params)
        seen["auth"] = request.headers["Authorization"]
        return httpx.Response(200, json={"items": [{"id": 1, "title": "上课"}]})

    async with httpx.AsyncClient(base_url="http://go", transport=httpx.MockTransport(handler)) as http:
        items = await GoClient(http, "tok").fetch("events", date(2026, 10, 1), date(2026, 10, 31))

    assert items == [{"id": 1, "title": "上课"}]
    assert seen == {
        "path": "/internal/agent/events",
        "params": {"from": "2026-10-01", "to": "2026-10-31"},
        "auth": "Bearer tok",
    }


async def test_go_client_raises_when_go_rejects():
    transport = httpx.MockTransport(lambda r: httpx.Response(401))
    async with httpx.AsyncClient(base_url="http://go", transport=transport) as http:
        with pytest.raises(httpx.HTTPStatusError):
            await GoClient(http, "bad").fetch("meals", date(2026, 10, 1), date(2026, 10, 2))


@pytest.mark.parametrize(
    ("args", "error"),
    [
        ({"from_date": "2026-10-01", "to_date": "2026-11-01"}, "31"),  # 32 days
        ({"from_date": "2026-10-10", "to_date": "2026-10-01"}, "早于"),
        ({"from_date": "10/01"}, "参数错误"),
    ],
)
async def test_bad_arguments_are_not_sent_to_go(args, error):
    source = FakeSource()
    text = await run_query(source, "get_meals", args, QueryBudget())

    assert error in text
    assert source.calls == []


async def test_31_days_is_allowed():
    source = FakeSource()
    await run_query(source, "get_meals", {"from_date": "2026-10-01", "to_date": "2026-10-31"}, QueryBudget())

    assert source.calls == [("meals", date(2026, 10, 1), date(2026, 10, 31))]


async def test_long_results_are_truncated():
    items = [{"id": i, "date": "2026-10-01", "meal_type": "lunch", "content": "很长的描述" * 10} for i in range(100)]
    text = await run_query(FakeSource(items), "get_meals", {"from_date": "2026-10-01", "to_date": "2026-10-31"}, QueryBudget())

    assert len(text) < MAX_RESULT_CHARS + 50
    assert text.endswith("请缩小日期范围）")


async def test_one_message_reads_at_most_31_days_per_kind():
    source, budget = FakeSource(), QueryBudget()
    sep = {"from_date": "2026-09-01", "to_date": "2026-09-30"}

    await run_query(source, "get_meals", sep, budget)
    # Overlapping days count once; events have their own 31 days.
    await run_query(source, "get_meals", {"from_date": "2026-09-15", "to_date": "2026-10-01"}, budget)
    await run_query(source, "get_events", sep, budget)
    text = await run_query(source, "get_meals", {"from_date": "2026-10-02", "to_date": "2026-10-02"}, budget)

    assert "累计最多查询 31 天" in text
    assert [c[0] for c in source.calls] == ["meals", "meals", "events"]
