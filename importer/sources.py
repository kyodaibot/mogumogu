"""The Co-op's menu, opening-hours feed and reviewed exceptional timetables."""

import hashlib
import json
import re
import time
from functools import cache
from pathlib import Path
from urllib.parse import parse_qs, urljoin, urlsplit

from menu_listing import DIRECTORY, direct_text, fetch, menu, read_bytes

HOURS = "https://www.s-coop.net/scms/shophours/getjsonall"
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
TIME = re.compile(r"(?:[01]\d|2[0-3]):[0-5]\d")


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


def regular_hours(feed, shop, day):
    holiday = day.isoformat() in {item["holiday"] for item in feed["holidays"]}
    kind = "holi" if holiday else ("sat" if day.weekday() == 5 else "sun" if day.weekday() == 6 else "day")
    rows = [row for row in feed["shophours"] if row["pagelink"] == shop]
    if len(rows) != 1:
        return None, holiday
    row = rows[0]
    if str(row.get("tmp_close")) == "1":
        return [], holiday
    if f"open_{kind}" not in row or f"close_{kind}" not in row:
        return None, holiday
    intervals = []
    for suffix in ("", "_2"):
        start, end = row.get(f"open_{kind}{suffix}"), row.get(f"close_{kind}{suffix}")
        if start is None and end is None:
            continue
        if not start or not end or not TIME.fullmatch(start) or not TIME.fullmatch(end) or start >= end:
            return None, holiday
        intervals.append({"start": start, "end": end})
    return intervals, holiday


@cache
def reviewed_notices():
    return json.loads(Path(__file__).with_name("hours_notices.json").read_text())


def exception_hours(digest, store, day):
    timetable = reviewed_notices().get(digest)
    if timetable is None:
        return True, None
    dates = timetable["dates"]
    if day.isoformat() not in dates:
        return False, None
    periods = timetable["shops"][store][dates.index(day.isoformat())]
    return True, [{"start": span[:5], "end": span[6:]} for span in periods]


class OfficialMenus:
    """A source cache lasts one scheduler tick. Failed reads can be retried."""

    def __init__(self):
        self.feed = None
        self.notices = {}
        self.digests = {}

    def listing(self, store, name):
        return listed_items(menu(name, store)["categories"], store)

    def detail(self, item):
        time.sleep(0.5)
        return details(fetch(item["source_url"]), item["name"])

    def hours(self, store, day):
        if self.feed is None:
            self.feed = json.loads(read_bytes(HOURS))
        campus, anchor = SHOPS[store]
        intervals, holiday = regular_hours(self.feed, anchor, day)
        url = f"https://www.s-coop.net/shop_guide/{campus}_shop/"
        if url not in self.notices:
            page = fetch(url)
            if not page.select_one("#shopHours"):
                raise ValueError("Missing campus opening-hours page")
            links = []
            for link in page.select('a[href]'):
                label = link.get_text(strip=True)
                href = urljoin(url, link["href"])
                if ("営業時間" in label and urlsplit(href).path.lower().endswith(".pdf")
                        and urlsplit(href).netloc == "www.s-coop.net"):
                    links.append(href)
            self.notices[url] = list(dict.fromkeys(links))
        source = url + "#" + anchor
        exceptions = []
        for href in self.notices[url]:
            if href not in self.digests:
                self.digests[href] = hashlib.sha256(read_bytes(href)).hexdigest()
            applicable, periods = exception_hours(self.digests[href], store, day)
            if applicable:
                exceptions.append(periods)
                source = href
        if exceptions:
            intervals = exceptions[0] if all(value == exceptions[0] for value in exceptions) else None
        return intervals, holiday, source
