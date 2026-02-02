"""On-chain account data structures for the telemetry program.

Binary layout: 1-byte AccountType discriminator followed by Borsh-serialized
header fields, then raw u32 LE sample values (not a Borsh Vec — count is
determined by next_sample_index).
"""

from __future__ import annotations

from dataclasses import dataclass, field

from borsh_incremental import IncrementalReader
from solders.pubkey import Pubkey  # type: ignore[import-untyped]


TELEMETRY_SEED_PREFIX = b"telemetry"
DEVICE_LATENCY_SAMPLES_SEED = b"dzlatency"
INTERNET_LATENCY_SAMPLES_SEED = b"inetlatency"

MAX_DEVICE_LATENCY_SAMPLES_PER_ACCOUNT = 35_000
MAX_INTERNET_LATENCY_SAMPLES_PER_ACCOUNT = 3_000

DEVICE_LATENCY_HEADER_SIZE = 1 + 8 + 32 * 6 + 8 + 8 + 4 + 128


def _read_pubkey(r: IncrementalReader) -> Pubkey:
    return Pubkey.from_bytes(r.read_pubkey_raw())


@dataclass
class DeviceLatencySamples:
    account_type: int
    epoch: int
    origin_device_agent_pk: Pubkey
    origin_device_pk: Pubkey
    target_device_pk: Pubkey
    origin_device_location_pk: Pubkey
    target_device_location_pk: Pubkey
    link_pk: Pubkey
    sampling_interval_microseconds: int
    start_timestamp_microseconds: int
    next_sample_index: int
    samples: list[int] = field(default_factory=list)

    @classmethod
    def from_bytes(cls, data: bytes) -> DeviceLatencySamples:
        if len(data) < DEVICE_LATENCY_HEADER_SIZE:
            raise ValueError(
                f"data too short for device latency header: {len(data)} < {DEVICE_LATENCY_HEADER_SIZE}"
            )

        r = IncrementalReader(data)

        account_type = r.read_u8()
        epoch = r.read_u64()
        origin_device_agent_pk = _read_pubkey(r)
        origin_device_pk = _read_pubkey(r)
        target_device_pk = _read_pubkey(r)
        origin_device_location_pk = _read_pubkey(r)
        target_device_location_pk = _read_pubkey(r)
        link_pk = _read_pubkey(r)
        sampling_interval = r.read_u64()
        start_timestamp = r.read_u64()
        next_sample_index = r.read_u32()

        r.read_bytes(128)  # reserved

        count = min(next_sample_index, MAX_DEVICE_LATENCY_SAMPLES_PER_ACCOUNT)
        samples: list[int] = []
        for _ in range(count):
            if r.remaining < 4:
                break
            samples.append(r.read_u32())

        return cls(
            account_type=account_type,
            epoch=epoch,
            origin_device_agent_pk=origin_device_agent_pk,
            origin_device_pk=origin_device_pk,
            target_device_pk=target_device_pk,
            origin_device_location_pk=origin_device_location_pk,
            target_device_location_pk=target_device_location_pk,
            link_pk=link_pk,
            sampling_interval_microseconds=sampling_interval,
            start_timestamp_microseconds=start_timestamp,
            next_sample_index=next_sample_index,
            samples=samples,
        )


@dataclass
class InternetLatencySamples:
    account_type: int
    epoch: int
    data_provider_name: str
    oracle_agent_pk: Pubkey
    origin_exchange_pk: Pubkey
    target_exchange_pk: Pubkey
    sampling_interval_microseconds: int
    start_timestamp_microseconds: int
    next_sample_index: int
    samples: list[int] = field(default_factory=list)

    @classmethod
    def from_bytes(cls, data: bytes) -> InternetLatencySamples:
        if len(data) < 10:
            raise ValueError("data too short")

        r = IncrementalReader(data)

        account_type = r.read_u8()
        epoch = r.read_u64()
        data_provider_name = r.read_string()
        oracle_agent_pk = _read_pubkey(r)
        origin_exchange_pk = _read_pubkey(r)
        target_exchange_pk = _read_pubkey(r)
        sampling_interval = r.read_u64()
        start_timestamp = r.read_u64()
        next_sample_index = r.read_u32()

        r.read_bytes(128)  # reserved

        count = min(next_sample_index, MAX_INTERNET_LATENCY_SAMPLES_PER_ACCOUNT)
        samples: list[int] = []
        for _ in range(count):
            if r.remaining < 4:
                break
            samples.append(r.read_u32())

        return cls(
            account_type=account_type,
            epoch=epoch,
            data_provider_name=data_provider_name,
            oracle_agent_pk=oracle_agent_pk,
            origin_exchange_pk=origin_exchange_pk,
            target_exchange_pk=target_exchange_pk,
            sampling_interval_microseconds=sampling_interval,
            start_timestamp_microseconds=start_timestamp,
            next_sample_index=next_sample_index,
            samples=samples,
        )
