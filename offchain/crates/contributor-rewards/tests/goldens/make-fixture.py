#!/usr/bin/env python3
"""Trim a full CompleteSnapshot capture down to a fixture that runs fast enough for git.

Exact Shapley value computation is O(2^n) in the number of operators, with one LP solve
per coalition per demand city. The full mainnet-beta capture has 14 operators across
about 30 demand cities: roughly 16,384 coalitions per city, tens of minutes of CPU even
in a release build. Byte size is not the constraint here, runtime is: a golden fixture
nobody can afford to run in CI is worthless. So this script does not preserve the full
network topology. It keeps a small, still-connected sub-network:

- Rank contributors by how many devices they own. Keep the top `--operator-cap` (default
  4) and drop the devices, and then the links, belonging to everyone else.
- Rank the remaining devices' exchanges (cities) by device count. Keep the top
  `--city-cap` (default 6) and drop devices outside them, and then their links.
- Drop users whose device no longer exists. AccessPass records carry no device
  reference in this schema (only a user_payer/validator identity), so there is nothing
  to drop there by device membership. They are left as captured.
- Truncate every `samples` array in `dz_telemetry.device_latency_samples` and
  `dz_internet.internet_latency_samples` to the first `--sample-cap` (default 32)
  entries, updating `sample_count` to match where present. 32 because
  `settings.inet_lookback.min_samples_per_link` is 20, and anything at or below risks a
  link being dropped for insufficient coverage. These telemetry records are NOT
  filtered to the surviving devices. All 328 device and 870 internet records in the
  full capture are kept, with only their sample arrays truncated, even though the trim
  above leaves only 74 of the 328 device records relevant to the surviving topology.

Everything else (locations, multicast_groups, access_passes, leader_schedule,
metro_prices) is kept as captured. None of it drives the operator or city count that
makes exact Shapley expensive.

Usage:
    python3 make-fixture.py <input snapshot path> <output fixture path> \\
        [--sample-cap N] [--operator-cap N] [--city-cap N]

The input snapshot is a full CompleteSnapshot capture. It is not committed (it lives
under the gitignored dry-run-output/ directory). Re-capture it with the crate's own
snapshot command, with network set to mainnet-beta in the config or environment, for
example:
    cargo run -p doublezero-contributor-rewards -- snapshot --epoch 129 \\
        --local-dir ../../dry-run-output/

Regenerate the committed fixture with:
    python3 make-fixture.py \\
        ../../dry-run-output/mainnet-beta-epoch-129-snapshot.json \\
        mainnet-beta-epoch-129-trimmed.json

If a future regeneration times out, lower --operator-cap or --city-cap and retry. If it
comes back all zero, the surviving sub-network is likely disconnected. Try a larger
--operator-cap with a smaller --city-cap: more operators concentrated into fewer
cities makes a connecting path more likely. See the task 1a report for the timings
this cap was chosen against.
"""

import argparse
import json
from collections import Counter


def truncate_samples(records, sample_cap):
    """Truncate each record's `samples` list to `sample_cap` entries in place.

    Updates `sample_count` to match when the record carries one, so the record stays
    internally consistent with its own sample list.
    """
    for record in records:
        samples = record.get("samples")
        if samples is None:
            continue
        record["samples"] = samples[:sample_cap]
        if "sample_count" in record:
            record["sample_count"] = len(record["samples"])


def top_keys_by_count(counts, cap):
    """Return the `cap` keys with the highest counts, ties broken by key for a
    deterministic result independent of dict iteration order."""
    ranked = sorted(counts.items(), key=lambda pair: (-pair[1], pair[0]))
    return [key for key, _count in ranked[:cap]]


def trim_topology(fetch_data, operator_cap, city_cap):
    serviceability = fetch_data["dz_serviceability"]
    devices = serviceability["devices"]

    devices_per_contributor = Counter(
        device["contributor_pk"] for device in devices.values()
    )
    kept_contributor_pks = set(top_keys_by_count(devices_per_contributor, operator_cap))
    kept_device_pks = {
        device_pk
        for device_pk, device in devices.items()
        if device["contributor_pk"] in kept_contributor_pks
    }

    devices_per_exchange = Counter(
        devices[device_pk]["exchange_pk"] for device_pk in kept_device_pks
    )
    kept_exchange_pks = set(top_keys_by_count(devices_per_exchange, city_cap))
    kept_device_pks = {
        device_pk
        for device_pk in kept_device_pks
        if devices[device_pk]["exchange_pk"] in kept_exchange_pks
    }

    # Recompute the surviving contributor set from the surviving devices: a contributor
    # that made the operator cap can still lose every device to the city cap.
    kept_contributor_pks = {
        devices[device_pk]["contributor_pk"] for device_pk in kept_device_pks
    }

    serviceability["devices"] = {
        device_pk: device
        for device_pk, device in devices.items()
        if device_pk in kept_device_pks
    }
    serviceability["contributors"] = {
        contributor_pk: contributor
        for contributor_pk, contributor in serviceability["contributors"].items()
        if contributor_pk in kept_contributor_pks
    }
    serviceability["exchanges"] = {
        exchange_pk: exchange
        for exchange_pk, exchange in serviceability["exchanges"].items()
        if exchange_pk in kept_exchange_pks
    }
    serviceability["links"] = {
        link_pk: link
        for link_pk, link in serviceability["links"].items()
        if link["side_a_pk"] in kept_device_pks and link["side_z_pk"] in kept_device_pks
    }
    serviceability["users"] = {
        user_pk: user
        for user_pk, user in serviceability["users"].items()
        if user.get("device_pk") in kept_device_pks
    }

    assert serviceability["devices"], "trim left no devices, raise --operator-cap or --city-cap"
    assert serviceability["contributors"], "trim left no contributors"
    assert serviceability["exchanges"], "trim left no exchanges"
    assert serviceability["users"], "trim left no users, raise --operator-cap or --city-cap"


def make_fixture(input_path, output_path, sample_cap, operator_cap, city_cap):
    with open(input_path) as input_file:
        snapshot = json.load(input_file)

    fetch_data = snapshot["fetch_data"]
    trim_topology(fetch_data, operator_cap, city_cap)
    truncate_samples(fetch_data["dz_telemetry"]["device_latency_samples"], sample_cap)
    truncate_samples(fetch_data["dz_internet"]["internet_latency_samples"], sample_cap)

    with open(output_path, "w") as output_file:
        json.dump(snapshot, output_file, indent=2)
        output_file.write("\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input_path", help="full CompleteSnapshot capture to trim")
    parser.add_argument("output_path", help="path to write the trimmed fixture to")
    parser.add_argument(
        "--sample-cap",
        type=int,
        default=32,
        help="number of samples to keep per latency record (default 32)",
    )
    parser.add_argument(
        "--operator-cap",
        type=int,
        default=4,
        help="number of contributors (by device count) to keep (default 4)",
    )
    parser.add_argument(
        "--city-cap",
        type=int,
        default=6,
        help="number of exchanges/cities (by surviving device count) to keep (default 6)",
    )
    args = parser.parse_args()
    make_fixture(
        args.input_path,
        args.output_path,
        args.sample_cap,
        args.operator_cap,
        args.city_cap,
    )


if __name__ == "__main__":
    main()
