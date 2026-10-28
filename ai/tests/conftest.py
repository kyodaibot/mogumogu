from datetime import date

import pytest

from mogumogu_ai.schemas import ReplyRequest


@pytest.fixture
def anyio_backend():
    return "asyncio"


def make_request(
    message: str = "今日の昼食は何を食べたらいい？", allergens: list[str] | None = None, locale: str = "ja"
) -> ReplyRequest:
    return ReplyRequest.model_validate({
        "message": message,
        "token": "test-token",
        "context": {
            "target_date": "2026-10-27",
            "now": "2026-10-27T11:30:00+09:00",
            "timezone": "Asia/Tokyo",
            "locale": locale,
            "profile": {
                "height_cm": 170, "weight_kg": 60, "age": 21, "has_exercise_habit": True,
                "allergens": allergens, "goal": "build_muscle",
            },
            "events": [{
                "title": "授業", "starts_at": "2026-10-27T13:00:00+09:00",
                "ends_at": "2026-10-27T16:00:00+09:00", "notes": None,
            }],
            "events_range": {"from": "2026-10-26", "to": "2026-10-28"},
            "meals": [{
                "date": "2026-10-26", "meal_type": "dinner", "content": "カレーライス",
                "calories": None, "protein_g": None, "fat_g": None, "carbs_g": None,
            }],
            "meals_range": {"from": "2026-10-21", "to": "2026-10-27"},
        },
    })


class FakeSource:
    """Stands in for Go's internal API and records every query."""

    def __init__(self, items: list[dict] | None = None):
        self.items = items or []
        self.calls: list[tuple[str, date, date]] = []

    async def fetch(self, kind, start, end):
        self.calls.append((kind, start, end))
        return self.items
