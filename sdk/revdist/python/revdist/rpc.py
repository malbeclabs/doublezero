"""RPC client helpers with retry on rate limiting."""

import time

import httpx
from solana.rpc.api import Client as SolanaHTTPClient  # type: ignore[import-untyped]
from solana.rpc.providers.http import HTTPProvider  # type: ignore[import-untyped]

_DEFAULT_MAX_RETRIES = 5


class _RetryTransport(httpx.BaseTransport):
    """HTTP transport that retries on 429 Too Many Requests."""

    def __init__(
        self,
        wrapped: httpx.BaseTransport | None = None,
        max_retries: int = _DEFAULT_MAX_RETRIES,
    ) -> None:
        self._wrapped = wrapped or httpx.HTTPTransport()
        self._max_retries = max_retries

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        for attempt in range(self._max_retries + 1):
            response = self._wrapped.handle_request(request)
            if response.status_code != 429 or attempt >= self._max_retries:
                return response
            response.close()
            time.sleep((attempt + 1) * 2)
        return response  # unreachable, but satisfies type checker


def new_rpc_client(
    url: str,
    timeout: float = 30,
    max_retries: int = _DEFAULT_MAX_RETRIES,
) -> SolanaHTTPClient:
    """Create a Solana RPC client with automatic retry on 429 responses."""
    client = SolanaHTTPClient(url, timeout=timeout)
    # Replace the underlying httpx session with one using retry transport.
    transport = _RetryTransport(
        wrapped=httpx.HTTPTransport(),
        max_retries=max_retries,
    )
    client._provider.session = httpx.Client(
        timeout=timeout,
        transport=transport,
    )
    return client
