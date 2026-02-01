"""On-chain account data structures for the telemetry program.

Binary layout: 1-byte AccountType discriminator followed by Borsh-serialized
header fields, then raw u32 LE sample values (not a Borsh Vec — count is
determined by next_sample_index).
"""

from __future__ import annotations

import struct
from dataclasses import dataclass, field

from solders.pubkey import Pubkey  # type: ignore[import-untyped]


TELEMETRY_SEED_PREFIX = b"telemetry"
DEVICE_LATENCY_SAMPLES_SEED = b"dzlatency"
INTERNET_LATENCY_SAMPLES_SEED = b"inetlatency"

MAX_DEVICE_LATENCY_SAMPLES_PER_ACCOUNT = 35_000
MAX_INTERNET_LATENCY_SAMPLES_PER_ACCOUNT = 3_000

DEVICE_LATENCY_HEADER_SIZE = 1 + 8 + 32 * 6 + 8 + 8 + 4 + 128


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

        off = 0
        account_type = data[off]
        off += 1

        (epoch,) = struct.unpack_from("<Q", data, off)
        off += 8

        origin_device_agent_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32
        origin_device_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32
        target_device_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32
        origin_device_location_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32
        target_device_location_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32
        link_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32

        (sampling_interval,) = struct.unpack_from("<Q", data, off)
        off += 8
        (start_timestamp,) = struct.unpack_from("<Q", data, off)
        off += 8
        (next_sample_index,) = struct.unpack_from("<I", data, off)
        off += 4

        off += 128  # _unused

        count = min(next_sample_index, MAX_DEVICE_LATENCY_SAMPLES_PER_ACCOUNT)
        samples: list[int] = []
        for _ in range(count):
            if off + 4 > len(data):
                break
            (v,) = struct.unpack_from("<I", data, off)
            samples.append(v)
            off += 4

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

        off = 0
        account_type = data[off]
        off += 1

        (epoch,) = struct.unpack_from("<Q", data, off)
        off += 8

        # Borsh string: 4-byte LE length + UTF-8
        (name_len,) = struct.unpack_from("<I", data, off)
        off += 4
        if off + name_len > len(data):
            raise ValueError(f"data_provider_name length {name_len} exceeds data")
        data_provider_name = data[off : off + name_len].decode("utf-8")
        off += name_len

        oracle_agent_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32
        origin_exchange_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32
        target_exchange_pk = Pubkey.from_bytes(data[off : off + 32])
        off += 32

        (sampling_interval,) = struct.unpack_from("<Q", data, off)
        off += 8
        (start_timestamp,) = struct.unpack_from("<Q", data, off)
        off += 8
        (next_sample_index,) = struct.unpack_from("<I", data, off)
        off += 4

        off += 128  # _unused

        count = min(next_sample_index, MAX_INTERNET_LATENCY_SAMPLES_PER_ACCOUNT)
        samples: list[int] = []
        for _ in range(count):
            if off + 4 > len(data):
                break
            (v,) = struct.unpack_from("<I", data, off)
            samples.append(v)
            off += 4

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
