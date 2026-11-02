"""The real model: Claude on Amazon Bedrock through the Converse API.

Credentials come from the standard AWS chain (aws login / SSO locally, the
EC2 instance role in the cloud); nothing here reads a key.
"""

from collections.abc import Callable, Sequence

from botocore.config import Config
from botocore.exceptions import ClientError, ConnectionClosedError, EndpointConnectionError
from langchain_aws import ChatBedrockConverse
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.utils.function_calling import convert_to_openai_tool

from .model import ToolSpec

# Enough for a reply and three recommendations with their estimates, with
# room to spare.
MAX_OUTPUT_TOKENS = 1536

# Worth one more try; anything else is raised at once.
_RETRYABLE_CODES = {"ThrottlingException", "ServiceUnavailableException", "InternalServerException"}


def _retryable(e: Exception) -> bool:
    if isinstance(e, ClientError):
        return e.response.get("Error", {}).get("Code") in _RETRYABLE_CODES
    return isinstance(e, (EndpointConnectionError, ConnectionClosedError))


def _tool(spec: ToolSpec) -> dict:
    tool = convert_to_openai_tool(spec.args)
    tool["function"]["name"] = spec.name
    tool["function"]["description"] = spec.description
    return tool


class BedrockModel:
    def __init__(self, model_id: str, region: str):
        self.llm = ChatBedrockConverse(
            model=model_id,
            region_name=region,
            max_tokens=MAX_OUTPUT_TOKENS,
            # The call runs in a worker thread (langchain-aws has no async
            # client), which keeps going after the agent stops waiting for it.
            # So boto never retries on its own, and read_timeout bounds how
            # long such a thread can live. The agent's TimeBudget sets the
            # real, usually shorter, limit; retries are done in ainvoke.
            # total_max_attempts counts the first request; max_attempts would
            # count only the retries.
            config=Config(connect_timeout=3, read_timeout=15, retries={"mode": "standard", "total_max_attempts": 1}),
        )

    async def ainvoke(
        self, messages: Sequence[BaseMessage], tools: Sequence[ToolSpec], force_tool: str | None = None,
        may_retry: Callable[[], bool] = lambda: False,
    ) -> AIMessage:
        # "any": the model must call some tool, so it never answers in plain text.
        bound = self.llm.bind_tools([_tool(t) for t in tools], tool_choice=force_tool or "any")
        try:
            return await bound.ainvoke(list(messages))
        except Exception as e:
            if not (_retryable(e) and may_retry()):
                raise
        return await bound.ainvoke(list(messages))
