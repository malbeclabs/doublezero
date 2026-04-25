"""Fixture-based compatibility tests."""

import json
from pathlib import Path

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from telemetry.state import (
    DeviceLatencySamples,
    InternetLatencySamples,
    TimestampIndex,
    TimestampIndexEntry,
    reconstruct_timestamp,
    reconstruct_timestamps,
)

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
                "AgentVersion": d.agent_version,
                "AgentCommit": d.agent_commit,
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


class TestFixtureTimestampIndex:
    def test_deserialize(self):
        data, meta = _load_fixture("timestamp_index")
        d = TimestampIndex.from_bytes(data)
        got = {
            "AccountType": d.account_type,
            "SamplesAccountPK": d.samples_account_pk,
            "NextEntryIndex": d.next_entry_index,
            "EntriesCount": len(d.entries),
        }
        if len(d.entries) > 0:
            got["Entry0SampleIndex"] = d.entries[0].sample_index
            got["Entry0Timestamp"] = d.entries[0].timestamp_microseconds
        if len(d.entries) > 1:
            got["Entry1SampleIndex"] = d.entries[1].sample_index
            got["Entry1Timestamp"] = d.entries[1].timestamp_microseconds
        if len(d.entries) > 2:
            got["Entry2SampleIndex"] = d.entries[2].sample_index
            got["Entry2Timestamp"] = d.entries[2].timestamp_microseconds
        _assert_fields(meta["fields"], got)


class TestReconstructTimestamp:
    def test_with_entries(self):
        interval = 5_000_000
        entries = [
            TimestampIndexEntry(0, 1_700_000_000_000_000),
            TimestampIndexEntry(12, 1_700_000_000_120_000),
            TimestampIndexEntry(24, 1_700_000_000_240_000),
        ]
        assert reconstruct_timestamp(entries, 0, 0, interval) == 1_700_000_000_000_000
        assert reconstruct_timestamp(entries, 5, 0, interval) == 1_700_000_000_000_000 + 5 * interval
        assert reconstruct_timestamp(entries, 12, 0, interval) == 1_700_000_000_120_000
        assert reconstruct_timestamp(entries, 15, 0, interval) == 1_700_000_000_120_000 + 3 * interval
        assert reconstruct_timestamp(entries, 30, 0, interval) == 1_700_000_000_240_000 + 6 * interval

    def test_fallback_no_entries(self):
        ts = reconstruct_timestamp([], 10, 1_700_000_000_000_000, 5_000_000)
        assert ts == 1_700_000_000_000_000 + 10 * 5_000_000

    def test_late_start(self):
        start_ts = 1_700_000_000_000_000
        interval = 5_000_000
        entries = [
            TimestampIndexEntry(120, 1_700_000_000_800_000),
            TimestampIndexEntry(240, 1_700_000_001_600_000),
        ]
        # Before first entry: implicit model
        assert reconstruct_timestamp(entries, 0, start_ts, interval) == start_ts
        assert reconstruct_timestamp(entries, 50, start_ts, interval) == start_ts + 50 * interval
        assert reconstruct_timestamp(entries, 119, start_ts, interval) == start_ts + 119 * interval
        # At and after first entry
        assert reconstruct_timestamp(entries, 120, start_ts, interval) == 1_700_000_000_800_000
        assert reconstruct_timestamp(entries, 125, start_ts, interval) == 1_700_000_000_800_000 + 5 * interval
        assert reconstruct_timestamp(entries, 240, start_ts, interval) == 1_700_000_001_600_000

    def test_reconstruct_all(self):
        entries = [
            TimestampIndexEntry(0, 1000),
            TimestampIndexEntry(3, 5000),
        ]
        ts = reconstruct_timestamps(5, entries, 0, 100)
        assert ts == [1000, 1100, 1200, 5000, 5100]

    def test_reconstruct_all_late_start(self):
        entries = [
            TimestampIndexEntry(3, 5000),
        ]
        ts = reconstruct_timestamps(5, entries, 1000, 100)
        assert ts == [1000, 1100, 1200, 5000, 5100]
