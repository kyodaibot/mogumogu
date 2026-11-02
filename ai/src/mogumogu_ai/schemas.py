"""Data exchanged with the Go backend and with the model.

Field names follow docs/api.md so Go can pass its own JSON through unchanged.
"""

from datetime import date, datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


MealType = Literal["breakfast", "lunch", "dinner", "snack"]

# Sources one reply may cite; Go has the same limit.
MAX_SOURCES = 5


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
    # The record's id; update_meal and delete_meal name a record by it.
    id: int
    date: date
    meal_type: MealType
    content: str
    calories: float | None = None
    protein_g: float | None = None
    fat_g: float | None = None
    carbs_g: float | None = None


class Memory(BaseModel):
    """A memory (GLOSSARY: 记忆): a lasting preference or fact the user stated.
    update_memory and forget name one by its id."""

    id: int
    content: str


class DateRange(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    from_: date = Field(alias="from")
    to: date


class Context(BaseModel):
    """The initial context Go assembles for every message (project-review 3.6)."""

    target_date: date
    now: datetime
    timezone: str
    # The interface language, which the reply is written in.
    locale: Literal["ja", "en"] = "en"
    # Whether the staging tools are offered (Pro only).
    allow_proposal: bool = False
    # Whether the memory tools are offered (Pro only); memories is empty otherwise.
    allow_memory: bool = False
    # Whether web_search is offered (Pro only).
    allow_search: bool = False
    memories: list[Memory] = Field(default_factory=list)
    profile: Profile | None
    events: list[Event]
    events_range: DateRange
    meals: list[Meal]
    meals_range: DateRange


class HistoryDish(BaseModel):
    """What the conversation history keeps of a recommendation."""

    name: str = Field(min_length=1, max_length=80)
    description: str = Field(min_length=1, max_length=200)


class HistoryChange(BaseModel):
    """What the conversation history keeps of a proposed change."""

    op: Literal["create", "update", "delete"]
    date: date
    meal_type: MealType
    content: str = Field(min_length=1, max_length=200)


class Turn(BaseModel):
    """One earlier turn of the conversation (project-review 3.6). The browser
    sends it back and Go only checks its shape, so it is untrusted."""

    date: date
    message: str = Field(min_length=1, max_length=500)
    reply: str = Field(min_length=1, max_length=1000)
    recommendations: list[HistoryDish] = Field(default_factory=list)
    # The change proposal of that reply, if it had one.
    changes: list[HistoryChange] = Field(default_factory=list, max_length=10)

    @field_validator("recommendations")
    @classmethod
    def zero_or_three(cls, v: list[HistoryDish]) -> list[HistoryDish]:
        if len(v) not in (0, 3):
            raise ValueError("recommendations must be empty or exactly 3")
        return v


class ReplyRequest(BaseModel):
    """Body of POST /internal/reply, sent by Go."""

    message: str = Field(min_length=1, max_length=500)
    context: Context
    # Earlier turns, oldest first; at most 10 turns in a conversation.
    history: list[Turn] = Field(default_factory=list, max_length=9)
    # Short-lived token Go issued for this request; sent back on tool calls.
    token: str = Field(min_length=1)


# Atwater factors: kcal per gram of protein, carbs and fat.
_KCAL_PER_G = {"protein_g": 4, "carbs_g": 4, "fat_g": 9}


def _check_energy(n: "ServingNutrition | Per100gNutrition") -> None:
    """Calories must roughly match what the three macronutrients add up to.

    The 15 kcal floor keeps rounding from failing low-calorie food.
    """
    expected = sum(getattr(n, k) * f for k, f in _KCAL_PER_G.items())
    if abs(expected - n.calories) > max(0.25 * n.calories, 15):
        raise ValueError(
            f"calories {n.calories:g} do not match protein, fat and carbs "
            f"(4*P + 4*C + 9*F = {expected:g}); fix the numbers"
        )


def _add_unit(data):
    """The model sometimes writes protein, fat and carbs without _g; the
    numbers are fine, so take them rather than reject the reply."""
    if isinstance(data, dict):
        for name in ("protein", "fat", "carbs"):
            if name in data and f"{name}_g" not in data:
                data = {**data, f"{name}_g": data[name]}
                del data[name]
    return data


class ServingNutrition(BaseModel):
    """One typical serving, in total. Same ranges as a meal record."""

    calories: float = Field(ge=0, le=10000, description="kcal")
    protein_g: float = Field(ge=0, le=1000)
    fat_g: float = Field(ge=0, le=1000)
    carbs_g: float = Field(ge=0, le=1000)

    @model_validator(mode="before")
    @classmethod
    def add_unit(cls, data):
        return _add_unit(data)

    @model_validator(mode="after")
    def energy_matches(self) -> "ServingNutrition":
        _check_energy(self)
        return self

    def rounded(self) -> "ServingNutrition":
        """kcal to 10, grams to 1."""
        return ServingNutrition.model_construct(
            calories=_round(self.calories, 10), protein_g=_round(self.protein_g),
            fat_g=_round(self.fat_g), carbs_g=_round(self.carbs_g),
        )


class Per100gNutrition(BaseModel):
    """Per 100 g of the food (a combination: of the whole serving mixed)."""

    calories: float = Field(ge=0, le=900, description="kcal per 100 g")
    protein_g: float = Field(ge=0, le=100)
    fat_g: float = Field(ge=0, le=100)
    carbs_g: float = Field(ge=0, le=100)

    @model_validator(mode="before")
    @classmethod
    def add_unit(cls, data):
        return _add_unit(data)

    @model_validator(mode="after")
    def fits_in_100g(self) -> "Per100gNutrition":
        if self.protein_g + self.fat_g + self.carbs_g > 100:
            raise ValueError("protein, fat and carbs per 100 g add up to more than 100 g")
        _check_energy(self)
        return self


class Estimate(BaseModel):
    """A rough nutrition estimate (GLOSSARY: 营养估算), not a measurement."""

    serving: ServingNutrition = Field(description="Total for one typical serving")
    per_100g: Per100gNutrition = Field(description="Per 100 g, for reference")

    @model_validator(mode="after")
    def serving_weight_is_plausible(self) -> "Estimate":
        # The two are given separately; the weight they imply only has to be sane.
        if self.per_100g.calories > 0:
            grams = self.serving.calories / self.per_100g.calories * 100
            if not 30 <= grams <= 2000:
                raise ValueError(
                    f"serving and per_100g imply a serving of {grams:.0f} g, outside 30-2000 g; fix the numbers"
                )
        return self

    def rounded(self) -> "Estimate":
        """Serving: kcal to 10, grams to 1. Per 100 g: kcal and grams to 1."""
        p = self.per_100g
        return Estimate.model_construct(
            serving=self.serving.rounded(),
            per_100g=Per100gNutrition.model_construct(
                calories=_round(p.calories), protein_g=_round(p.protein_g),
                fat_g=_round(p.fat_g), carbs_g=_round(p.carbs_g),
            ),
        )


def _round(x: float, step: int = 1) -> int:
    # Half up, not Python's round-half-to-even; the values are never negative.
    return int(x / step + 0.5) * step


class Recommendation(BaseModel):
    # Limits leave room for English, which is longer than Japanese.
    name: str = Field(min_length=1, max_length=80, description="Dish or combination of foods")
    description: str = Field(min_length=1, max_length=200, description="Short description")
    reason: str = Field(
        min_length=1,
        max_length=200,
        description="Why it suits the user, based on their profile, schedule or meal records",
    )
    estimate: Estimate


class SubmitReply(BaseModel):
    """Arguments of the submit_reply tool, which is also the API response."""

    reply: str = Field(min_length=1, max_length=1000, description="Reply text for the user")
    recommendations: list[Recommendation] = Field(
        default_factory=list, description="Empty without recommendations, otherwise exactly 3"
    )
    ask_meal_type: bool = Field(
        default=False,
        description="True when the target date is not today and the user did not say which meal; then no recommendations",
    )
    meal_type: MealType | None = Field(
        default=None,
        description="The meal all three recommendations are for; required with recommendations, otherwise null",
    )
    sources: list[str] = Field(
        default_factory=list,
        description="url of each web_search result the reply uses, copied exactly; empty when you did not search",
    )

    @field_validator("recommendations")
    @classmethod
    def zero_or_three(cls, v: list[Recommendation]) -> list[Recommendation]:
        if len(v) not in (0, 3):
            raise ValueError("recommendations must be empty or exactly 3")
        return v

    @field_validator("sources")
    @classmethod
    def at_most_five_sources(cls, v: list[str]) -> list[str]:
        v = list(dict.fromkeys(u.strip() for u in v))
        if len(v) > MAX_SOURCES:
            raise ValueError(f"at most {MAX_SOURCES} sources; keep the ones the reply relies on most")
        return v

    @model_validator(mode="before")
    @classmethod
    def no_meal_type_without_recommendations(cls, data):
        # A text-only reply often still carries the meal it was about (or a
        # made-up value). It means nothing there, so drop it rather than
        # reject a reply that is otherwise fine.
        if isinstance(data, dict) and not data.get("recommendations"):
            return {**data, "meal_type": None}
        return data

    @model_validator(mode="after")
    def no_recommendations_when_asking(self) -> "SubmitReply":
        if self.ask_meal_type and self.recommendations:
            raise ValueError("no recommendations allowed when ask_meal_type is true")
        if self.recommendations and self.meal_type is None:
            raise ValueError("meal_type is required with recommendations")
        return self

    def rounded(self) -> "SubmitReply":
        """The reply as sent to Go, with every estimate rounded."""
        recs = [r.model_copy(update={"estimate": r.estimate.rounded()}) for r in self.recommendations]
        return self.model_copy(update={"recommendations": recs})


class Source(BaseModel):
    """A web page the reply cites (GLOSSARY: 来源). The title comes from the
    search result, not from the model."""

    title: str
    url: str


class Reply(SubmitReply):
    """Body of the response to Go: the reply, the changes staged with it
    (tools.Staging), which Go checks and saves as a proposal, and the changes
    to memories (tools.MemoryChanges), which Go checks and saves at once.
    sources gets each URL's title here (the model gives only URLs)."""

    sources: list[Source] = Field(default_factory=list)  # type: ignore[assignment]
    changes: list[dict] = Field(default_factory=list)
    memory_changes: list[dict] = Field(default_factory=list)
