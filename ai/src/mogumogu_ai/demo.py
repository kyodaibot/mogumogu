"""The model used when the service runs with MODEL_PROVIDER=fake (CI, E2E).

Unlike FakeModel, it serves any number of requests: each call answers the
user's message straight away with submit_reply, using fixed text. A question
with a meal gets three recommendations; one without asks which meal. Questions
are recognised in Japanese and English; the answer is in context.locale, as
the real prompt asks.
"""

import json

from langchain_core.messages import HumanMessage

from .model import reply, tool_call

_ASK = ("何を食べ", "what should i eat")
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
        "recommendations": [
            {"name": "Teriyaki chicken bowl", "description": "Chicken thigh with rice and broccoli", "reason": "(Test data) Plenty of protein and carbs"},
            {"name": "Salmon onigiri set", "description": "Two rice balls and miso soup", "reason": "(Test data) Quick to buy at a convenience store"},
            {"name": "Tofu and vegetable hot pot", "description": "Tofu, napa cabbage and mushrooms", "reason": "(Test data) Light, good for the evening"},
        ],
    },
}


def _locale(first: str) -> str:
    context = json.loads(first.split("<context>\n", 1)[1].split("\n</context>", 1)[0])
    return context.get("locale", "en")


class DemoModel:
    async def ainvoke(self, messages, tools, force_tool=None):
        first = next(m.content for m in messages if isinstance(m, HumanMessage))
        lowered = first.split("<message>")[-1].lower()
        t = _TEXT[_locale(first)]
        meal_type = next((v for k, v in _MEALS.items() if k in lowered), None)
        if any(a in lowered for a in _ASK) and meal_type:
            recs = [{**r, "estimate": e} for r, e in zip(t["recommendations"], _ESTIMATES)]
            args = {"reply": t["recommend"], "recommendations": recs, "meal_type": meal_type}
        elif any(a in lowered for a in _ASK):
            args = {"reply": t["ask_meal"], "ask_meal_type": True}
        else:
            args = {"reply": t["off_topic"]}
        return reply(tool_call("submit_reply", **args))


def demo_model() -> DemoModel:
    return DemoModel()
