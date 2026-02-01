"""PDA derivation tests."""

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from telemetry.pda import (
    derive_device_latency_samples_pda,
    derive_internet_latency_samples_pda,
)

PROGRAM_ID = Pubkey.from_string("tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC")


class TestDeriveDeviceLatencySamplesPDA:
    def test_deterministic(self):
        origin = Pubkey.from_string("11111111111111111111111111111112")
        target = Pubkey.from_string("11111111111111111111111111111113")
        link = Pubkey.from_string("11111111111111111111111111111114")

        addr1, bump1 = derive_device_latency_samples_pda(
            PROGRAM_ID, origin, target, link, 42
        )
        addr2, bump2 = derive_device_latency_samples_pda(
            PROGRAM_ID, origin, target, link, 42
        )
        assert addr1 == addr2
        assert bump1 == bump2


class TestDeriveInternetLatencySamplesPDA:
    def test_deterministic(self):
        oracle = Pubkey.from_string("11111111111111111111111111111112")
        origin = Pubkey.from_string("11111111111111111111111111111113")
        target = Pubkey.from_string("11111111111111111111111111111114")

        addr1, _ = derive_internet_latency_samples_pda(
            PROGRAM_ID, oracle, "RIPE Atlas", origin, target, 42
        )
        assert addr1 != Pubkey.default()
