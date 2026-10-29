"""The real model: Claude on Amazon Bedrock through the Converse API.

Credentials come from the standard AWS chain (aws login / SSO locally, the
EC2 instance role in the cloud); nothing here reads a key.
"""

from collections.abc import Sequence

from botocore.config import Config
from langchain_aws import ChatBedrockConverse
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.utils.function_calling import convert_to_openai_tool

from .model import ToolSpec

# Enough for a reply and three recommendations with their estimates, with
# room to spare.
MAX_OUTPUT_TOKENS = 1536


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
            # One retry for throttling or a dropped connection; the whole
            # message is still bounded by the service's 30 s timeout.
            config=Config(connect_timeout=5, read_timeout=25, retries={"mode": "standard", "max_attempts": 2}),
        )

    async def ainvoke(
        self, messages: Sequence[BaseMessage], tools: Sequence[ToolSpec], force_tool: str | None = None
    ) -> AIMessage:
        # "any": the model must call some tool, so it never answers in plain text.
        bound = self.llm.bind_tools([_tool(t) for t in tools], tool_choice=force_tool or "any")
        return await bound.ainvoke(list(messages))
