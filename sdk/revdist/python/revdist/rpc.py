"""Async RPC client helpers with retry on rate limiting."""

import asyncio

import httpx2  # type: ignore[import-untyped]
from solana.rpc.async_api import AsyncClient  # type: ignore[import-untyped]
from solana.rpc.async_http_provider import (  # type: ignore[import-untyped]
    AsyncHTTPProvider,
)

_DEFAULT_MAX_RETRIES = 5


class _RetryTransport(httpx2.AsyncBaseTransport):
    """Async HTTP transport that retries on 429 Too Many Requests."""

    def __init__(
        self,
        wrapped: httpx2.AsyncBaseTransport | None = None,
        max_retries: int = _DEFAULT_MAX_RETRIES,
    ) -> None:
        self._wrapped = wrapped or httpx2.AsyncHTTPTransport()
        self._max_retries = max_retries

    async def handle_async_request(
        self, request: httpx2.Request
    ) -> httpx2.Response:
        for attempt in range(self._max_retries + 1):
            response = await self._wrapped.handle_async_request(request)
            if response.status_code != 429 or attempt >= self._max_retries:
                return response
            await response.aclose()
            await asyncio.sleep((attempt + 1) * 2)
        return response  # unreachable, but satisfies type checker


def new_rpc_client(
    url: str,
    timeout: float = 30,
    max_retries: int = _DEFAULT_MAX_RETRIES,
) -> AsyncClient:
    """Create an async Solana RPC client that retries on 429 responses."""
    client = AsyncClient(url, timeout=timeout)
    # Replace the underlying httpx2 session with one using retry transport.
    transport = _RetryTransport(
        wrapped=httpx2.AsyncHTTPTransport(),
        max_retries=max_retries,
    )
    provider: AsyncHTTPProvider = client._provider
    provider.session = httpx2.AsyncClient(
        timeout=timeout,
        transport=transport,
    )
    return client
