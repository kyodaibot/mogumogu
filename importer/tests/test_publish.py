import os
import unittest
import uuid
from datetime import datetime, timedelta
from pathlib import Path

import psycopg
from psycopg import sql
from psycopg.rows import dict_row

from jobs import JST, merge, run_store


class Feed:
    def __init__(self):
        self.bad = {"two"}
        self.list_calls = 0
        self.reads = []

    def listing(self, store, name):
        self.list_calls += 1
        return [dict(dish_code=id, name=id, name_en=None, categories=["朝食プレート"],
                     member_price_yen=385, status="unmarked",
                     source_url=f"https://west2-univ.jp/sp/detail.php?t={store}&c={id}")
                for id in ["one","two","missing"]]

    def detail(self, item):
        id = item["dish_code"]
        self.reads.append(id)
        if id in self.bad:
            raise ValueError("temporary error")
        if id == "missing":
            return None
        return dict(calories=310.0, protein_g=5.0, fat_g=None, carbs_g=50.0, allergens=["egg"], extra_nutrients={})

    def hours(self, store, day):
        return [{"start":"08:00","end":"15:15"}],False,"https://www.s-coop.net/"


@unittest.skipUnless(os.environ.get("TEST_DATABASE_URL"), "requires TEST_DATABASE_URL with CREATE DATABASE")
class Publication(unittest.TestCase):
    def setUp(self):
        dsn = os.environ["TEST_DATABASE_URL"]
        self.admin = psycopg.connect(dsn, autocommit=True)
        self.addCleanup(self.admin.close)
        name = "mogumogu_test_menu_" + uuid.uuid4().hex
        self.admin.execute(sql.SQL("CREATE DATABASE {}").format(sql.Identifier(name)))
        self.addCleanup(self.admin.execute, sql.SQL("DROP DATABASE {}").format(sql.Identifier(name)))
        self.db = psycopg.connect(dsn, dbname=name, autocommit=True, row_factory=dict_row)
        self.addCleanup(self.db.close)
        migration = Path(__file__).resolve().parents[2] / "backend/migrations/000011_create_cafeteria_menus.up.sql"
        self.db.execute(migration.read_text())

    def test_partial_resume_recheck_and_midnight(self):
        now = datetime(2026,11,24,8,0,tzinfo=JST)
        feed = Feed()
        def run(at):
            return run_store(self.db, "650111", "中央食堂", feed, lambda:at)
        run(now)
        self.assertEqual(self.db.execute("SELECT dish_code FROM menu_items").fetchone()["dish_code"],"one")
        retry = self.db.execute("SELECT next_retry_at FROM menu_import_jobs").fetchone()["next_retry_at"]
        self.assertEqual(retry,now+timedelta(minutes=10))
        feed.bad.clear()
        feed.reads.clear()
        run(retry-timedelta(seconds=1))
        self.assertEqual(feed.reads, [])
        run(retry)
        self.assertEqual(feed.reads,["two"])
        self.assertEqual(feed.list_calls,1)
        self.assertEqual(self.db.execute("SELECT count(*) AS n FROM menu_snapshots").fetchone()["n"],2)
        run(now+timedelta(hours=2,minutes=30))
        self.assertEqual(feed.list_calls,2)
        self.assertEqual(self.db.execute("SELECT count(*) AS n FROM menu_snapshots").fetchone()["n"],2)
        run(now+timedelta(hours=4))
        self.assertEqual(feed.list_calls,2)
        tomorrow = now + timedelta(days=1)
        run(tomorrow)
        self.assertEqual(self.db.execute("SELECT count(*) AS n FROM menu_snapshots WHERE menu_date=%s",
                                         (tomorrow.date(),)).fetchone()["n"],1)
        start = now + timedelta(days=2,hours=15,minutes=59)
        clock = iter([start, start+timedelta(minutes=2)])
        run_store(self.db, "650111", "中央食堂",feed,lambda:next(clock))
        self.assertEqual(self.db.execute("SELECT count(*) AS n FROM menu_snapshots WHERE menu_date=%s",
                                         (start.date(),)).fetchone()["n"],0)

    def test_recheck_keeps_verified_dishes_when_details_fail(self):
        morning = datetime(2026,11,24,8,0,tzinfo=JST)
        feed = Feed()
        feed.bad.clear()
        run = lambda at: run_store(self.db, "650111", "中央食堂", feed, lambda:at)
        codes = lambda: [r["dish_code"] for r in self.db.execute("""
            SELECT dish_code FROM menu_items WHERE snapshot_id=(SELECT max(id) FROM menu_snapshots)
            ORDER BY dish_code""").fetchall()]
        run(morning)
        self.assertEqual(codes(), ["one", "two"])
        # At 10:30 the detail of "two" times out: the morning's row stays published.
        feed.bad = {"two"}
        review = morning + timedelta(hours=2, minutes=30)
        run(review)
        self.assertEqual(codes(), ["one", "two"])
        job = self.db.execute("SELECT next_retry_at, pending_details FROM menu_import_jobs WHERE phase='review'").fetchone()
        self.assertEqual([e["dish_code"] for e in job["pending_details"]], ["two"])
        # The retry replaces only "two"; a full listing that drops a dish removes it.
        feed.bad.clear()
        run(job["next_retry_at"])
        self.assertEqual(codes(), ["one", "two"])
        self.assertIsNone(self.db.execute("SELECT next_retry_at FROM menu_import_jobs WHERE phase='review'").fetchone()["next_retry_at"])
        self.assertEqual(merge([{"dish_code": "gone"}, {"dish_code": "two"}], [{"dish_code": "one"}],
                               [{"dish_code": "two"}], True, {"one", "two"}),
                         [{"dish_code": "two"}, {"dish_code": "one"}])

    def test_every_detail_failing_reads_the_listing_again(self):
        morning = datetime(2026,11,24,8,0,tzinfo=JST)
        feed = Feed()
        feed.bad = {"one", "two"}
        run = lambda at: run_store(self.db, "650111", "中央食堂", feed, lambda:at)
        run(morning)
        self.assertEqual(self.db.execute("SELECT count(*) AS n FROM menu_snapshots").fetchone()["n"], 0)
        job = self.db.execute("SELECT next_retry_at, pending_details FROM menu_import_jobs").fetchone()
        self.assertIsNone(job["pending_details"])
        feed.bad.clear()
        run(job["next_retry_at"])
        self.assertEqual(feed.list_calls, 2)
        self.assertEqual(self.db.execute("SELECT count(*) AS n FROM menu_items").fetchone()["n"], 2)
