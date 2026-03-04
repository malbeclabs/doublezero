"""Fixture-based compatibility tests."""

import json
from pathlib import Path

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from telemetry.state import DeviceLatencySamples, InternetLatencySamples

FIXTURES_DIR = Path(__file__).resolve().parent.parent.parent.parent / "testdata" / "fixtures"


def _load_fixture(name: str) -> tuple[bytes, dict]:
    bin_data = (FIXTURES_DIR / f"{name}.bin").read_bytes()
    meta = json.loads((FIXTURES_DIR / f"{name}.json").read_text())
    return bin_data, meta


def _assert_fields(expected_fields: list[dict], got: dict) -> None:
    for f in expected_fields:
        name = f["name"]
        if name not in got:
            continue
        typ = f["typ"]
        raw = f["value"]
        actual = got[name]
        if typ in ("u8", "u16", "u32", "u64"):
            assert actual == int(raw), f"{name}: expected {raw}, got {actual}"
        elif typ == "pubkey":
            expected = Pubkey.from_string(raw)
            assert actual == expected, f"{name}: expected {expected}, got {actual}"
        elif typ == "string":
            assert actual == raw, f"{name}: expected {raw}, got {actual}"


class TestFixtureDeviceLatencySamples:
    def test_deserialize(self):
        data, meta = _load_fixture("device_latency_samples")
        d = DeviceLatencySamples.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": d.account_type,
                "Epoch": d.epoch,
                "OriginDeviceAgentPK": d.origin_device_agent_pk,
                "OriginDevicePK": d.origin_device_pk,
                "TargetDevicePK": d.target_device_pk,
                "OriginDeviceLocationPK": d.origin_device_location_pk,
                "TargetDeviceLocationPK": d.target_device_location_pk,
                "LinkPK": d.link_pk,
                "SamplingIntervalMicroseconds": d.sampling_interval_microseconds,
                "StartTimestampMicroseconds": d.start_timestamp_microseconds,
                "NextSampleIndex": d.next_sample_index,
                "SamplesCount": len(d.samples),
            },
        )


class TestFixtureInternetLatencySamples:
    def test_deserialize(self):
        data, meta = _load_fixture("internet_latency_samples")
        d = InternetLatencySamples.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": d.account_type,
                "Epoch": d.epoch,
                "DataProviderName": d.data_provider_name,
                "OracleAgentPK": d.oracle_agent_pk,
                "OriginExchangePK": d.origin_exchange_pk,
                "TargetExchangePK": d.target_exchange_pk,
                "SamplingIntervalMicroseconds": d.sampling_interval_microseconds,
                "StartTimestampMicroseconds": d.start_timestamp_microseconds,
                "NextSampleIndex": d.next_sample_index,
                "SamplesCount": len(d.samples),
            },
        )
