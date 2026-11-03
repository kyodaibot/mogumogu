"""Internal HTTP API, called only by the Go backend."""

import asyncio
import json
import logging
import os
import time
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from .agent import Outcome, run_agent
from .demo import demo_model
from .model import ChatModel
from .schemas import Reply, ReplyRequest, Source
from .search import FakeSearch, SearchProvider, TavilySearch
from .time_budget import HARD_LIMIT_S, TimeBudget
from .tools import GoClient
from .tracing import make_langfuse, trace_config

log = logging.getLogger("mogumogu_ai")
log.setLevel(logging.INFO)

# One JSON object per line and nothing else, so CloudWatch Logs Insights reads
# its fields directly (docs/operations.md 6.6).
reply_log = logging.getLogger("mogumogu_ai.reply")
reply_log.setLevel(logging.INFO)
reply_log.propagate = False
_handler = logging.StreamHandler()
_handler.setFormatter(logging.Formatter("%(message)s"))
reply_log.addHandler(_handler)

# Only a fuse: TimeBudget starts nothing that could run past HARD_LIMIT_S.
# Go waits 35 s, so this answer still reaches it.
REQUEST_TIMEOUT_S = HARD_LIMIT_S + 1
# How often to check whether Go has dropped the request.
DISCONNECT_POLL_S = 0.5


DEFAULT_MODEL_ID = "jp.anthropic.claude-haiku-4-5-20251001-v1:0"


def make_model() -> ChatModel:
    # Empty counts as unset: compose passes optional variables as "".
    provider = os.environ.get("MODEL_PROVIDER") or "bedrock"
    if provider == "fake":
        return demo_model()
    if provider == "bedrock":
        # Imported here so tests and CI (fake) never load the AWS SDK.
        from .bedrock import BedrockModel

        return BedrockModel(
            model_id=os.environ.get("BEDROCK_MODEL_ID") or DEFAULT_MODEL_ID,
            region=os.environ.get("AWS_REGION") or "ap-northeast-1",
        )
    raise RuntimeError(f"unsupported MODEL_PROVIDER: {provider}")


def make_search() -> SearchProvider | None:
    """None: web_search is not offered, even to Pro users."""
    provider = os.environ.get("SEARCH_PROVIDER") or "off"
    if provider == "off":
        return None
    if provider == "fake":
        return FakeSearch()
    if provider == "tavily":
        key = os.environ.get("TAVILY_API_KEY")
        if not key:
            # A missing key turns search off rather than stopping the service.
            log.warning("SEARCH_PROVIDER=tavily but TAVILY_API_KEY is empty; web search is off")
            return None
        return TavilySearch(key)
    raise RuntimeError(f"unsupported SEARCH_PROVIDER: {provider}")


@asynccontextmanager
async def lifespan(app: FastAPI):
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    if not hasattr(app.state, "model"):
        app.state.model = make_model()
    if not hasattr(app.state, "search"):
        app.state.search = make_search()
    if not hasattr(app.state, "langfuse"):
        app.state.langfuse = make_langfuse()
    log.info(
        "model %s, search %s, tracing %s", type(app.state.model).__name__,
        type(app.state.search).__name__, "on" if app.state.langfuse else "off",
    )
    base_url = os.environ.get("GO_INTERNAL_URL", "http://127.0.0.1:8081")
    # TimeBudget usually allows less; this bounds a query on its own.
    async with httpx.AsyncClient(base_url=base_url, timeout=5) as http:
        app.state.http = http
        yield
    if app.state.search is not None:
        await app.state.search.aclose()
    if app.state.langfuse is not None:
        # Sends the traces still queued; bounded by LANGFUSE_TIMEOUT.
        app.state.langfuse.shutdown()


app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)


@app.get("/healthz")
async def healthz():
    return {"status": "ok"}


async def _watch_disconnect(request: Request, time_budget: TimeBudget) -> None:
    while not await request.is_disconnected():
        await asyncio.sleep(DISCONNECT_POLL_S)
    time_budget.stop()


@app.post("/internal/reply", response_model=Reply)
async def reply(req: ReplyRequest, request: Request):
    started = time.monotonic()
    source = GoClient(request.app.state.http, req.token)
    time_budget = TimeBudget()
    watcher = asyncio.create_task(_watch_disconnect(request, time_budget))
    trace = trace_config(req) if request.app.state.langfuse else None
    try:
        async with asyncio.timeout(REQUEST_TIMEOUT_S):
            outcome = await run_agent(
                request.app.state.model, source, req, time_budget, request.app.state.search, trace,
            )
    except TimeoutError:
        _log("timeout", started)
        return JSONResponse({"error": "timeout"}, status_code=503)
    except Exception as e:
        # Only the exception type: messages may contain request data.
        _log("error", started, exc=type(e).__name__)
        return JSONResponse({"error": "upstream_error"}, status_code=503)
    finally:
        watcher.cancel()

    if outcome.result is None:
        _log(outcome.error, started, **_fields(outcome))
        return JSONResponse({"error": outcome.error}, status_code=503)

    _log(
        "ok", started, **_fields(outcome), changes=len(outcome.changes),
        memory_changes=len(outcome.memory_changes), sources=len(outcome.sources),
    )
    return Reply.model_construct(
        **{**dict(outcome.result), "sources": [Source(**src) for src in outcome.sources]},
        changes=outcome.changes, memory_changes=outcome.memory_changes,
    )


def _fields(o: Outcome) -> dict:
    return {
        "finish": o.finish, "model_calls": o.counts.model_calls, "retried": o.retried,
        "model_timeouts": o.counts.model_timeouts, "model_retries": o.counts.model_retries,
        "go_timeouts": o.counts.go_timeouts,
        "searches": o.counts.searches, "search_errors": o.counts.search_errors,
        "input_tokens": o.input_tokens, "output_tokens": o.output_tokens,
        "cache_read_tokens": o.cache_read_tokens, "cache_write_tokens": o.cache_write_tokens,
    }


def _log(result: str, started: float, **fields) -> None:
    """One line per message. Never logs message text, context or model output."""
    fields = {"msg": "reply", "result": result, "ms": round((time.monotonic() - started) * 1000), **fields}
    reply_log.info(json.dumps(fields))
