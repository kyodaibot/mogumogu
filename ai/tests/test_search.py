import json

import httpx
import pytest

from mogumogu_ai.app import make_search
from mogumogu_ai.search import FakeSearch, SearchError, TavilySearch

pytestmark = pytest.mark.anyio


def tavily(handler) -> TavilySearch:
    return TavilySearch("test-key", transport=httpx.MockTransport(handler))


async def test_tavily_request_and_results():
    sent = []

    def handler(request: httpx.Request):
        sent.append(request)
        return httpx.Response(200, json={"query": "q", "results": [
            {"title": f"t{i}", "url": f"https://example.com/{i}", "content": f"c{i}", "score": 0.9}
            for i in range(7)
        ]})

    results = await tavily(handler).search("京都大学 北部食堂 メニュー")

    (req,) = sent
    assert str(req.url) == "https://api.tavily.com/search"
    assert req.headers["Authorization"] == "Bearer test-key"
    assert json.loads(req.content) == {
        "query": "京都大学 北部食堂 メニュー", "search_depth": "basic", "max_results": 5,
        "include_answer": False, "include_raw_content": False,
    }
    assert [r.url for r in results] == [f"https://example.com/{i}" for i in range(5)]
    assert (results[0].title, results[0].content) == ("t0", "c0")


@pytest.mark.parametrize("status", [401, 429, 432, 433, 500])
async def test_tavily_error_status_is_a_search_error(status):
    with pytest.raises(SearchError) as e:
        await tavily(lambda r: httpx.Response(status)).search("q")
    assert e.value.reason == str(status)


async def test_tavily_network_errors_are_search_errors():
    def timeout(request):
        raise httpx.ReadTimeout("slow", request=request)

    def refused(request):
        raise httpx.ConnectError("refused", request=request)

    with pytest.raises(SearchError) as e:
        await tavily(timeout).search("q")
    assert e.value.reason == "timeout"
    with pytest.raises(SearchError) as e:
        await tavily(refused).search("q")
    assert e.value.reason == "ConnectError"
    with pytest.raises(SearchError) as e:
        await tavily(lambda r: httpx.Response(200, json={"oops": 1})).search("q")
    assert e.value.reason == "bad_response"


def test_make_search(monkeypatch):
    monkeypatch.delenv("SEARCH_PROVIDER", raising=False)
    assert make_search() is None  # off unless set
    monkeypatch.setenv("SEARCH_PROVIDER", "fake")
    assert isinstance(make_search(), FakeSearch)
    monkeypatch.setenv("SEARCH_PROVIDER", "tavily")
    monkeypatch.setenv("TAVILY_API_KEY", "")
    assert make_search() is None  # no key: off, the service still starts
    monkeypatch.setenv("TAVILY_API_KEY", "test-key")
    assert isinstance(make_search(), TavilySearch)
    monkeypatch.setenv("SEARCH_PROVIDER", "brave")
    with pytest.raises(RuntimeError):
        make_search()
