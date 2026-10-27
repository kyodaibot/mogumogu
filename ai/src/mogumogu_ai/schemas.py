"""Data exchanged with the Go backend and with the model.

Field names follow docs/api.md so Go can pass its own JSON through unchanged.
"""

from datetime import date, datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


class Profile(BaseModel):
    height_cm: float | None = None
    weight_kg: float | None = None
    age: int | None = None
    has_exercise_habit: bool | None = None
    # null: not filled in yet; []: confirmed none; otherwise the user's list.
    allergens: list[str] | None = None
    goal: Literal["build_muscle", "lose_fat"] | None = None


class Event(BaseModel):
    title: str
    starts_at: datetime
    ends_at: datetime
    notes: str | None = None


class Meal(BaseModel):
    date: date
    meal_type: Literal["breakfast", "lunch", "dinner", "snack"]
    content: str
    calories: float | None = None
    protein_g: float | None = None
    fat_g: float | None = None
    carbs_g: float | None = None


class DateRange(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    from_: date = Field(alias="from")
    to: date


class Context(BaseModel):
    """The initial context Go assembles for every message (project-review 3.6)."""

    target_date: date
    now: datetime
    timezone: str
    profile: Profile | None
    events: list[Event]
    events_range: DateRange
    meals: list[Meal]
    meals_range: DateRange


class ReplyRequest(BaseModel):
    """Body of POST /internal/reply, sent by Go."""

    message: str = Field(min_length=1, max_length=500)
    context: Context
    # Short-lived token Go issued for this request; sent back on tool calls.
    token: str = Field(min_length=1)


class Recommendation(BaseModel):
    name: str = Field(min_length=1, max_length=40, description="菜名或食物组合")
    description: str = Field(min_length=1, max_length=120, description="简短说明")
    reason: str = Field(
        min_length=1,
        max_length=120,
        description="与用户的 Profile、行程或饮食记录相关的推荐理由",
    )


class SubmitReply(BaseModel):
    """Arguments of the submit_reply tool, which is also the API response."""

    reply: str = Field(min_length=1, max_length=600, description="给用户的回复文字")
    recommendations: list[Recommendation] = Field(
        default_factory=list, description="没有推荐时为空，有推荐时正好 3 个"
    )
    ask_meal_type: bool = Field(
        default=False,
        description="目标日期不是今天且用户没说餐次时为 true，此时不给推荐",
    )

    @field_validator("recommendations")
    @classmethod
    def zero_or_three(cls, v: list[Recommendation]) -> list[Recommendation]:
        if len(v) not in (0, 3):
            raise ValueError("recommendations 必须为空或正好 3 个")
        return v

    @model_validator(mode="after")
    def no_recommendations_when_asking(self) -> "SubmitReply":
        if self.ask_meal_type and self.recommendations:
            raise ValueError("ask_meal_type 为 true 时不能给推荐")
        return self
