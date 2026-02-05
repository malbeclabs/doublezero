#!/usr/bin/env python3
"""Example CLI that fetches and displays serviceability program data."""

import argparse
import sys

from serviceability.client import Client


def format_ip(ip_bytes: bytes) -> str:
    """Format IPv4 bytes as dotted decimal string."""
    return ".".join(str(b) for b in ip_bytes[:4])


def main() -> None:
    parser = argparse.ArgumentParser(description="Fetch serviceability program data")
    parser.add_argument(
        "--env",
        default="mainnet-beta",
        choices=["mainnet-beta", "testnet", "devnet", "localnet"],
        help="Environment to connect to",
    )
    args = parser.parse_args()

    print(f"Fetching serviceability data from {args.env}...\n")

    client = Client.from_env(args.env)
    data = client.get_program_data()

    # Summary
    print("=== Summary ===")
    print(f"Locations:        {len(data.locations)}")
    print(f"Exchanges:        {len(data.exchanges)}")
    print(f"Contributors:     {len(data.contributors)}")
    print(f"Devices:          {len(data.devices)}")
    print(f"Links:            {len(data.links)}")
    print(f"Users:            {len(data.users)}")
    print(f"Multicast Groups: {len(data.multicast_groups)}")
    print(f"Access Passes:    {len(data.access_passes)}")
    print()

    # Global Config
    if data.global_config:
        print("=== Global Config ===")
        print(f"Local ASN:        {data.global_config.local_asn}")
        print(f"Remote ASN:       {data.global_config.remote_asn}")
        print()

    # Locations
    if data.locations:
        print("=== Locations ===")
        for loc in data.locations:
            print(f"  {loc.code} ({loc.name}) - {loc.country} [{loc.status}]")
        print()

    # Exchanges
    if data.exchanges:
        print("=== Exchanges ===")
        for ex in data.exchanges:
            print(f"  {ex.code} ({ex.name}) [{ex.status}]")
        print()

    # Devices
    if data.devices:
        print("=== Devices ===")
        for dev in data.devices:
            public_ip = format_ip(dev.public_ip)
            print(
                f"  {dev.code} - {public_ip} [status={dev.status}, health={dev.device_health}]"
            )
        print()

    # Links
    if data.links:
        print("=== Links ===")
        for link in data.links:
            delay_ms = link.delay_ns // 1_000_000
            print(
                f"  {link.code} - {link.link_type}, {link.bandwidth} bps, {delay_ms}ms delay [{link.status}]"
            )
        print()

    # Users
    if data.users:
        print("=== Users ===")
        for user in data.users:
            owner_short = str(user.owner)[:12]
            print(f"  {owner_short}... - {user.user_type} [{user.status}]")
        print()

    print("Done.")


if __name__ == "__main__":
    main()
