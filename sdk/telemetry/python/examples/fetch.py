#!/usr/bin/env python3
"""Example CLI that fetches and displays telemetry data."""

import argparse
import sys
import time

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from serviceability.client import Client as ServiceabilityClient
from telemetry.client import Client as TelemetryClient


def main() -> None:
    parser = argparse.ArgumentParser(description="Fetch telemetry data")
    parser.add_argument(
        "--env",
        default="mainnet-beta",
        choices=["mainnet-beta", "testnet", "devnet", "localnet"],
        help="Environment to connect to",
    )
    parser.add_argument(
        "--epoch",
        type=int,
        default=0,
        help="Epoch to fetch samples for (0 = try recent epochs)",
    )
    args = parser.parse_args()

    print(f"Fetching telemetry data from {args.env}...\n")

    # First, get serviceability data to discover devices and links
    svc_client = ServiceabilityClient.from_env(args.env)
    svc_data = svc_client.get_program_data()

    print("=== Network Overview ===")
    print(f"Devices: {len(svc_data.devices)}")
    print(f"Links:   {len(svc_data.links)}")
    print()

    if not svc_data.links:
        print("No links found - no telemetry data to fetch.")
        return

    # Build device code map for display
    device_codes: dict[str, str] = {}
    device_pks: dict[str, Pubkey] = {}
    for dev in svc_data.devices:
        pk_str = str(dev.owner)  # Using owner as pubkey proxy
        device_codes[pk_str] = dev.code
        device_pks[dev.code] = dev.owner

    # Create telemetry client
    tel_client = TelemetryClient.from_env(args.env)

    # Determine which epoch to try
    target_epoch = args.epoch
    if target_epoch == 0:
        # Rough approximation based on current time
        target_epoch = int(time.time() / 432000)

    print(f"=== Device Latency Samples (epoch {target_epoch}) ===")

    samples_found = 0
    for link in svc_data.links:
        side_a_pk = link.side_a_pub_key
        side_z_pk = link.side_z_pub_key
        link_pk = link.owner  # Using owner as link pubkey proxy

        # Find device codes
        side_a_code = "unknown"
        side_z_code = "unknown"
        for dev in svc_data.devices:
            if str(dev.owner) == str(side_a_pk):
                side_a_code = dev.code
            if str(dev.owner) == str(side_z_pk):
                side_z_code = dev.code

        # Try both directions
        for origin_pk, target_pk, o_code, t_code in [
            (side_a_pk, side_z_pk, side_a_code, side_z_code),
            (side_z_pk, side_a_pk, side_z_code, side_a_code),
        ]:
            try:
                samples = tel_client.get_device_latency_samples(
                    origin_pk, target_pk, link_pk, target_epoch
                )
            except Exception:
                # Account likely doesn't exist for this epoch
                continue

            samples_found += 1
            sample_count = len(samples.samples)

            if sample_count == 0:
                print(f"  {o_code} -> {t_code} ({link.code}): initialized, no samples yet")
                continue

            # Calculate stats
            total = sum(samples.samples)
            min_val = min(samples.samples)
            max_val = max(samples.samples)
            avg_us = total / sample_count
            avg_ms = avg_us / 1000.0
            min_ms = min_val / 1000.0
            max_ms = max_val / 1000.0

            print(
                f"  {o_code} -> {t_code} ({link.code}): {sample_count} samples, "
                f"avg {avg_ms:.2f}ms, min {min_ms:.2f}ms, max {max_ms:.2f}ms"
            )

    if samples_found == 0:
        print(f"  No samples found for epoch {target_epoch}. Try a different epoch with --epoch flag.")

    print()
    print("Done.")


if __name__ == "__main__":
    main()
