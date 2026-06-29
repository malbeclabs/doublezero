"""Unit tests for the async RPC client and retry transport."""

import httpx2  # type: ignore[import-untyped]
import pytest
from solana.rpc.async_api import AsyncClient  # type: ignore[import-untyped]

from revdist import rpc
from revdist.rpc import _RetryTransport, new_rpc_client


class _StubTransport(httpx2.AsyncBaseTransport):
    """Returns the given status codes in order, then 200 forever."""

    def __init__(self, statuses: list[int]) -> None:
        self._statuses = list(statuses)
        self.calls = 0

    async def handle_async_request(
        self, request: httpx2.Request
    ) -> httpx2.Response:
        self.calls += 1
        code = self._statuses.pop(0) if self._statuses else 200
        return httpx2.Response(code, content=b"{}", request=request)


@pytest.fixture(autouse=True)
def _no_sleep(monkeypatch: pytest.MonkeyPatch) -> list[float]:
    """Capture backoff delays instead of actually sleeping."""
    slept: list[float] = []

    async def fake_sleep(seconds: float) -> None:
        slept.append(seconds)

    monkeypatch.setattr(rpc.asyncio, "sleep", fake_sleep)
    return slept


async def _send(transport: _RetryTransport) -> httpx2.Response:
    async with httpx2.AsyncClient(transport=transport) as client:
        return await client.post("http://rpc.test/", content="{}")


async def test_retries_on_429_then_succeeds(_no_sleep: list[float]) -> None:
    stub = _StubTransport([429, 429, 200])
    resp = await _send(_RetryTransport(wrapped=stub, max_retries=5))
    assert resp.status_code == 200
    assert stub.calls == 3  # two 429s + one success
    assert _no_sleep == [2, 4]  # backoff (attempt+1)*2 for the two retries


async def test_gives_up_after_max_retries(_no_sleep: list[float]) -> None:
    stub = _StubTransport([429] * 10)
    resp = await _send(_RetryTransport(wrapped=stub, max_retries=5))
    assert resp.status_code == 429
    assert stub.calls == 6  # initial attempt + 5 retries
    assert _no_sleep == [2, 4, 6, 8, 10]


async def test_no_retry_on_non_429(_no_sleep: list[float]) -> None:
    stub = _StubTransport([500])
    resp = await _send(_RetryTransport(wrapped=stub, max_retries=5))
    assert resp.status_code == 500
    assert stub.calls == 1
    assert _no_sleep == []


def test_new_rpc_client_installs_retry_session() -> None:
    client = new_rpc_client("http://rpc.test/")
    assert isinstance(client, AsyncClient)
    # Guard the private-attribute swap: the provider session must be our
    # retry-enabled httpx2 client, so an upstream rename fails loudly here.
    session = client._provider.session
    assert isinstance(session, httpx2.AsyncClient)
    assert isinstance(session._transport, _RetryTransport)
