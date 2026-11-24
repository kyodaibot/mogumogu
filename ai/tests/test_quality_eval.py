from pathlib import Path

from evals import quality

ROOT = Path(__file__).resolve().parents[2]


def test_approved_suite_keeps_rubrics_separate_from_messages():
    cases = quality.load_cases(ROOT / "tests.md")
    assert len(cases) == 20
    assert sum(len(c["messages"]) for c in cases) == 25
    assert cases[15]["messages"][1] == "そこからご飯を外すと、残りはいくら？"
    assert all(c["rubric"] and c["rubric"] not in c["messages"] for c in cases)


def test_browser_history_uses_names_and_stores_for_followup():
    turn = {"date": "2026-11-24", "message": "二つお願い", "answer": {
        "reply": "どうぞ", "cafeteria_cards": [{"name": "魚", "stores": [{"name": "中央"}, {"name": "北部"}]}]}}
    assert quality.history_turn(turn)["recommendations"] == [{"name": "魚", "description": "中央 / 北部"}]


def test_assistant_failure_and_ungraded_cases_have_separate_counts():
    rows = [{"status": s} for s in ("pass", "fail", "assistant_error", "judge_error")]
    rows[0]["score"], rows[1]["score"] = 0.8, 0.0
    assert quality.summary(rows[:3])["mean_score"] == 0.8 / 3
    assert quality.summary(rows) == {"cases": 4, "passed": 1, "quality_failures": 1,
                                      "assistant_errors": 1, "judge_errors": 1, "pending": 0,
                                      "mean_score": None, "scored_mean": 0.4}


def test_report_shows_api_failure_next_to_scored_answers():
    rows = [
        {"id": "KYO-001", "title": "昼食", "turns": [{"message": "お昼は？", "ms": 1210, "error": "ai_unavailable",
          "ai_log": [{"validation": "validation failed: Group the breakfast"}, {"result": "invalid_output"}]}], "status": "assistant_error"},
        {"id": "KYO-002", "title": "夕食", "turns": [{"message": "夕食は？", "ms": 1000,
          "answer": {"reply": "中央で魚はいかがですか。"}}], "status": "pass", "score": 0.9, "reason": "Specific dish."},
    ]
    report = {"cases": rows, "started": "today", "assistant": "assistant", "judge": "judge", "threshold": 0.7}
    output = quality.markdown_report(report)
    for fragment in ("KYO-001", "invalid_output", "校验：validation failed: Group the breakfast", "KYO-002", "中央で魚", "Specific dish.", "1/2"):
        assert fragment in output


def test_compare_matches_ids_even_when_case_order_changes():
    old = {"cases": [{"id": "KYO-002", "status": "fail"}, {"id": "KYO-001", "status": "pass"}]}
    new = {"cases": [{"id": "KYO-001", "status": "assistant_error"}, {"id": "KYO-002", "status": "pass"}]}
    output = quality.comparison(old, new)
    assert "| KYO-001 | pass | assistant_error |" in output
    assert "| KYO-002 | fail | pass |" in output


def test_judge_receives_the_actual_profile_and_displayed_store_menu_date():
    turn = {"at": "2026-11-24T12:00:00+09:00", "date": "2026-11-24", "menu_queries": [], "answer": {
        "reply": "筋肉づくりに", "cafeteria_cards": [{"name": "魚", "reason": "たんぱく質",
            "menu_date": "2026-11-24", "stores": [{"name": "中央", "hours": [], "menu_date": "2026-11-23"}]}]}}
    card = quality.visible_answer(turn)["cafeteria_cards"][0]
    assert "menu_date" not in card
    assert card["stores"][0]["menu_date"] == "2026-11-23"
    assert card["stores"][0]["earlier_menu_notice"] is True
    assert "build_muscle" in quality.judge_context(turn, {"goal": "build_muscle"})

