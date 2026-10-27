"""The model used when the service runs with MODEL_PROVIDER=fake (CI, E2E).

Unlike FakeModel, it serves any number of requests: each call answers the
user's message straight away with submit_reply, using fixed text. A question
with a meal gets three recommendations; one without asks which meal.
"""

from langchain_core.messages import HumanMessage

from .model import reply, tool_call

_RECOMMENDATIONS = [
    {"name": "照烧鸡腿饭", "description": "鸡腿肉配米饭和西兰花", "reason": "（测试数据）蛋白质和碳水都比较充足"},
    {"name": "三文鱼饭团套餐", "description": "两个饭团加味噌汤", "reason": "（测试数据）便利店就能买到，节省时间"},
    {"name": "豆腐蔬菜锅", "description": "豆腐、白菜和菌菇", "reason": "（测试数据）清淡，适合晚上"},
]


class DemoModel:
    async def ainvoke(self, messages, tools, force_tool=None):
        text = next(m.content for m in messages if isinstance(m, HumanMessage)).split("<message>")[-1]
        if "吃什么" in text and any(meal in text for meal in ("早餐", "午餐", "晚餐")):
            args = {"reply": "（测试回复）这里有三个方案：", "recommendations": _RECOMMENDATIONS}
        elif "吃什么" in text:
            args = {"reply": "（测试回复）想问早餐、午餐还是晚餐？", "ask_meal_type": True}
        else:
            args = {"reply": "（测试回复）我只能回答和饮食有关的问题。"}
        return reply(tool_call("submit_reply", **args))


def demo_model() -> DemoModel:
    return DemoModel()
