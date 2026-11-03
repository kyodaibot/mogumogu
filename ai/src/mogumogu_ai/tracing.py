"""Langfuse traces of each message (plans/langfuse.md, ADR 0002).

A trace holds the full content: context, every model input and output, tool
arguments and results. It goes to Langfuse's Japan region and is kept 30
days. The JSON log line (app.py) still never holds content.

On only when LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY are set. The SDK
reads LANGFUSE_BASE_URL and LANGFUSE_RELEASE itself, and sends in the
background: when Langfuse is down or over its quota, traces are dropped and
replies are not affected.
"""

import os
from typing import TYPE_CHECKING

from .schemas import ReplyRequest

if TYPE_CHECKING:
    from langfuse import Langfuse

TRACE_NAME = "reply"


def make_langfuse() -> "Langfuse | None":
    if not (os.environ.get("LANGFUSE_PUBLIC_KEY") and os.environ.get("LANGFUSE_SECRET_KEY")):
        return None
    # Imported here so tests and CI never start the exporter.
    from langfuse import Langfuse

    # "prod" on the server, "eval" for make ai-eval; anything else is local.
    return Langfuse(environment=os.environ.get("LANGFUSE_TRACING_ENVIRONMENT") or "local")


def trace_config(req: ReplyRequest, name: str = TRACE_NAME, tags: list[str] | None = None) -> dict:
    """The graph config that sends one message's run to Langfuse as one trace."""
    from langfuse.langchain import CallbackHandler

    tags = [*(tags or []), req.context.locale]
    if req.tier:
        tags.append(req.tier)
    metadata: dict = {"langfuse_trace_name": name, "langfuse_tags": tags}
    if req.user_id is not None:
        metadata["langfuse_user_id"] = str(req.user_id)
    return {"callbacks": [CallbackHandler()], "run_name": name, "metadata": metadata}
