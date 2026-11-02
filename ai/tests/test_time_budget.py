import pytest

from mogumogu_ai.time_budget import TimeBudget


class FakeClock:
    def __init__(self):
        self.now = 1000.0

    def __call__(self) -> float:
        return self.now


def budget_at(seconds: float) -> TimeBudget:
    clock = FakeClock()
    time = TimeBudget(clock)
    clock.now += seconds
    return time


@pytest.mark.parametrize(("at", "kind", "expected"), [
    (0, "model", 15),
    (16, "model", 4),       # started, then cut at the soft limit
    (17.5, "model", None),  # under 3 s to the soft limit: straight to force_finish
    (19, "model", None),
    (20, "model", None),
    (0, "go", 5),
    (18, "go", 2),
    (19.2, "go", None),     # under 1 s: not run, the model is told time is up
    (20, "go", None),
    (13, "search", 7),
    (20, "search", None),
    (0, "final", 15),
    (20, "final", 10),      # the forced submit right at the soft limit
    (22, "final", 8),
    (22.5, "final", None),  # under 8 s to the hard limit
    (26, "final", None),    # failed validation at 26 s: no retry
    (30, "final", None),
])
def test_timeout_for(at, kind, expected):
    assert budget_at(at).timeout_for(kind) == expected


def test_nothing_after_the_hard_limit():
    time = budget_at(30)
    assert [time.timeout_for(k) for k in ("model", "final", "go", "search")] == [None] * 4


def test_nothing_once_stopped():
    time = budget_at(0)
    time.stop()
    assert [time.timeout_for(k) for k in ("model", "final", "go", "search")] == [None] * 4
