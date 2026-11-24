"""Advance each due shop by one attempt, atomically publishing verified rows."""

import argparse
import hashlib
import json
import logging
import os
from datetime import datetime, time, timedelta
from decimal import Decimal
from zoneinfo import ZoneInfo

import psycopg
from psycopg import sql
from psycopg.rows import dict_row
from psycopg.types.json import Jsonb

from sources import OfficialMenus, SHOPS

JST = ZoneInfo("Asia/Tokyo")
FIELDS = ("dish_code", "name", "name_en", "categories", "member_price_yen", "status", "source_url",
          "allergens", "calories", "protein_g", "fat_g", "carbs_g", "extra_nutrients")
SOURCE_ERRORS = (ValueError, KeyError, TypeError, OSError)


def due_phase(now, retries):
    if now.time() < time(8):
        return None
    phase = "morning" if now.time() < time(10, 30) else "review"
    if phase in retries and (retries[phase] is None or now < retries[phase]):
        return None
    return phase


def retry_delay(attempt):
    return timedelta(minutes=10 if attempt == 1 else 30 if attempt == 2 else 60)


def load_items(db, snapshot_id):
    if snapshot_id is None:
        return []
    rows = db.execute(sql.SQL("SELECT {} FROM menu_items WHERE snapshot_id = %s").format(
        sql.SQL(", ").join(map(sql.Identifier, FIELDS))), (snapshot_id,)).fetchall()
    return [{key: float(v) if isinstance(v, Decimal) else v for key, v in row.items()} for row in rows]


def publish(db, store, day, checked_at, items):
    items.sort(key=lambda item: item["dish_code"])
    encoded = json.dumps(items, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    digest = hashlib.sha256(encoded.encode()).hexdigest()
    previous = db.execute("""
        SELECT id, version, digest FROM menu_snapshots WHERE cafeteria_id=%s AND menu_date=%s
        ORDER BY version DESC LIMIT 1""", (store, day)).fetchone()
    if previous and previous["digest"] == digest:
        db.execute("UPDATE menu_snapshots SET checked_at=%s WHERE id=%s", (checked_at, previous["id"]))
        return previous["id"]
    version = previous["version"]+1 if previous else 1
    result = db.execute("""
        INSERT INTO menu_snapshots(cafeteria_id,menu_date,version,checked_at,digest)
        VALUES (%s,%s,%s,%s,%s) RETURNING id""", (store, day, version, checked_at, digest)).fetchone()
    statement = sql.SQL("INSERT INTO menu_items ({}) VALUES ({})").format(
        sql.SQL(", ").join(map(sql.Identifier, ("snapshot_id", *FIELDS))),
        sql.SQL(", ").join(sql.Placeholder() for _ in range(len(FIELDS)+1)))
    with db.cursor() as cur:
        cur.executemany(statement, [
            (result["id"], *(Jsonb(item[key]) if key == "extra_nutrients" else item[key] for key in FIELDS))
            for item in items])
    return result["id"]


def fetch_menu(source, store, name, pending):
    """Returns the verified items, the entries still to retry, and whether the full listing was read."""
    full = pending is None
    if full:
        pending = source.listing(store, name)
    items, failed = [], []
    for entry in pending:
        try:
            nutrients = source.detail(entry)
        except SOURCE_ERRORS as exc:
            logging.warning("Menu %s dish %s: %s", store, entry["dish_code"], exc)
            failed.append(entry)
            continue
        if nutrients is not None:
            items.append({**entry, **nutrients})
    return items, failed, full, {entry["dish_code"] for entry in pending}


def merge(base_items, new_items, failed, full, attempted):
    """A failed detail keeps today's verified row instead of dropping the dish.

    A full listing is authoritative: dishes it no longer lists disappear. A
    retry of some details only replaces the rows it settled (read or skipped).
    """
    retry = {entry["dish_code"] for entry in failed}
    settled = attempted - retry
    keep = [item for item in base_items
            if (item["dish_code"] in retry if full else item["dish_code"] not in settled)]
    return keep + new_items


def advance(db, store, name, source, clock, now, jobs, phase):
    day = now.date()
    previous = jobs.get(phase, {})
    attempt = previous.get("attempts", 0) + 1
    pending = previous.get("pending_details")
    base = previous.get("base_snapshot_id")
    if base is None:
        # The first attempt of a phase builds on today's latest verified snapshot.
        latest = db.execute("""
            SELECT id FROM menu_snapshots WHERE cafeteria_id=%s AND menu_date=%s
            ORDER BY version DESC LIMIT 1""", (store, day)).fetchone()
        base = latest["id"] if latest else None
    items = None
    errors = []
    refresh = pending is None or bool(pending)
    if refresh:
        try:
            new_items, failed, full, attempted = fetch_menu(source, store, name, pending)
            pending = failed
            if new_items or not failed:
                items = merge(load_items(db, base), new_items, failed, full, attempted)
            elif full:
                # Nothing from this listing was published, so the next attempt reads it again.
                pending = None
                errors.append("no detail could be read")
            if pending:
                errors.append(f"{len(pending)} pending details")
        except SOURCE_ERRORS as exc:
            errors.append(f"category list: {exc}")
    # Opening hours are fixed in the backend (plans/fixed-hours.md); the column stays.
    hours_pending = False
    finished = clock().astimezone(JST)
    if finished.date() != day:
        logging.warning("Discarded %s observation crossing midnight", store)
        return
    with db.transaction():
        if items is not None:
            base = publish(db, store, day, finished, items)
        retry = finished + retry_delay(attempt) if errors else None
        db.execute("""
            INSERT INTO menu_import_jobs(cafeteria_id, menu_date, phase, attempts, next_retry_at,
                                         last_error, pending_details, base_snapshot_id, hours_pending)
            VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s) ON CONFLICT(cafeteria_id,menu_date,phase)
            DO UPDATE SET attempts=excluded.attempts,next_retry_at=excluded.next_retry_at,
              last_error=excluded.last_error,pending_details=excluded.pending_details,
              base_snapshot_id=excluded.base_snapshot_id,hours_pending=excluded.hours_pending""",
            (store,day,phase,attempt,retry,", ".join(errors) or None,
             Jsonb(pending) if pending is not None else None,base,hours_pending))
        if phase == "review":
            # The morning job is superseded even if its next retry was due after 10:30.
            db.execute("""
                UPDATE menu_import_jobs SET next_retry_at=NULL,pending_details=NULL,hours_pending=false,
                  last_error='superseded by review'
                WHERE cafeteria_id=%s AND menu_date=%s AND phase='morning' AND next_retry_at IS NOT NULL""",
                (store,day))
    logging.info("%s %s: %s", store, phase, f"retry at {retry}: {', '.join(errors)}" if errors else "complete")


def run_store(db, store, name, source, clock):
    # A session lock is released on process/connection loss; no network I/O inside a DB transaction.
    locked = db.execute("SELECT pg_try_advisory_lock(650, %s) AS locked", (int(store),)).fetchone()["locked"]
    if not locked:
        return
    try:
        now = clock().astimezone(JST)
        rows = db.execute("SELECT * FROM menu_import_jobs WHERE cafeteria_id=%s AND menu_date=%s",
                          (store,now.date())).fetchall()
        jobs = {row["phase"]:row for row in rows}
        phase = due_phase(now, {k:v["next_retry_at"] for k,v in jobs.items()})
        if phase:
            advance(db, store, name, source, clock, now, jobs, phase)
    finally:
        db.execute("SELECT pg_advisory_unlock(650, %s)", (int(store),))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--store", choices=SHOPS, help="Only process this cafeteria")
    args = parser.parse_args()
    if not os.getenv("MENU_DATABASE_URL"):
        parser.error("Set MENU_DATABASE_URL for the importer database role")
    source = OfficialMenus()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s")
    with psycopg.connect(os.environ["MENU_DATABASE_URL"], autocommit=True, row_factory=dict_row) as db:
        shops = db.execute("SELECT id, name FROM cafeterias ORDER BY id").fetchall()
        for shop in shops:
            if args.store in (None, shop["id"]):
                run_store(db, shop["id"], shop["name"], source, lambda:datetime.now(JST))


if __name__ == "__main__":
    main()
