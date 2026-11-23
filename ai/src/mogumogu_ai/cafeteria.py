"""Request-local evidence for cafeteria selections. Go owns prices, nutrition and rendered cards."""

import json
from datetime import date
from typing import Literal, Protocol

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from .model import ToolSpec
from .schemas import Campus, MealType, SubmitReply


class MenuArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    meal_type: MealType
    time: str | None = Field(default=None, pattern=r"^([01]\d|2[0-3]):[0-5]\d$",
                             description="Requested JST eating time, including the current time for 'eat now'")
    campus: Campus | Literal["all"] | None = Field(default=None, description="Default: account campus. "
                                                   "Use all only after the user agrees to other campuses.")
    cafeteria: str | None = Field(default=None, min_length=1, max_length=80,
                                  description="Official store id or exact Japanese name, only if specified.")
    use_today: bool = Field(default=False, description="Explicit consent to use today's menu for tomorrow")


GET_MENU = ToolSpec(
    "get_cafeteria_menu",
    "Read the imported Kyoto University co-op menu for the conversation date, meal, eating time and campus. "
    "Available for Normal and Pro users. Use before submitting any meal selection.",
    MenuArgs,
)


class MenuSource(Protocol):
    async def fetch_menu(self, params: dict[str, str]) -> dict: ...


def _served_together(items: list[dict]) -> bool:
    windows = [item["windows"] for item in items if "windows" in item]
    if not windows:
        return True
    # A shared interval starts at the beginning of at least one item's interval.
    starts = {w["start"] for choices in windows for w in choices}
    return any(all(any(w["start"] <= start < w["end"] for w in choices) for choices in windows)
               for start in starts)


class MenuQueries:
    def __init__(self, target_date: date):
        self.target_date = target_date
        self.current: tuple[MealType, dict] | None = None

    async def fetch(self, source: MenuSource, args: dict) -> str:
        try:
            query = MenuArgs.model_validate(args)
        except ValidationError as e:
            return f"Invalid menu arguments: {e.errors()[0]['msg']}"
        params = query.model_dump(exclude_none=True, exclude_defaults=True)
        if query.use_today:
            params["use_today"] = "true"
        self.current = None
        data = await source.fetch_menu({**params, "date": self.target_date.isoformat()})
        if data.get("status") == "ready" and data.get("query_id"):
            self.current = query.meal_type, data
        return json.dumps(data, ensure_ascii=False, separators=(",", ":"))

    def check(self, reply: SubmitReply) -> str | None:
        """Structure only. Allergens are the model's job: Go gives each dish's official
        labels in Japanese and English, and the prompt compares them with the user's words."""
        if reply.recommendations:
            return "Use the current cafeteria menu. Submit only menu_selections, or text if the menu is unavailable."
        if not reply.menu_selections:
            return None
        query = self.current
        if query is None or query[0] != reply.meal_type or query[1]["query_id"] != reply.menu_query_id:
            return "Select items only from a successful menu query for this meal in this turn."
        breakfast = reply.meal_type == "breakfast"
        if breakfast and len(reply.menu_selections) != 1:
            return "Group the available breakfast plate alternatives on a single card."
        stores = {store["id"]: {item["id"]: item for item in store["items"]} for store in query[1]["stores"]}
        bundles = set()
        for choice in reply.menu_selections:
            bundle = frozenset((item.id, item.portions) for item in choice.items)
            ids = {i.id for i in choice.items}
            if len(ids) != len(choice.items) or bundle in bundles or len(set(choice.store_ids)) != len(choice.store_ids):
                return "Combine duplicate choices, and list each store and dish only once per card."
            bundles.add(bundle)
            if breakfast and any(i.portions != 1 for i in choice.items):
                return "Each breakfast alternative is one plate."
            for store in choice.store_ids:
                if store not in stores or not ids <= stores[store].keys():
                    return "Every item must come from this query and be offered by every listed store."
                items = [stores[store][dish] for dish in ids]
                if not _served_together(items):
                    return "Choose dishes whose serving windows overlap at this store."
        return None
