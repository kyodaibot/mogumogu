"""The meal assistant's agent loop (project-review §4, docs/diagram/agent-loop.html).

agent → route → tools → agent …
              → validate → END, or once → force_finish → validate
              → force_finish → validate
"""

import asyncio
from dataclasses import dataclass
from typing import Annotated, TypedDict

from langchain_core.messages import AIMessage, AnyMessage, HumanMessage, SystemMessage, ToolMessage
from langgraph.errors import GraphRecursionError
from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages
from pydantic import ValidationError

from .model import ChatModel
from .prompt import SYSTEM_PROMPT, first_message
from .schemas import ReplyRequest, SubmitReply
from .tools import ALL_TOOLS, QUERY_TOOLS, SUBMIT_REPLY, DataSource, QueryBudget, run_query

# Query tools are allowed while fewer than this many model calls were made.
MAX_AGENT_STEPS = 4
# Only a safety net for counting bugs; the normal worst case is 11 node runs.
RECURSION_LIMIT = 20

_QUERY_NAMES = {t.name for t in QUERY_TOOLS}


class State(TypedDict):
    messages: Annotated[list[AnyMessage], add_messages]
    steps: int  # model calls so far
    retried: bool  # validate has sent a reply back once
    result: SubmitReply | None
    error: str | None


@dataclass
class Outcome:
    result: SubmitReply | None
    # None on success; "invalid_output" or "recursion_limit" otherwise.
    error: str | None
    steps: int
    retried: bool
    input_tokens: int
    output_tokens: int


def check_reply(args: dict, allergens: list[str]) -> SubmitReply | str:
    """Returns the parsed reply, or the reason it was rejected."""
    try:
        parsed = SubmitReply.model_validate(args)
    except ValidationError as e:
        return "; ".join(f"{'.'.join(map(str, err['loc'])) or 'arguments'}: {err['msg']}" for err in e.errors()[:3])
    # Only the obvious case: the allergen's name appears in what is shown on the
    # card. An allergen written in another language than the reply (the user
    # typed "えび", the card says "shrimp") is not caught here; the prompt's
    # allergen rules are the guard for that.
    for rec in parsed.recommendations:
        text = f"{rec.name} {rec.description}".casefold()
        for allergen in allergens:
            if allergen.strip() and allergen.strip().casefold() in text:
                return f"Recommendation \"{rec.name}\" contains the user's allergen \"{allergen}\"; replace it with an option without it"
    return parsed


def _answer_pending(last: AIMessage, text: str) -> list[AnyMessage]:
    """Every tool call needs a result before the model is called again."""
    if not last.tool_calls:
        return [HumanMessage(text)]
    return [ToolMessage(text, tool_call_id=call["id"]) for call in last.tool_calls]


def build_graph(model: ChatModel, source: DataSource, allergens: list[str]):
    budget = QueryBudget()

    async def call_model(messages, force_tool=None) -> AIMessage:
        return await model.ainvoke([SystemMessage(SYSTEM_PROMPT), *messages], ALL_TOOLS, force_tool)

    async def agent(state: State):
        resp = await call_model(state["messages"])
        return {"messages": [resp], "steps": state["steps"] + 1}

    def route(state: State) -> str:
        names = [call["name"] for call in state["messages"][-1].tool_calls]
        if SUBMIT_REPLY.name in names:
            return "validate"
        if names and set(names) <= _QUERY_NAMES and state["steps"] < MAX_AGENT_STEPS:
            return "tools"
        return "force_finish"

    async def tools(state: State):
        calls = state["messages"][-1].tool_calls
        results = await asyncio.gather(*(run_query(source, c["name"], c["args"], budget) for c in calls))
        return {"messages": [ToolMessage(r, tool_call_id=c["id"]) for c, r in zip(calls, results)]}

    async def force_finish(state: State):
        new: list[AnyMessage] = []
        last = state["messages"][-1]
        # Coming straight from agent: its queries (if any) are not run.
        if isinstance(last, AIMessage):
            new = _answer_pending(last, "No more queries. Call submit_reply with what you already have.")
        resp = await call_model([*state["messages"], *new], force_tool=SUBMIT_REPLY.name)
        return {"messages": [*new, resp], "steps": state["steps"] + 1}

    def validate(state: State):
        last = state["messages"][-1]
        submit = next((c for c in last.tool_calls if c["name"] == SUBMIT_REPLY.name), None)
        checked = check_reply(submit["args"], allergens) if submit else "submit_reply was not called"
        if isinstance(checked, SubmitReply):
            return {"result": checked.rounded()}
        if state["retried"]:
            return {"error": "invalid_output"}
        feedback = f"The reply failed validation: {checked}. Fix it and call submit_reply again."
        if submit:
            # Queries sent alongside submit_reply are ignored, not run.
            msgs = [
                ToolMessage(feedback if c is submit else "Ignored.", tool_call_id=c["id"])
                for c in last.tool_calls
            ]
        else:
            msgs = _answer_pending(last, feedback)
        return {"messages": msgs, "retried": True}

    def after_validate(state: State) -> str:
        done = state["result"] is not None or state["error"] is not None
        return END if done else "force_finish"

    g = StateGraph(State)
    g.add_node("agent", agent)
    g.add_node("tools", tools)
    g.add_node("force_finish", force_finish)
    g.add_node("validate", validate)
    g.add_edge(START, "agent")
    g.add_conditional_edges("agent", route, ["tools", "validate", "force_finish"])
    g.add_edge("tools", "agent")
    g.add_edge("force_finish", "validate")
    g.add_conditional_edges("validate", after_validate, [END, "force_finish"])
    return g.compile()


async def run_agent(model: ChatModel, source: DataSource, req: ReplyRequest) -> Outcome:
    """Answers one message. Model and data-source errors propagate to the caller."""
    profile = req.context.profile
    graph = build_graph(model, source, (profile.allergens or []) if profile else [])
    init: State = {
        "messages": [HumanMessage(first_message(req.context, req.message))],
        "steps": 0,
        "retried": False,
        "result": None,
        "error": None,
    }
    try:
        final = await graph.ainvoke(init, {"recursion_limit": RECURSION_LIMIT})
    except GraphRecursionError:
        return Outcome(None, "recursion_limit", -1, False, 0, 0)

    usage = [m.usage_metadata for m in final["messages"] if isinstance(m, AIMessage) and m.usage_metadata]
    return Outcome(
        result=final["result"],
        error=final["error"],
        steps=final["steps"],
        retried=final["retried"],
        input_tokens=sum(u["input_tokens"] for u in usage),
        output_tokens=sum(u["output_tokens"] for u in usage),
    )
