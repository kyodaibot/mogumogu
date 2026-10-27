"""Internal HTTP API, called only by the Go backend."""

import asyncio
import logging
import os
import time
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from .agent import run_agent
from .demo import demo_model
from .model import ChatModel
from .schemas import ReplyRequest, SubmitReply
from .tools import GoClient

log = logging.getLogger("mogumogu_ai")
log.setLevel(logging.INFO)

# Stops work (and model spending) shortly after Go's own 30 s timeout.
REQUEST_TIMEOUT_S = 30


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


@asynccontextmanager
async def lifespan(app: FastAPI):
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    if not hasattr(app.state, "model"):
        app.state.model = make_model()
    log.info("model %s", type(app.state.model).__name__)
    base_url = os.environ.get("GO_INTERNAL_URL", "http://127.0.0.1:8081")
    async with httpx.AsyncClient(base_url=base_url, timeout=10) as http:
        app.state.http = http
        yield


app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)


@app.get("/healthz")
async def healthz():
    return {"status": "ok"}


@app.post("/internal/reply", response_model=SubmitReply)
async def reply(req: ReplyRequest, request: Request):
    started = time.monotonic()
    source = GoClient(request.app.state.http, req.token)
    try:
        async with asyncio.timeout(REQUEST_TIMEOUT_S):
            outcome = await run_agent(request.app.state.model, source, req)
    except TimeoutError:
        _log("timeout", started)
        return JSONResponse({"error": "timeout"}, status_code=503)
    except Exception as e:
        # Only the exception type: messages may contain request data.
        _log("error", started, exc=type(e).__name__)
        return JSONResponse({"error": "upstream_error"}, status_code=503)

    if outcome.result is None:
        _log(outcome.error, started, steps=outcome.steps, retried=outcome.retried,
             input_tokens=outcome.input_tokens, output_tokens=outcome.output_tokens)
        return JSONResponse({"error": outcome.error}, status_code=503)

    _log("ok", started, steps=outcome.steps, retried=outcome.retried,
         input_tokens=outcome.input_tokens, output_tokens=outcome.output_tokens)
    return outcome.result


def _log(result: str, started: float, **fields) -> None:
    """One line per message. Never logs message text, context or model output."""
    fields = {"result": result, "ms": round((time.monotonic() - started) * 1000), **fields}
    log.info("reply %s", " ".join(f"{k}={v}" for k, v in fields.items()))
