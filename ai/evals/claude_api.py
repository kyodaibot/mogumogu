"""Direct Claude Messages API adapter for local evaluations."""
import os

from anthropic import APIConnectionError, APIStatusError
from langchain_anthropic import ChatAnthropic
from langchain_core.messages import SystemMessage

DEFAULT_ASSISTANT = "claude-haiku-4-5-20251001"


class ClaudeModel:
    def __init__(self):
        self.llm = ChatAnthropic(model=os.environ.get("EVAL_ASSISTANT_MODEL") or DEFAULT_ASSISTANT,
                                 max_tokens=1536, timeout=15, max_retries=0)

    async def ainvoke(self, messages, tools, force_tool=None, may_retry=lambda: False):
        schemas = [{"name": t.name, "description": t.description, "input_schema": t.args.model_json_schema()}
                   for t in tools]
        bound = self.llm.bind_tools(schemas, tool_choice=force_tool or "any")
        messages = list(messages)
        # The shared agent appends a Bedrock cachePoint after the static text.
        # On the Messages API the cache directive belongs to the text block.
        if messages and isinstance(messages[0], SystemMessage):
            blocks = messages[0].content
            if isinstance(blocks, list) and blocks[-1:] == [{"cachePoint": {"type": "default"}}]:
                content = [*blocks[:-2], {**blocks[-2], "cache_control": {"type": "ephemeral"}}]
                messages[0] = messages[0].model_copy(update={"content": content})
        try:
            return await bound.ainvoke(messages)
        except (APIConnectionError, APIStatusError) as error:
            if isinstance(error, APIStatusError) and error.status_code not in (429, 500, 502, 503, 504, 529):
                raise
            if not may_retry():
                raise
        return await bound.ainvoke(messages)
