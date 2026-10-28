"""A few end-to-end checks against the real model (`make ai-eval`).

Each case runs the full agent with Bedrock and fixed test data (no Go, no
database), then checks what can be checked mechanically. The reply texts are
printed so a person can judge the rest. Costs a few cents per run.
"""

import asyncio
import os
import sys
import time
from datetime import date, timedelta
from collections.abc import Callable
from dataclasses import dataclass

from mogumogu_ai.agent import Outcome, run_agent
from mogumogu_ai.app import DEFAULT_MODEL_ID
from mogumogu_ai.bedrock import BedrockModel
from mogumogu_ai.schemas import ReplyRequest


class Source:
    """Fixed records for the query tools; remembers the ranges asked for."""

    def __init__(self, meals: list[dict]):
        self.meals = meals
        self.calls: list[tuple] = []

    async def fetch(self, kind, start, end):
        self.calls.append((kind, start, end))
        if kind == "events":
            return []
        return [m for m in self.meals if start.isoformat() <= m["date"] <= end.isoformat()]


def request(message: str, *, locale="ja", target="2026-10-27", allergens=None, meals=None, events=None) -> ReplyRequest:
    return ReplyRequest.model_validate({
        "message": message,
        "token": "eval",
        "context": {
            "target_date": target,
            "now": "2026-10-27T11:30:00+09:00",
            "timezone": "Asia/Tokyo",
            "locale": locale,
            "profile": {
                "height_cm": 170, "weight_kg": 62, "age": 21, "has_exercise_habit": True,
                "allergens": allergens, "goal": "build_muscle",
            },
            "events": events or [],
            "events_range": {"from": "2026-10-26", "to": "2026-10-28"},
            "meals": meals or [],
            "meals_range": {"from": "2026-10-21", "to": "2026-10-27"},
        },
    })


def meal(date: str, meal_type: str, content: str) -> dict:
    return {"date": date, "meal_type": meal_type, "content": content,
            "calories": None, "protein_g": None, "fat_g": None, "carbs_g": None}


# Two months of dinners, fish every fifth day, for the long-range question.
HISTORY = [meal(f"2026-{m:02d}-{d:02d}", "dinner", "焼き魚" if d % 5 == 0 else "チキンライス")
           for m in (8, 9, 10) for d in range(1, 29)]


@dataclass
class Case:
    name: str
    req: ReplyRequest
    source: Source
    # Returns a problem description, or None when the case passes.
    check: Callable[[Outcome, Source], str | None]


def _has_kana(text: str) -> bool:
    return any("\u3040" <= c <= "\u30ff" for c in text)


def in_language(o: Outcome, locale: str) -> str | None:
    """Japanese text has kana; English text has none (food names aside, it is
    checked on the reply and the recommendation reasons)."""
    texts = [o.result.reply, *(r.reason for r in o.result.recommendations)]
    wrong = [t for t in texts if _has_kana(t) != (locale == "ja")]
    return f"回复语言不是 {locale}：{wrong[0][:40]}" if wrong else None


def needs_three(o: Outcome, _: Source) -> str | None:
    return None if len(o.result.recommendations) == 3 else "应有 3 个推荐"


def text_only(o: Outcome, _: Source) -> str | None:
    return "不应有推荐" if o.result.recommendations else None


def no_allergens(o: Outcome, s: Source) -> str | None:
    if problem := needs_three(o, s):
        return problem
    shown = " ".join(f"{r.name} {r.description}" for r in o.result.recommendations)
    hits = [a for a in ("えび", "エビ", "海老", "shrimp", "prawn", "ピーナッツ", "落花生", "peanut") if a in shown.lower()]
    return f"推荐中出现过敏原 {hits}" if hits else None


def within_31_days(o: Outcome, s: Source) -> str | None:
    if problem := text_only(o, s):
        return problem
    days = {(start + timedelta(days=i)) for _, start, end in s.calls for i in range((end - start).days + 1)}
    if len(days) > 31:
        return f"累计查询了 {len(days)} 天"
    # "Now" is 2026-10-27, so the last 31 days start on 2026-09-27.
    if days and min(days) < date(2026, 9, 27):
        return f"查询了最近 31 天以前的记录（从 {min(days)} 起）"
    return None if "31" in o.result.reply else "回复应说明只看最近 31 天"


def asks_meal(o: Outcome, s: Source) -> str | None:
    if not o.result.ask_meal_type:
        return "ask_meal_type 应为 true"
    return text_only(o, s)


CASES = [
    Case("有过敏原", request("今日の昼食は何を食べたらいい？", allergens=["えび", "ピーナッツ"]), Source([]), no_allergens),
    Case("没有任何记录", request("今日は何を食べたらいい？"), Source([]), needs_three),
    Case("超过 31 天的记录", request("最近2か月で魚を何回食べた？", meals=HISTORY[-7:]), Source(HISTORY), within_31_days),
    Case("与饮食无关", request("秋についての詩を書いて"), Source([]), text_only),
    Case("不是今天且没说餐次", request("その日は何を食べたらいい？", target="2026-10-29"), Source([]), asks_meal),
    # English interface. The allergens were typed in Japanese, so only the
    # prompt's rules keep shrimp and peanuts out (the code check cannot match).
    Case("英语 + 日语写的过敏原", request("What should I eat for lunch today?", locale="en", allergens=["えび", "ピーナッツ"]),
         Source([]), no_allergens),
    Case("英语界面、日语提问", request("今日の夕食は何を食べたらいい？", locale="en"), Source([]), needs_three),
]


async def main() -> int:
    model = BedrockModel(os.environ.get("BEDROCK_MODEL_ID", DEFAULT_MODEL_ID), os.environ.get("AWS_REGION", "ap-northeast-1"))
    failed = 0
    for case in CASES:
        started = time.monotonic()
        outcome = await run_agent(model, case.source, case.req)
        ms = round((time.monotonic() - started) * 1000)
        problem = outcome.error or case.check(outcome, case.source) or in_language(outcome, case.req.context.locale)
        failed += problem is not None
        print(f"{'PASS' if problem is None else 'FAIL'}  {case.name}  {ms} ms, {outcome.steps} 步, "
              f"输入 {outcome.input_tokens} / 输出 {outcome.output_tokens} token"
              + (f"  ← {problem}" if problem else ""))
        if outcome.result:
            print(f"      回复：{outcome.result.reply}")
            for r in outcome.result.recommendations:
                print(f"      · {r.name}：{r.description}（{r.reason}）")
        if case.source.calls:
            print("      查询：" + ", ".join(f"{k} {a}~{b}" for k, a, b in case.source.calls))
    print(f"\n{len(CASES) - failed}/{len(CASES)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
