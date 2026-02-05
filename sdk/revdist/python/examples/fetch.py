#!/usr/bin/env python3
"""Example CLI that fetches and displays revenue distribution data."""

import argparse
import sys

from revdist.client import Client


def main() -> None:
    parser = argparse.ArgumentParser(description="Fetch revenue distribution data")
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
        help="Specific epoch to fetch distribution for (0 = use latest from config)",
    )
    args = parser.parse_args()

    print(f"Fetching revenue distribution data from {args.env}...\n")

    client = Client.from_env(args.env)

    # Fetch program config
    try:
        config = client.fetch_config()
    except Exception as e:
        print(f"Error fetching config: {e}")
        sys.exit(1)

    print("=== Program Config ===")
    print(f"Admin:                  {config.admin_key}")
    print(f"Debt Accountant:        {config.debt_accountant_key}")
    print(f"Rewards Accountant:     {config.rewards_accountant_key}")
    print(f"Contributor Manager:    {config.contributor_manager_key}")
    print(f"Next Completed Epoch:   {config.next_completed_dz_epoch}")
    print()

    print("=== Distribution Parameters ===")
    print(f"Calculation Grace Period:   {config.distribution_parameters.calculation_grace_period_minutes} minutes")
    print(f"Initialization Grace:       {config.distribution_parameters.initialization_grace_period_minutes} minutes")
    print(f"Min Epoch Duration:         {config.distribution_parameters.minimum_epoch_duration_to_finalize_rewards}")
    print()

    vfee = config.distribution_parameters.solana_validator_fee_parameters
    print("=== Validator Fee Parameters ===")
    print(f"Base Block Rewards:     {vfee.base_block_rewards_pct / 100:.2f}%")
    print(f"Priority Block Rewards: {vfee.priority_block_rewards_pct / 100:.2f}%")
    print(f"Inflation Rewards:      {vfee.inflation_rewards_pct / 100:.2f}%")
    print(f"Jito Tips:              {vfee.jito_tips_pct / 100:.2f}%")
    print()

    # Fetch distribution for a specific epoch
    target_epoch = args.epoch
    if target_epoch == 0 and config.next_completed_dz_epoch > 0:
        target_epoch = config.next_completed_dz_epoch - 1

    if target_epoch > 0:
        try:
            dist = client.fetch_distribution(target_epoch)
            print(f"=== Distribution (epoch {dist.dz_epoch}) ===")
            print(f"Community Burn Rate:            {dist.community_burn_rate} ({dist.community_burn_rate / 1_000_000_000 * 100:.2f}%)")
            print(f"Total Solana Validators:        {dist.total_solana_validators}")
            print(f"Validator Payments Count:       {dist.solana_validator_payments_count}")
            print(f"Total Validator Debt:           {dist.total_solana_validator_debt} lamports")
            print(f"Collected Validator Payments:   {dist.collected_solana_validator_payments} lamports")
            print(f"Total Contributors:             {dist.total_contributors}")
            print(f"Distributed Rewards Count:      {dist.distributed_rewards_count}")
            print(f"Collected Prepaid 2Z:           {dist.collected_prepaid_2z_payments}")
            print(f"2Z Converted from SOL:          {dist.collected_2z_converted_from_sol}")
            print(f"Distributed 2Z Amount:          {dist.distributed_2z_amount}")
        except Exception as e:
            print(f"=== Distribution (epoch {target_epoch}) ===")
            print(f"  Not found or error: {e}")
        print()

    # Fetch journal
    try:
        journal = client.fetch_journal()
        print("=== Journal ===")
        print(f"Total SOL Balance:          {journal.total_sol_balance} lamports")
        print(f"Total 2Z Balance:           {journal.total_2z_balance}")
        print(f"Swapped SOL Amount:         {journal.swapped_sol_amount} lamports")
        print(f"Next Epoch to Sweep:        {journal.next_dz_epoch_to_sweep_tokens}")
    except Exception as e:
        print("=== Journal ===")
        print(f"  Not found or error: {e}")
    print()

    # Fetch all validator deposits
    try:
        deposits = client.fetch_all_validator_deposits()
        print(f"=== Validator Deposits ({len(deposits)}) ===")
        for i, dep in enumerate(deposits[:10]):
            print(f"  {str(dep.node_id)[:16]}...: written off debt {dep.written_off_sol_debt}")
        if len(deposits) > 10:
            print(f"  ... and {len(deposits) - 10} more")
    except Exception as e:
        print("=== Validator Deposits ===")
        print(f"  Error: {e}")
    print()

    # Fetch all contributor rewards
    try:
        rewards = client.fetch_all_contributor_rewards()
        print(f"=== Contributor Rewards ({len(rewards)}) ===")
        for i, r in enumerate(rewards[:10]):
            print(f"  {str(r.service_key)[:16]}...: rewards manager {str(r.rewards_manager_key)[:16]}...")
        if len(rewards) > 10:
            print(f"  ... and {len(rewards) - 10} more")
    except Exception as e:
        print("=== Contributor Rewards ===")
        print(f"  Error: {e}")
    print()

    print("Done.")


if __name__ == "__main__":
    main()
