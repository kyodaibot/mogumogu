import pytest
from botocore.exceptions import ClientError, EndpointConnectionError
from langchain_core.messages import AIMessage, HumanMessage

from mogumogu_ai.bedrock import BedrockModel
from mogumogu_ai.tools import SUBMIT_REPLY

pytestmark = pytest.mark.anyio


def client_error(code: str) -> ClientError:
    return ClientError({"Error": {"Code": code, "Message": "x"}}, "Converse")


class FakeLLM:
    """Stands in for ChatBedrockConverse: fails with the given errors, then answers."""

    def __init__(self, *errors: Exception):
        self.errors = list(errors)
        self.calls = 0

    def bind_tools(self, tools, tool_choice):
        return self

    async def ainvoke(self, messages):
        self.calls += 1
        if self.errors:
            raise self.errors.pop(0)
        return AIMessage(content="ok")


def model_with(llm: FakeLLM) -> BedrockModel:
    model = BedrockModel("test-model", "ap-northeast-1")
    model.llm = llm
    return model


async def call(model: BedrockModel, may_retry):
    return await model.ainvoke([HumanMessage("hi")], [SUBMIT_REPLY], may_retry=may_retry)


async def test_boto_itself_never_retries():
    config = BedrockModel("test-model", "ap-northeast-1").llm.client.meta.config
    assert config.retries["total_max_attempts"] == 1
    assert (config.connect_timeout, config.read_timeout) == (3, 15)


@pytest.mark.parametrize("error", [client_error("ThrottlingException"), EndpointConnectionError(endpoint_url="x")])
async def test_retries_once_when_allowed(error):
    llm = FakeLLM(error)
    assert (await call(model_with(llm), lambda: True)).content == "ok"
    assert llm.calls == 2


async def test_no_retry_without_time():
    llm = FakeLLM(client_error("ThrottlingException"))
    with pytest.raises(ClientError):
        await call(model_with(llm), lambda: False)
    assert llm.calls == 1


async def test_only_one_retry():
    llm = FakeLLM(client_error("ThrottlingException"), client_error("ThrottlingException"))
    with pytest.raises(ClientError):
        await call(model_with(llm), lambda: True)
    assert llm.calls == 2


async def test_other_errors_are_not_retried():
    llm = FakeLLM(client_error("ValidationException"))
    asked = []
    with pytest.raises(ClientError):
        await call(model_with(llm), lambda: asked.append(1) or True)
    assert llm.calls == 1 and asked == []
