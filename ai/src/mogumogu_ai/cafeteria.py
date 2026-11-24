"""Request-local evidence for cafeteria selections. Go owns prices, nutrition and rendered cards."""

import calendar
import datetime
import json
from datetime import date
from typing import Literal, Protocol

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from .model import ToolSpec
from .schemas import Campus, MealType, SubmitReply


class MenuArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    meal_type: MealType
    date: datetime.date | None = Field(default=None, description="The day the user will eat, when they name one (今日, 明日, "
                              "a weekday): work it out from context.now, not the calendar. Default: target_date.")
    campus: Campus | Literal["all"] | None = Field(default=None, description="Default: account campus. "
                                                   "Use all only after the user agrees to other campuses.")
    cafeteria: str | None = Field(default=None, min_length=1, max_length=80,
                                  description="Official store id or exact Japanese name, only if specified.")


GET_MENU = ToolSpec(
    "get_cafeteria_menu",
    "Read the imported Kyoto University co-op menu for a day, meal and campus. "
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


def _names_date(text: str, day: date) -> bool:
    month = calendar.month_name[day.month]
    forms = (day.isoformat(), f"{day.month}/{day.day}", f"{day.month:02}/{day.day:02}", f"{day.month}月{day.day}日",
             f"{month} {day.day}", f"{month[:3]} {day.day}", f"{day.day} {month}", f"{day.day} {month[:3]}")
    return any(form in text for form in forms)


class MenuQueries:
    def __init__(self, target_date: date, locale: str = "ja"):
        self.target_date = target_date
        self.locale = locale
        self.current: tuple[MealType, dict] | None = None

    async def fetch(self, source: MenuSource, args: dict) -> str:
        try:
            query = MenuArgs.model_validate(args)
        except ValidationError as e:
            return f"Invalid menu arguments: {e.errors()[0]['msg']}"
        params = query.model_dump(mode="json", exclude_none=True)
        self.current = None
        data = await source.fetch_menu({**params, "date": (query.date or self.target_date).isoformat()})
        if data.get("status") == "ready" and data.get("query_id"):
            self.current = query.meal_type, data
        earlier = sorted({s["menu_date"] for s in data.get("stores", []) if s.get("menu_date", data.get("menu_date")) != data.get("menu_date")})
        if earlier:
            # Said in words: the model misses a date that differs only inside each store.
            day = data["menu_date"]
            data = {"notice": f"Stores whose menu_date is {' or '.join(earlier)} have not imported {day}'s menu yet; this "
                              f"is their latest menu. If you select one, name that date in reply and say {day}'s menu "
                              "has not been updated yet.", **data}
        return json.dumps(data, ensure_ascii=False, separators=(",", ":"))

    # Fix-ups for mistakes with only one right answer, instead of a retry that
    # may fail again and end in a 503 (plans/menu-reply-autofix.md).
    def fix_query_id(self, args: dict) -> dict:
        """Cards without menu_query_id come from the latest successful query (check() still
        verifies them); a menu_query_id without cards means nothing and is dropped."""
        if not isinstance(args, dict):
            return args
        if not args.get("menu_selections") and args.get("menu_query_id") is not None:
            return {k: v for k, v in args.items() if k != "menu_query_id"}
        if args.get("menu_selections") and not args.get("menu_query_id") and self.current is not None:
            return {**args, "menu_query_id": self.current[1]["query_id"]}
        return args

    def merge_breakfast(self, reply: SubmitReply) -> SubmitReply:
        """Breakfast plates are alternatives on one card: merge cards split by plate."""
        if reply.meal_type != "breakfast" or len(reply.menu_selections) < 2:
            return reply
        first = reply.menu_selections[0]
        items, stores = {}, []
        for choice in reply.menu_selections:
            for item in choice.items:
                items.setdefault(item.id, item.model_copy(update={"portions": 1}))
            stores += [s for s in choice.store_ids if s not in stores]
        merged = first.model_copy(update={"items": list(items.values()), "store_ids": stores})
        return reply.model_copy(update={"menu_selections": [merged]})

    def prune_stores(self, reply: SubmitReply) -> SubmitReply:
        """Drop stores that do not serve a card's whole combination. A card with no
        such store is left as it is, for check() to reject."""
        if self.current is None or not reply.menu_selections:
            return reply
        stores = {store["id"]: {item["id"]: item for item in store["items"]} for store in self.current[1]["stores"]}
        def serves(store, choice):
            ids = {i.id for i in choice.items}
            return store in stores and ids <= stores[store].keys() and _served_together([stores[store][d] for d in ids])
        choices = []
        for choice in reply.menu_selections:
            kept = [s for s in choice.store_ids if serves(s, choice)]
            choices.append(choice.model_copy(update={"store_ids": kept}) if kept else choice)
        return reply.model_copy(update={"menu_selections": choices})

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
            # Name the card: the model fixes it more easily, and the log says what went wrong.
            card = f"(card {reply.menu_selections.index(choice) + 1}: items {sorted(ids)}, stores {choice.store_ids})"
            for store in choice.store_ids:
                if store not in stores or not ids <= stores[store].keys():
                    return f"Every item must come from this query and be offered by every listed store {card}."
                items = [stores[store][dish] for dish in ids]
                if not _served_together(items):
                    return f"Choose dishes whose serving windows overlap at this store {card}."
        return None

    def disclose(self, reply: SubmitReply) -> SubmitReply:
        """A selected store without the day's own menu shows an earlier one. The card
        says so; when the reply text does not, add a sentence instead of rejecting it."""
        if self.current is None or not reply.menu_selections:
            return reply
        data = self.current[1]
        selected = {s for choice in reply.menu_selections for s in choice.store_ids}
        earlier = sorted({date.fromisoformat(store["menu_date"]) for store in data["stores"]
                          if store["id"] in selected and store.get("menu_date", data["menu_date"]) != data["menu_date"]})
        missing = [d for d in earlier if not _names_date(reply.reply, d)]
        if not missing:
            return reply
        days = "・".join(f"{d.month}/{d.day}" for d in missing)
        eaten = date.fromisoformat(data["menu_date"])
        day = f"{eaten.month}/{eaten.day}"
        note = (f"\n\n※ {days} のメニューです。{day} のメニューはまだ更新されていません。" if self.locale == "ja"
                else f"\n\nNote: this is the {days} menu; the {day} menu has not been updated yet.")
        if len(reply.reply) + len(note) > 1000:
            return reply  # the card's own notice still shows the date
        return reply.model_copy(update={"reply": reply.reply + note})
