"""The model used when the service runs with MODEL_PROVIDER=fake (CI, E2E).

Unlike FakeModel, it serves any number of requests: each call answers the
user's message straight away with submit_reply, using fixed text. A question
with a meal gets three recommendations; one without asks which meal. Questions
are recognised in Japanese and English; the answer is in context.locale, as
the real prompt asks. Any other message after a turn with recommendations is
taken as a follow-up: three new ones for the same meal, naming the first of
the earlier ones, so a test can see the history arrived.

"I ate" stages a record of ramen for the target date, and "delete" stages
deleting the first record of that date, when the context allows proposals;
otherwise the reply says it is a Pro feature.

"Remember X" saves X (the last clause before it, or the words after it) as a
memory, in the same response as the reply, and "forget" forgets every memory,
when the context allows memories; otherwise the reply says it is a Pro
feature. A message that also records a meal stages both.
"""

import json
import re

from langchain_core.messages import HumanMessage, ToolMessage

from .model import reply, tool_call

_ASK = ("何を食べ", "what should i eat")
_ATE = ("食べた", "i ate", "i had")
_DELETE = ("消して", "削除", "delete")
_REMEMBER = ("って覚えて", "を覚えて", "覚えて", "remember that ", "remember ")
_FORGET = ("忘れて", "forget")
_MEALS = {
    "朝食": "breakfast", "昼食": "lunch", "夕食": "dinner",
    "breakfast": "breakfast", "lunch": "lunch", "dinner": "dinner",
}

# The same made-up estimates in both languages; they pass the reply's checks.
_ESTIMATES = [
    {"serving": {"calories": 650, "protein_g": 35, "fat_g": 18, "carbs_g": 85},
     "per_100g": {"calories": 163, "protein_g": 9, "fat_g": 4, "carbs_g": 21}},
    {"serving": {"calories": 450, "protein_g": 15, "fat_g": 8, "carbs_g": 78},
     "per_100g": {"calories": 150, "protein_g": 5, "fat_g": 3, "carbs_g": 26}},
    {"serving": {"calories": 300, "protein_g": 22, "fat_g": 12, "carbs_g": 26},
     "per_100g": {"calories": 60, "protein_g": 4, "fat_g": 2, "carbs_g": 5}},
]

_TEXT = {
    "ja": {
        "recommend": "（テスト回答）3つの案です：",
        "ask_meal": "（テスト回答）朝食・昼食・夕食のどれですか？",
        "off_topic": "（テスト回答）食事に関する質問にだけ答えられます。",
        "follow_up": "（テスト回答）前回の「{}」などを踏まえて、3つの案です：",
        "staged": "（テスト回答）変更を用意しました。カードで確認してください。",
        "pro_only": "（テスト回答）チャットでの記録は Pro の機能です。カレンダーから追加できます。",
        "no_record": "（テスト回答）その日の記録はありません。",
        "remembered": "（テスト回答）覚えておきます。",
        "forgot": "（テスト回答）忘れました。",
        "memory_pro_only": "（テスト回答）好みを覚えるのは Pro の機能です。",
        "ramen": "ラーメン",
        "recommendations": [
            {"name": "照り焼きチキン丼", "description": "鶏もも肉とご飯、ブロッコリー", "reason": "（テストデータ）たんぱく質と炭水化物が十分"},
            {"name": "鮭おにぎりセット", "description": "おにぎり2個と味噌汁", "reason": "（テストデータ）コンビニで買えて時間を節約"},
            {"name": "豆腐と野菜の鍋", "description": "豆腐、白菜、きのこ", "reason": "（テストデータ）あっさりしていて夜向き"},
        ],
    },
    "en": {
        "recommend": "(Test reply) Here are three ideas:",
        "ask_meal": "(Test reply) Breakfast, lunch or dinner?",
        "off_topic": "(Test reply) I can only answer questions about food.",
        "follow_up": "(Test reply) Building on \"{}\" and the others, here are three ideas:",
        "staged": "(Test reply) The changes are ready. Check them on the card.",
        "pro_only": "(Test reply) Recording meals in the chat is a Pro feature. You can add them on the calendar.",
        "no_record": "(Test reply) There is no record on that day.",
        "remembered": "(Test reply) I'll remember that.",
        "forgot": "(Test reply) Forgotten.",
        "memory_pro_only": "(Test reply) Remembering preferences is a Pro feature.",
        "ramen": "Ramen",
        "recommendations": [
            {"name": "Teriyaki chicken bowl", "description": "Chicken thigh with rice and broccoli", "reason": "(Test data) Plenty of protein and carbs"},
            {"name": "Salmon onigiri set", "description": "Two rice balls and miso soup", "reason": "(Test data) Quick to buy at a convenience store"},
            {"name": "Tofu and vegetable hot pot", "description": "Tofu, napa cabbage and mushrooms", "reason": "(Test data) Light, good for the evening"},
        ],
    },
}


def _block(first: str, tag: str):
    if f"<{tag}>\n" not in first:
        return None
    return json.loads(first.split(f"<{tag}>\n", 1)[1].split(f"\n</{tag}>", 1)[0])


def _locale(first: str) -> str:
    return _block(first, "context").get("locale", "en")


def _stage(context: dict, message: str, t: dict, staged: bool):
    """The tool call for a message that asks to record or delete, or None."""
    # 何を食べたらいい also contains 食べた.
    ate = any(w in message for w in _ATE) and not any(a in message for a in _ASK)
    delete = any(w in message for w in _DELETE)
    if not (ate or delete):
        return None
    if not context.get("allow_proposal"):
        return tool_call("submit_reply", reply=t["pro_only"])
    if staged:
        return tool_call("submit_reply", reply=t["staged"])
    day = context["target_date"]
    if ate:
        return tool_call(
            "create_meal", date=day, meal_type=_meal(message) or "lunch", content=t["ramen"],
            nutrition={"calories": 500, "protein_g": 20, "fat_g": 15, "carbs_g": 70},
        )
    meal = next((m for m in context["meals"] if m["date"] == day), None)
    if meal is None:
        return tool_call("submit_reply", reply=t["no_record"])
    return tool_call("delete_meal", meal_id=meal["id"])


def _memory(context: dict, message: str, t: dict, answered: bool) -> list[dict] | None:
    """The tool calls for a message that asks to remember or forget, or None."""
    lowered = message.lower()
    remember = next((w for w in _REMEMBER if w in lowered), None)
    forget = any(w in lowered for w in _FORGET)
    if not (remember or forget):
        return None
    if not context.get("allow_memory"):
        return [tool_call("submit_reply", reply=t["memory_pro_only"])]
    done = tool_call("submit_reply", reply=t["remembered" if remember else "forgot"])
    if answered:
        return [done]
    if remember:
        start = lowered.index(remember)
        before = re.split(r"[、,。.]", message[:start])[-1]
        content = (before + message[start + len(remember):]).strip(" 。.、,!！")
        return [tool_call("remember", content=content, quote=content), done]
    return [*(tool_call("forget", memory_id=m["id"], quote=message) for m in context["memories"]), done]


def _meal(text: str) -> str | None:
    lowered = text.lower()
    return next((v for k, v in _MEALS.items() if k in lowered), None)


class DemoModel:
    async def ainvoke(self, messages, tools, force_tool=None):
        first = next(m.content for m in messages if isinstance(m, HumanMessage))
        message = first.split("<message>\n")[-1].split("\n</message>")[0]
        lowered = message.lower()
        t = _TEXT[_locale(first)]
        answered = any(isinstance(m, ToolMessage) for m in messages)
        memory = _memory(_block(first, "context"), message, t, answered)
        staging = _stage(_block(first, "context"), lowered, t, answered)
        if memory and staging:
            # Both: the memory with the meal change, then the meal's reply.
            return reply(*memory[:-1], staging)
        if memory:
            return reply(*memory)
        if staging:
            return reply(staging)
        meal_type = _meal(lowered)
        recs = [{**r, "estimate": e} for r, e in zip(t["recommendations"], _ESTIMATES)]
        earlier = [turn for turn in _block(first, "history") or [] if turn["recommendations"]]
        if any(a in lowered for a in _ASK) and meal_type:
            args = {"reply": t["recommend"], "recommendations": recs, "meal_type": meal_type}
        elif earlier and not any(a in lowered for a in _ASK):
            last = earlier[-1]
            args = {
                "reply": t["follow_up"].format(last["recommendations"][0]["name"]),
                "recommendations": recs,
                "meal_type": _meal(last["user"]) or "lunch",
            }
        elif any(a in lowered for a in _ASK):
            args = {"reply": t["ask_meal"], "ask_meal_type": True}
        else:
            args = {"reply": t["off_topic"]}
        return reply(tool_call("submit_reply", **args))


def demo_model() -> DemoModel:
    return DemoModel()
