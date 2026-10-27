"""The interface the agent uses to call a model, and a scripted fake for tests.

The Bedrock implementation is added in milestone 3. MODEL_PROVIDER picks one.
"""

from collections.abc import Callable, Sequence
from dataclasses import dataclass
from itertools import count
from typing import Protocol

from langchain_core.messages import AIMessage, BaseMessage
from pydantic import BaseModel


@dataclass(frozen=True)
class ToolSpec:
    name: str
    description: str
    args: type[BaseModel]


class ChatModel(Protocol):
    async def ainvoke(
        self,
        messages: Sequence[BaseMessage],
        tools: Sequence[ToolSpec],
        force_tool: str | None = None,
    ) -> AIMessage:
        """Call the model once.

        The model must answer with tool calls. force_tool, when set, names the
        only tool it may call this time.
        """
        ...


# A script step receives the conversation so far and returns the reply.
Step = AIMessage | Callable[[Sequence[BaseMessage]], AIMessage]

_ids = count(1)


def tool_call(name: str, **args) -> dict:
    return {"name": name, "args": args, "id": f"call_{next(_ids)}", "type": "tool_call"}


def reply(*calls: dict, input_tokens: int = 100, output_tokens: int = 20) -> AIMessage:
    """An AIMessage that makes the given tool calls, with made-up token usage."""
    return AIMessage(
        content="",
        tool_calls=list(calls),
        usage_metadata={
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
            "total_tokens": input_tokens + output_tokens,
        },
    )


class FakeModel:
    """Returns scripted replies in order and records every call.

    Used by tests, CI and E2E (MODEL_PROVIDER=fake); never calls a real model.
    """

    def __init__(self, script: Sequence[Step]):
        self.script = list(script)
        self.calls: list[dict] = []

    async def ainvoke(self, messages, tools, force_tool=None) -> AIMessage:
        self.calls.append({"messages": list(messages), "force_tool": force_tool})
        if len(self.calls) > len(self.script):
            raise AssertionError(f"FakeModel called {len(self.calls)} times; script has {len(self.script)} steps")
        step = self.script[len(self.calls) - 1]
        return step(messages) if callable(step) else step
