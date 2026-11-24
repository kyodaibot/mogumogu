"""Evaluation-only uvicorn entry point: record actual Go menu responses."""
import json
import logging
import os
from contextlib import asynccontextmanager
from pathlib import Path

from mogumogu_ai.app import app, lifespan, reply_log
from claude_api import ClaudeModel


async def record_menu(response):
    if response.request.url.path == "/internal/agent/menu":
        await response.aread()
        entry = {"query": dict(response.request.url.params), "status": response.status_code,
                 "result": response.json()}
        with Path(os.environ["EVAL_MENU_LOG"]).open("a") as stream:
            stream.write(json.dumps(entry, ensure_ascii=False) + "\n")


class EvalLog(logging.Formatter):
    """One JSON object per line: the reply log as is, and why a reply failed validation."""

    def format(self, record):
        message = record.getMessage()
        return message if record.name == reply_log.name else json.dumps({"validation": message}, ensure_ascii=False)


@asynccontextmanager
async def capture_lifespan(application):
    application.state.model = ClaudeModel()
    handler = logging.FileHandler(os.environ["EVAL_AI_LOG"])
    handler.setFormatter(EvalLog())
    handler.addFilter(lambda r: r.name == reply_log.name or r.getMessage().startswith("validation failed"))
    agent_log = logging.getLogger("mogumogu_ai")
    reply_log.addHandler(handler)
    agent_log.addHandler(handler)
    try:
        async with lifespan(application):
            application.state.http.event_hooks["response"].append(record_menu)
            yield
    finally:
        reply_log.removeHandler(handler)
        agent_log.removeHandler(handler)
        handler.close()


app.router.lifespan_context = capture_lifespan
