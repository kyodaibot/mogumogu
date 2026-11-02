"""The time rules for one message (project-review §4, docs/operations.md 6.11).

Every model call and tool call asks timeout_for(kind) how long it may take;
None means it is not started. No other code decides about time.

Until the soft limit, model calls and tools share the time; after it, only
the final reply (a forced submit_reply, or the retry after a failed
validation) may run, so the last seconds before the hard limit are kept for it.
"""

import time
from collections.abc import Callable

SOFT_LIMIT_S = 20
HARD_LIMIT_S = 30

# kind: (longest it may take, least time left for it to be worth starting,
# counted to the soft limit or, for "final", to the hard limit).
_RULES = {
    # A model call that may still use tools.
    "model": (15, 3),
    # A forced submit_reply or the validation retry: writing three
    # recommendations took about 5.5 s in the evals.
    "final": (15, 8),
    # Reading meals or events from Go.
    "go": (5, 1),
    # A web search (feat/web-search).
    "search": (8, 1),
}


class TimeBudget:
    def __init__(self, clock: Callable[[], float] = time.monotonic):
        self.clock = clock
        self.started = clock()
        self.stopped = False

    def stop(self) -> None:
        """The caller has gone (Go cancelled the request): start nothing more."""
        self.stopped = True

    def elapsed(self) -> float:
        return self.clock() - self.started

    def timeout_for(self, kind: str) -> float | None:
        """Seconds the next call of this kind may take, or None to not start it."""
        longest, least = _RULES[kind]
        limit = HARD_LIMIT_S if kind == "final" else SOFT_LIMIT_S
        left = limit - self.elapsed()
        if self.stopped or left < least:
            return None
        return min(longest, left)
