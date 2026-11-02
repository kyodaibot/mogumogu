"""Web search for the web_search tool (plans/web-search.md).

Only the query the model wrote leaves the service: never the profile,
allergens, memories or records. SEARCH_PROVIDER picks the provider.
"""

from dataclasses import dataclass
from typing import Protocol

import httpx

TAVILY_URL = "https://api.tavily.com"
# TimeBudget usually allows less; this bounds a search on its own.
TAVILY_TIMEOUT_S = 8
MAX_RESULTS = 5


@dataclass(frozen=True)
class SearchResult:
    title: str
    url: str
    # A short extract of the page, chosen by the search provider.
    content: str


class SearchError(Exception):
    """The search could not be done now. reason is safe to log: an HTTP
    status code or an error class, never the query."""

    def __init__(self, reason: str):
        super().__init__(reason)
        self.reason = reason


class SearchProvider(Protocol):
    async def search(self, query: str) -> list[SearchResult]:
        """At most MAX_RESULTS results; raises SearchError."""
        ...

    async def aclose(self) -> None: ...


class TavilySearch:
    """POST /search of Tavily. Free plan without a card: when the monthly
    credits run out it answers 432, which is a SearchError like any other."""

    def __init__(self, api_key: str, transport: httpx.AsyncBaseTransport | None = None):
        self.http = httpx.AsyncClient(
            base_url=TAVILY_URL, timeout=TAVILY_TIMEOUT_S, transport=transport,
            headers={"Authorization": f"Bearer {api_key}"},
        )

    async def search(self, query: str) -> list[SearchResult]:
        try:
            resp = await self.http.post("/search", json={
                "query": query,
                "search_depth": "basic",  # 1 credit; advanced costs 2
                "max_results": MAX_RESULTS,
                # Tavily's own generated answer is text no one has checked.
                "include_answer": False,
                "include_raw_content": False,
            })
        except httpx.TimeoutException:
            raise SearchError("timeout") from None
        except httpx.HTTPError as e:
            raise SearchError(type(e).__name__) from None
        if resp.status_code != 200:
            raise SearchError(str(resp.status_code))
        try:
            items = resp.json()["results"]
            return [
                SearchResult(str(r.get("title") or ""), str(r["url"]), str(r.get("content") or ""))
                for r in items[:MAX_RESULTS]
            ]
        except (ValueError, KeyError, TypeError):
            raise SearchError("bad_response") from None

    async def aclose(self) -> None:
        await self.http.aclose()


class FakeSearch:
    """Returns fixed results and records every query; never goes online.

    Used by tests and SEARCH_PROVIDER=fake.
    """

    def __init__(self, results: list[SearchResult] | None = None, error: str | None = None):
        self.results = results if results is not None else [
            SearchResult("北部食堂 メニュー", "https://example.com/north-menu", "本日の定食：鶏の照り焼き定食 520円。"),
            SearchResult("北部食堂 営業時間", "https://example.com/north-hours", "平日 11:00〜14:00。"),
        ]
        self.error = error
        self.queries: list[str] = []

    async def search(self, query: str) -> list[SearchResult]:
        self.queries.append(query)
        if self.error:
            raise SearchError(self.error)
        return self.results[:MAX_RESULTS]

    async def aclose(self) -> None:
        pass
