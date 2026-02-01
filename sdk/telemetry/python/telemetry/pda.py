"""PDA derivation for telemetry program accounts."""

import struct

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from telemetry.state import (
    TELEMETRY_SEED_PREFIX,
    DEVICE_LATENCY_SAMPLES_SEED,
    INTERNET_LATENCY_SAMPLES_SEED,
)


def derive_device_latency_samples_pda(
    program_id: Pubkey,
    origin_device_pk: Pubkey,
    target_device_pk: Pubkey,
    link_pk: Pubkey,
    epoch: int,
) -> tuple[Pubkey, int]:
    epoch_bytes = struct.pack("<Q", epoch)
    return Pubkey.find_program_address(
        [
            TELEMETRY_SEED_PREFIX,
            DEVICE_LATENCY_SAMPLES_SEED,
            bytes(origin_device_pk),
            bytes(target_device_pk),
            bytes(link_pk),
            epoch_bytes,
        ],
        program_id,
    )


def derive_internet_latency_samples_pda(
    program_id: Pubkey,
    collector_oracle_pk: Pubkey,
    data_provider_name: str,
    origin_location_pk: Pubkey,
    target_location_pk: Pubkey,
    epoch: int,
) -> tuple[Pubkey, int]:
    epoch_bytes = struct.pack("<Q", epoch)
    return Pubkey.find_program_address(
        [
            TELEMETRY_SEED_PREFIX,
            INTERNET_LATENCY_SAMPLES_SEED,
            bytes(collector_oracle_pk),
            data_provider_name.encode("utf-8"),
            bytes(origin_location_pk),
            bytes(target_location_pk),
            epoch_bytes,
        ],
        program_id,
    )
