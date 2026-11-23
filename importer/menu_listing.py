"""Fetch the Kyoto University Co-op's published menus as a JSON snapshot.

    uv run scripts/kyoto_menu.py --store 北部食堂
    uv run scripts/kyoto_menu.py > kyoto-menus.json

The timestamp is the observation time in Japan. The site may change dishes
without notice. Prices are for Co-op members.
"""

import argparse
import json
import re
import time
from datetime import datetime
from urllib.error import URLError
from urllib.parse import parse_qs, urljoin, urlsplit
from urllib.request import Request, urlopen
from zoneinfo import ZoneInfo

from bs4 import BeautifulSoup

DIRECTORY = "https://west2-univ.jp/sp/kyoto-univ.php"


def read_bytes(url):
    request = Request(url, headers={"User-Agent": "mogumogu-menu-importer/1.0"})
    try:
        with urlopen(request, timeout=20) as response:
            return response.read()
    except (URLError, OSError, UnicodeError) as exc:
        raise ValueError(f"Could not read {url}: {exc}") from exc


def fetch(url):
    return BeautifulSoup(read_bytes(url).decode("utf-8-sig"), "html.parser")


def direct_text(tag):
    return "".join(tag.find_all(string=True, recursive=False)).strip()


def stores():
    directory = fetch(DIRECTORY)
    result = {}
    for link in directory.select('#store a[href^="index.php?"]'):
        name = direct_text(link)
        store_id = parse_qs(urlsplit(link["href"]).query).get("t", [""])[0]
        if not name or not store_id.isdigit():
            raise ValueError(f"Unexpected cafeteria link: {link['href']}")
        result[name] = store_id
    if not result:
        raise ValueError(f"No cafeterias found at {DIRECTORY}")
    return result


def dishes(fragment, source):
    if not fragment.select_one(".Loaded"):
        raise ValueError(f"Missing category content at {source}")
    items = []
    for row in fragment.select("ul > li"):
        link = row.select_one('a[href^="detail.php?"]')
        title = row.select_one("h3")
        price = row.select_one("h3 .price")
        if not (link and title and direct_text(title) and price):
            raise ValueError(f"Unrecognized menu item at {source}")
        yen = re.fullmatch(r"[¥￥]\s*([\d,]+)", price.get_text(strip=True))
        if not yen:
            raise ValueError(f"Unrecognized price {price.get_text()!r} at {source}")
        english = title.select_one("span:not(.price)")
        badge = row.select_one(".sold")
        status = "unmarked"
        if badge is None:
            status = "unknown"
        elif "out" in badge["class"]:
            status = "sold_out"
        elif "standby" in badge["class"]:
            status = "standby"
        elif set(badge["class"]) != {"sold"}:
            status = "unknown"
        items.append({
            "name": direct_text(title),
            "name_en": english.get_text(strip=True) if english else None,
            "member_price_yen": int(yen[1].replace(",", "")),
            "status": status,
            "url": urljoin(source, link["href"]),
        })
    return items


def menu(name, store_id):
    url = urljoin(DIRECTORY, f"menu.php?t={store_id}")
    page = fetch(url)
    headings = page.select("#menu .toggleTitle")
    if not headings:
        raise ValueError(f"No menu categories found at {url}")
    categories = []
    for heading in headings:
        category_id = heading.get("id", "")
        if not re.fullmatch(r"on_\w+", category_id):
            raise ValueError(f"Unrecognized category {category_id!r} at {url}")
        source = urljoin(url, f"menu_load.php?t={store_id}&a={category_id}")
        time.sleep(0.5)
        categories.append({
            "category": heading.get_text(strip=True),
            "items": dishes(fetch(source), source),
        })
    return {"name": name, "store_id": store_id, "url": url, "categories": categories}


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--store", help="Cafeteria name or ID (default: all cafeterias)")
    args = parser.parse_args()
    observed = datetime.now(ZoneInfo("Asia/Tokyo")).isoformat(timespec="seconds")
    directory = stores()
    selected = [(n, i) for n, i in directory.items() if args.store in (None, n, i)]
    if not selected:
        parser.error("Unknown cafeteria. Choose from: " + ", ".join(directory))
    snapshot = {
        "observed_at": observed,
        "source": DIRECTORY,
        "cafeterias": [menu(name, store_id) for name, store_id in selected],
    }
    print(json.dumps(snapshot, ensure_ascii=False, indent=2))
