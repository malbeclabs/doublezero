"""SOL/2Z oracle client."""

from __future__ import annotations

from dataclasses import dataclass

import httpx


@dataclass
class SwapRate:
    rate: float
    timestamp: int
    signature: str
    sol_price_usd: str
    twoz_price_usd: str
    cache_hit: bool


class OracleClient:
    """Fetches SOL/2Z swap rates from the oracle API."""

    def __init__(self, base_url: str) -> None:
        self._base_url = base_url
        self._http = httpx.Client(timeout=30)

    def fetch_swap_rate(self) -> SwapRate:
        resp = self._http.get(f"{self._base_url}/swap-rate")
        resp.raise_for_status()
        data = resp.json()
        return SwapRate(
            rate=data["swapRate"],
            timestamp=data["timestamp"],
            signature=data["signature"],
            sol_price_usd=data["solPriceUsd"],
            twoz_price_usd=data["twozPriceUsd"],
            cache_hit=data["cacheHit"],
        )
