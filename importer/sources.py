"""The Co-op's menu. Opening hours are fixed in the backend (plans/fixed-hours.md)."""

import re
import time
from urllib.parse import parse_qs, urlsplit

from menu_listing import DIRECTORY, direct_text, fetch, menu

SHOPS = {
    "650111": ("yoshida", "central_cafeteria"),
    "650112": ("yoshida", "dining_yoshida"),
    "650113": ("yoshida", "dining_northern"),
    "650115": ("yoshida", "dining_southern"),
    "650116": ("uji", "uji_shokudo"),
    "650118": ("yoshida", "cafe_lune"),
    "650120": ("katsura", "selene"),
}
MACROS = {"エネルギー": ("calories", "kcal"), "タンパク質": ("protein_g", "g"),
          "脂質": ("fat_g", "g"), "炭水化物": ("carbs_g", "g")}
QUANTITY = re.compile(r"(-|\d+(?:\.\d+)?)\s*(kcal|[mμµ]?g)")


def listed_items(categories, store):
    result = {}
    for group in categories:
        for row in group["items"]:
            parts = urlsplit(row["url"])
            params = parse_qs(parts.query)
            codes = params.get("c", [])
            if (parts.scheme != "https" or parts.netloc != urlsplit(DIRECTORY).netloc
                    or parts.path != "/sp/detail.php" or params.get("t") != [store]
                    or len(codes) != 1 or not re.fullmatch(r"[A-Za-z0-9_-]+", codes[0])):
                raise ValueError("Unexpected menu detail link")
            code = codes[0]
            item = {key: row[key] for key in ("name", "name_en", "member_price_yen", "status")}
            item.update(dish_code=code, source_url=row["url"])
            if code in result and any(result[code][key] != value for key, value in item.items()):
                raise ValueError(f"Conflicting listing for dish {code}")
            result.setdefault(code, {**item, "categories": set()})["categories"].add(group["category"])
    for item in result.values():
        item["categories"] = sorted(item["categories"])
    return sorted(result.values(), key=lambda item: item["dish_code"])


def details(page, expected_name):
    title = page.select_one("#main h1")
    if title is None or direct_text(title) != expected_name:
        raise ValueError("Missing or mismatched detail page title")
    result = {"allergens": None, "extra_nutrients": {}}
    for row in page.select("#main ul.detail > li"):
        label, value = row.select_one(":scope > strong"), row.select_one(":scope > .price")
        if not label or not value:
            continue
        name = label.get_text(strip=True)
        quantity = QUANTITY.fullmatch(value.get_text(strip=True))
        if name in MACROS:
            field, unit = MACROS[name]
            if not quantity or quantity[2] != unit or field in result:
                raise ValueError(f"Unexpected {name} quantity")
            result[field] = None if quantity[1] == "-" else float(quantity[1])
        elif quantity and quantity[1] != "-":
            result["extra_nutrients"][name] = {"value": float(quantity[1]), "unit": quantity[2]}
    if "calories" not in result:
        raise ValueError("Energy row missing")
    if result["calories"] is None:
        return None
    for field, _ in MACROS.values():
        result.setdefault(field, None)
    icons = page.select_one("li.allergy ul.icon-list")
    if icons is not None:
        markers = set()
        for icon in icons.find_all("li", recursive=False):
            classes = [c.removeprefix("icon_") for c in icon.get("class", []) if c.startswith("icon_")]
            if len(classes) != 1:
                break
            markers.add(classes[0])
        else:
            result["allergens"] = sorted(markers)
    return result


class OfficialMenus:
    """A source cache lasts one scheduler tick. Failed reads can be retried."""

    def listing(self, store, name):
        return listed_items(menu(name, store)["categories"], store)

    def detail(self, item):
        time.sleep(0.5)
        return details(fetch(item["source_url"]), item["name"])
