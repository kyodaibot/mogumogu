"""The meal assistant's agent loop (project-review §4, docs/diagram/agent-loop.html).

agent → route → tools → agent …
              → memorize → validate, or → agent
              → validate → END, or once → force_finish → validate
              → force_finish → validate

How long each call may take is decided by TimeBudget (time_budget.py).
"""

import asyncio
import logging
from dataclasses import dataclass, field
from typing import Annotated, TypedDict

from langchain_core.messages import AIMessage, AnyMessage, HumanMessage, SystemMessage, ToolMessage
from langgraph.errors import GraphRecursionError
from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages
from pydantic import ValidationError

from .model import ChatModel
from .prompt import SYSTEM_PROMPT, first_message
from .schemas import ReplyRequest, SubmitReply
from .search import SearchError, SearchProvider
from .time_budget import TimeBudget
from .tools import (
    MEMORY_TOOLS, QUERY_TOOLS, STAGING_TOOLS, SUBMIT_REPLY, WEB_SEARCH,
    DataSource, MemoryChanges, QueryBudget, SearchBudget, Staging, run_query, run_search,
    search_unavailable, tools_for,
)

log = logging.getLogger("mogumogu_ai")

# Tools are allowed while fewer than this many model calls were made. With the
# forced submit and the validation retry, a message makes at most 6 calls.
MAX_TOOL_MODEL_CALLS = 4
# Only a fuse against counting bugs; the normal worst case is 11 node runs.
RECURSION_LIMIT = 20

_TIME_UP = "Time is up; this was not run. Call submit_reply with what you already have."

_QUERY_NAMES = {t.name for t in QUERY_TOOLS}
_STAGING_NAMES = {t.name for t in STAGING_TOOLS}
_MEMORY_NAMES = {t.name for t in MEMORY_TOOLS}
# Run in parallel, like the query tools.
_FETCH_NAMES = _QUERY_NAMES | {WEB_SEARCH.name}


class State(TypedDict):
    messages: Annotated[list[AnyMessage], add_messages]
    # The soft limit stopped a model call that could use tools.
    out_of_time: bool
    retried: bool  # validate has sent a reply back once
    # How the last submit_reply came about; see Outcome.finish.
    finish: str | None
    result: SubmitReply | None
    error: str | None


@dataclass
class Counts:
    model_calls: int = 0  # started, including timed-out ones
    model_timeouts: int = 0
    model_retries: int = 0  # after throttling or a dropped connection
    go_timeouts: int = 0
    searches: int = 0  # sent to the search provider, failed ones included
    search_errors: int = 0  # timeouts included


@dataclass
class Outcome:
    result: SubmitReply | None
    # None on success; otherwise "invalid_output", "timeout", "disconnected"
    # or "recursion_limit".
    error: str | None
    retried: bool = False
    # "submitted" (the model's own submit_reply), "max_calls", "soft_deadline"
    # or "no_tool_call" (forced), "deadline_invalid" (failed validation with
    # no time to retry); None when no reply was written.
    finish: str | None = None
    counts: Counts = field(default_factory=Counts)
    # input_tokens counts every input token, cached ones included.
    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_tokens: int = 0
    cache_write_tokens: int = 0
    # Changes staged for a proposal (tools.Staging); empty without any.
    changes: list[dict] = field(default_factory=list)
    # Changes to memories (tools.MemoryChanges); empty without any.
    memory_changes: list[dict] = field(default_factory=list)
    # The reply's sources with their titles: [{"title", "url"}].
    sources: list[dict] = field(default_factory=list)


def _cache_written(usage: dict) -> int:
    # langchain-aws reports writes under cache_creation, or per TTL when
    # Bedrock breaks them down (cache_creation is then 0).
    details = usage.get("input_token_details", {})
    return sum(details.get(k, 0) for k in ("cache_creation", "ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens"))


def check_reply(args: dict, allergens: list[str], found: dict[str, str] | None = None) -> SubmitReply | str:
    """Returns the parsed reply, or the reason it was rejected. found holds
    the URLs web_search returned in this message; a source must be one."""
    try:
        parsed = SubmitReply.model_validate(args)
    except ValidationError as e:
        return "; ".join(f"{'.'.join(map(str, err['loc'])) or 'arguments'}: {err['msg']}" for err in e.errors()[:3])
    unknown = [u for u in parsed.sources if u not in (found or {})]
    if unknown:
        return (
            f"sources: {unknown[0]} is not the url of a web_search result in this message; "
            "copy urls exactly from the results, or leave sources empty"
        )
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


# The tool definitions and this prompt are the same for every request of a tier,
# so Bedrock caches everything up to the cache point (5 minutes, renewed on each
# hit). Anything that differs per request must stay after it, in the messages.
# The cached prefix must be at least 4,096 tokens on Haiku 4.5, or nothing is
# cached (no error).
SYSTEM = SystemMessage([{"type": "text", "text": SYSTEM_PROMPT}, {"cachePoint": {"type": "default"}}])


def build_graph(
    model: ChatModel, source: DataSource, allergens: list[str], time: TimeBudget, counts: Counts,
    staging: Staging | None = None, memories: MemoryChanges | None = None,
    search: SearchProvider | None = None, searches: SearchBudget | None = None,
):
    """staging is None when Go does not allow proposals, memories when it
    does not allow memories, search when searching is not allowed or not set
    up; their tools are then not offered."""
    budget = QueryBudget()
    searches = searches or SearchBudget()
    tools_offered = tools_for(staging is not None, memories is not None, search is not None)
    runnable = (
        _QUERY_NAMES | (_STAGING_NAMES if staging else set()) | (_MEMORY_NAMES if memories else set())
        | ({WEB_SEARCH.name} if search else set())
    )
    # What memorize answered each memory call; validate repeats it.
    memory_answers: dict[str, str] = {}

    async def call_model(messages, kind: str, force_tool=None) -> AIMessage | None:
        """None when the call was not started or timed out."""
        timeout = time.timeout_for(kind)
        if timeout is None:
            return None

        def may_retry() -> bool:
            if time.timeout_for(kind) is None:
                return False
            counts.model_retries += 1
            return True

        counts.model_calls += 1
        try:
            async with asyncio.timeout(timeout):
                return await model.ainvoke([SYSTEM, *messages], tools_offered, force_tool, may_retry)
        except TimeoutError:
            counts.model_timeouts += 1
            return None

    async def agent(state: State):
        resp = await call_model(state["messages"], "model")
        if resp is None:
            return {"out_of_time": True}
        return {"messages": [resp]}

    def route(state: State) -> str:
        if state["out_of_time"]:
            return "force_finish"
        names = {call["name"] for call in state["messages"][-1].tool_calls}
        can_run = counts.model_calls < MAX_TOOL_MODEL_CALLS
        # Memories saved next to submit_reply need no second model call when
        # they all pass: the reply already says what it saves.
        others = names - {SUBMIT_REPLY.name}
        if memories and SUBMIT_REPLY.name in names and others and others <= _MEMORY_NAMES:
            return "memorize"
        # Changes staged next to submit_reply (with a query or a meal change)
        # are staged first, so they are not lost; the model submits again
        # after seeing the results.
        if can_run and names & (_STAGING_NAMES | _MEMORY_NAMES) and names - {SUBMIT_REPLY.name} <= runnable:
            return "tools"
        if SUBMIT_REPLY.name in names:
            return "validate"
        if names and names <= runnable and can_run:
            return "tools"
        return "force_finish"

    async def query(call: dict) -> str:
        timeout = time.timeout_for("go")
        if timeout is None:
            return _TIME_UP
        try:
            async with asyncio.timeout(timeout):
                return await run_query(source, call["name"], call["args"], budget, seen_meals)
        except TimeoutError:
            counts.go_timeouts += 1
            return "This query timed out. Answer without it, and do not query it again."

    seen_meals = staging.seen_meals if staging else None

    async def web_search(call: dict) -> str:
        timeout = time.timeout_for("search")
        if timeout is None:
            return _TIME_UP
        try:
            async with asyncio.timeout(timeout):
                return await run_search(search, call["args"], searches)
        except (SearchError, TimeoutError) as e:
            counts.search_errors += 1
            # Only why it failed: the query may contain anything the user wrote.
            log.warning("web search failed: %s", e.reason if isinstance(e, SearchError) else "timeout")
            return search_unavailable(searches)

    async def fetch(call: dict) -> str:
        return await (web_search(call) if call["name"] == WEB_SEARCH.name else query(call))

    async def tools(state: State):
        calls = state["messages"][-1].tool_calls
        fetches = [c for c in calls if c["name"] in _FETCH_NAMES]
        if any(c["name"] == WEB_SEARCH.name for c in fetches):
            searches.rounds += 1
        answers = dict(zip((c["id"] for c in fetches), await asyncio.gather(*(fetch(c) for c in fetches))))
        # After the queries, which may show the ids of the records to change.
        for c in calls:
            if c["name"] in _STAGING_NAMES:
                answers[c["id"]] = staging.stage(c["name"], c["args"])
            elif c["name"] in _MEMORY_NAMES:
                answers[c["id"]] = memories.stage(c["name"], c["args"])[0]
            elif c["name"] == SUBMIT_REPLY.name:
                answers[c["id"]] = "Not submitted yet: read these results, then call submit_reply again."
        return {"messages": [ToolMessage(answers[c["id"]], tool_call_id=c["id"]) for c in calls]}

    def memorize(state: State):
        """Saves the memory calls made next to submit_reply. When one fails
        and a step is left, the model sees the results and submits again."""
        last = state["messages"][-1]
        ok = True
        for c in last.tool_calls:
            if c["name"] in _MEMORY_NAMES:
                memory_answers[c["id"]], passed = memories.stage(c["name"], c["args"])
                ok = ok and passed
        if ok or counts.model_calls >= MAX_TOOL_MODEL_CALLS:
            return {}
        not_yet = "Not submitted yet: read these results, then call submit_reply again."
        return {"messages": [
            ToolMessage(memory_answers.get(c["id"], not_yet), tool_call_id=c["id"]) for c in last.tool_calls
        ]}

    def after_memorize(state: State) -> str:
        return "validate" if isinstance(state["messages"][-1], AIMessage) else "agent"

    async def force_finish(state: State):
        # The validation retry keeps how the reply came about; otherwise this
        # is why the model must submit now.
        if state["retried"]:
            finish = state["finish"]
        elif state["out_of_time"]:
            finish = "soft_deadline"
        elif counts.model_calls >= MAX_TOOL_MODEL_CALLS:
            finish = "max_calls"
        else:
            finish = "no_tool_call"
        new: list[AnyMessage] = []
        last = state["messages"][-1]
        # Coming straight from agent: its queries (if any) are not run.
        if isinstance(last, AIMessage):
            new = _answer_pending(
                last, "No more tool calls; these were not run. Call submit_reply with what you already have."
            )
        resp = await call_model([*state["messages"], *new], "final", force_tool=SUBMIT_REPLY.name)
        if resp is None:
            if state["retried"]:
                return {"error": "invalid_output", "finish": "deadline_invalid"}
            return {"error": "timeout", "finish": finish}
        return {"messages": [*new, resp], "finish": finish}

    def validate(state: State):
        finish = state["finish"] or "submitted"
        last = state["messages"][-1]
        submit = next((c for c in last.tool_calls if c["name"] == SUBMIT_REPLY.name), None)
        checked = check_reply(submit["args"], allergens, searches.found) if submit else "submit_reply was not called"
        if isinstance(checked, SubmitReply):
            return {"result": checked.rounded(), "finish": finish}
        if state["retried"]:
            return {"error": "invalid_output", "finish": finish}
        if time.timeout_for("final") is None:
            return {"error": "invalid_output", "finish": "deadline_invalid"}
        feedback = f"The reply failed validation: {checked}. Fix it and call submit_reply again."
        if submit:
            # Queries sent alongside submit_reply are ignored, not run.
            msgs = [
                ToolMessage(feedback if c is submit else memory_answers.get(c["id"], "Ignored."), tool_call_id=c["id"])
                for c in last.tool_calls
            ]
        else:
            msgs = _answer_pending(last, feedback)
        return {"messages": msgs, "retried": True, "finish": finish}

    def after_force_finish(state: State) -> str:
        return END if state["error"] is not None else "validate"

    def after_validate(state: State) -> str:
        done = state["result"] is not None or state["error"] is not None
        return END if done else "force_finish"

    g = StateGraph(State)
    g.add_node("agent", agent)
    g.add_node("tools", tools)
    g.add_node("memorize", memorize)
    g.add_node("force_finish", force_finish)
    g.add_node("validate", validate)
    g.add_edge(START, "agent")
    g.add_conditional_edges("agent", route, ["tools", "memorize", "validate", "force_finish"])
    g.add_edge("tools", "agent")
    g.add_conditional_edges("memorize", after_memorize, ["validate", "agent"])
    g.add_conditional_edges("force_finish", after_force_finish, ["validate", END])
    g.add_conditional_edges("validate", after_validate, [END, "force_finish"])
    return g.compile()


async def run_agent(
    model: ChatModel, source: DataSource, req: ReplyRequest, time: TimeBudget | None = None,
    search: SearchProvider | None = None, trace: dict | None = None,
) -> Outcome:
    """Answers one message. Model and data-source errors propagate to the
    caller; search errors do not (the model answers without the results).
    search is offered only when the context allows it. trace is the
    tracing config (tracing.trace_config), merged into the graph's."""
    time = time or TimeBudget()
    counts = Counts()
    profile = req.context.profile
    allergens = (profile.allergens or []) if profile else []
    staging = Staging(req.context.meals, allergens) if req.context.allow_proposal else None
    memories = MemoryChanges(req.context.memories, req.message) if req.context.allow_memory else None
    searches = SearchBudget()
    search = search if req.context.allow_search else None
    graph = build_graph(model, source, allergens, time, counts, staging, memories, search, searches)
    init: State = {
        "messages": [HumanMessage(first_message(req.context, req.message, req.history))],
        "out_of_time": False,
        "retried": False,
        "finish": None,
        "result": None,
        "error": None,
    }
    try:
        final = await graph.ainvoke(init, {**(trace or {}), "recursion_limit": RECURSION_LIMIT})
    except GraphRecursionError:
        return Outcome(None, "recursion_limit", counts=counts)
    finally:
        counts.searches = searches.used

    error, finish = final["error"], final["finish"]
    if final["result"] is None and time.stopped:
        error, finish = "disconnected", None
    usage = [m.usage_metadata for m in final["messages"] if isinstance(m, AIMessage) and m.usage_metadata]
    return Outcome(
        result=final["result"],
        error=error,
        retried=final["retried"],
        finish=finish,
        counts=counts,
        input_tokens=sum(u["input_tokens"] for u in usage),
        output_tokens=sum(u["output_tokens"] for u in usage),
        cache_read_tokens=sum(u.get("input_token_details", {}).get("cache_read", 0) for u in usage),
        cache_write_tokens=sum(_cache_written(u) for u in usage),
        changes=staging.changes if staging and final["result"] else [],
        memory_changes=memories.changes if memories and final["result"] else [],
        sources=[{"title": searches.found[u], "url": u} for u in final["result"].sources] if final["result"] else [],
    )
