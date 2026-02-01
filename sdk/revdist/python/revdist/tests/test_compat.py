"""Mainnet compatibility tests.

These tests fetch live mainnet-beta data and verify that our struct
deserialization works against real on-chain accounts.

Run with:
    REVDIST_COMPAT_TEST=1 cd sdk/revdist/python && uv run pytest -k compat -v

Requires network access to Solana mainnet RPC.
"""

import os
import struct

import pytest
from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from revdist.client import Client


def skip_unless_compat() -> None:
    if not os.environ.get("REVDIST_COMPAT_TEST"):
        pytest.skip("set REVDIST_COMPAT_TEST=1 to run compatibility tests against mainnet")


def compat_client() -> Client:
    from solana.rpc.api import Client as SolanaHTTPClient  # type: ignore[import-untyped]
    from revdist.config import PROGRAM_ID, LEDGER_RPC_URLS

    rpc_url = os.environ.get("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
    return Client(
        SolanaHTTPClient(rpc_url),
        SolanaHTTPClient(LEDGER_RPC_URLS["mainnet-beta"]),
        Pubkey.from_string(PROGRAM_ID),
    )


def _rpc_url() -> str:
    return os.environ.get("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")


def fetch_raw_account(addr: Pubkey) -> bytes:
    from solana.rpc.api import Client as SolanaHTTPClient  # type: ignore[import-untyped]

    rpc = SolanaHTTPClient(_rpc_url())
    resp = rpc.get_account_info(addr)
    assert resp.value is not None, f"account not found: {addr}"
    return bytes(resp.value.data)


def read_u8(raw: bytes, offset: int) -> int:
    return raw[offset]


def read_u16(raw: bytes, offset: int) -> int:
    return struct.unpack_from("<H", raw, offset)[0]


def read_u32(raw: bytes, offset: int) -> int:
    return struct.unpack_from("<I", raw, offset)[0]


def read_u64(raw: bytes, offset: int) -> int:
    return struct.unpack_from("<Q", raw, offset)[0]


def read_pubkey(raw: bytes, offset: int) -> Pubkey:
    return Pubkey.from_bytes(raw[offset : offset + 32])


class TestCompatProgramConfig:
    def test_deserialize(self) -> None:
        skip_unless_compat()
        client = compat_client()

        config = client.fetch_config()

        # Fetch raw bytes for independent verification.
        from revdist.config import PROGRAM_ID
        from revdist.pda import derive_config_pda

        addr, _ = derive_config_pda(Pubkey.from_string(PROGRAM_ID))
        raw = fetch_raw_account(addr)

        # Verify fields at known raw byte offsets (offset = struct_offset + 8 for discriminator).
        assert config.flags == read_u64(raw, 8), "Flags"
        assert config.next_completed_dz_epoch == read_u64(raw, 16), "NextCompletedDZEpoch"
        assert config.bump_seed == read_u8(raw, 24), "BumpSeed"
        assert config.admin_key == read_pubkey(raw, 32), "AdminKey"
        assert config.debt_accountant_key == read_pubkey(raw, 64), "DebtAccountantKey"
        assert config.rewards_accountant_key == read_pubkey(raw, 96), "RewardsAccountantKey"
        assert config.contributor_manager_key == read_pubkey(raw, 128), "ContributorManagerKey"
        assert config.sol_2z_swap_program_id == read_pubkey(raw, 192), "SOL2ZSwapProgramID"

        # DistributionParameters starts at raw offset 224.
        dp = config.distribution_parameters
        assert dp.calculation_grace_period_minutes == read_u16(raw, 224), "CalculationGracePeriodMinutes"
        assert dp.initialization_grace_period_minutes == read_u16(raw, 226), "InitializationGracePeriodMinutes"
        assert dp.minimum_epoch_duration_to_finalize_rewards == read_u8(raw, 228), "MinEpochDuration"

        # CommunityBurnRateParameters at raw offset 232.
        cb = dp.community_burn_rate_parameters
        assert cb.limit == read_u32(raw, 232), "BurnRateLimit"
        assert cb.dz_epochs_to_increasing == read_u32(raw, 236), "DZEpochsToIncreasing"
        assert cb.dz_epochs_to_limit == read_u32(raw, 240), "DZEpochsToLimit"

        # SolanaValidatorFeeParameters at raw offset 256.
        vf = dp.solana_validator_fee_parameters
        assert vf.base_block_rewards_pct == read_u16(raw, 256), "BaseBlockRewardsPct"
        assert vf.priority_block_rewards_pct == read_u16(raw, 258), "PriorityBlockRewardsPct"
        assert vf.inflation_rewards_pct == read_u16(raw, 260), "InflationRewardsPct"
        assert vf.jito_tips_pct == read_u16(raw, 262), "JitoTipsPct"
        assert vf.fixed_sol_amount == read_u32(raw, 264), "FixedSOLAmount"

        # RelayParameters at raw offset 552.
        rp = config.relay_parameters
        assert rp.placeholder_lamports == read_u32(raw, 552), "PlaceholderLamports"
        assert rp.distribute_rewards_lamports == read_u32(raw, 556), "DistributeRewardsLamports"

        # DebtWriteOffFeatureActivationEpoch at raw offset 600.
        assert config.debt_write_off_feature_activation_epoch == read_u64(raw, 600), "DebtWriteOffEpoch"

        # Sanity: epoch should be > 0 on mainnet.
        assert config.next_completed_dz_epoch > 0, "NextCompletedDZEpoch should be > 0 on mainnet"


class TestCompatDistribution:
    def test_deserialize(self) -> None:
        skip_unless_compat()
        client = compat_client()

        config = client.fetch_config()
        epoch = config.next_completed_dz_epoch - 1

        dist = client.fetch_distribution(epoch)

        # Fetch raw bytes.
        from revdist.config import PROGRAM_ID
        from revdist.pda import derive_distribution_pda

        addr, _ = derive_distribution_pda(Pubkey.from_string(PROGRAM_ID), epoch)
        raw = fetch_raw_account(addr)

        assert dist.dz_epoch == read_u64(raw, 8), "DZEpoch"
        assert dist.dz_epoch == epoch
        assert dist.flags == read_u64(raw, 16), "Flags"
        assert dist.community_burn_rate == read_u32(raw, 24), "CommunityBurnRate"

        vf = dist.solana_validator_fee_parameters
        assert vf.base_block_rewards_pct == read_u16(raw, 32), "BaseBlockRewardsPct"
        assert vf.priority_block_rewards_pct == read_u16(raw, 34), "PriorityBlockRewardsPct"
        assert vf.inflation_rewards_pct == read_u16(raw, 36), "InflationRewardsPct"
        assert vf.jito_tips_pct == read_u16(raw, 38), "JitoTipsPct"
        assert vf.fixed_sol_amount == read_u32(raw, 40), "FixedSOLAmount"

        assert dist.total_solana_validators == read_u32(raw, 104), "TotalSolanaValidators"
        assert dist.solana_validator_payments_count == read_u32(raw, 108), "SolanaValidatorPaymentsCount"
        assert dist.total_solana_validator_debt == read_u64(raw, 112), "TotalSolanaValidatorDebt"
        assert dist.collected_solana_validator_payments == read_u64(raw, 120), "CollectedPayments"
        assert dist.total_contributors == read_u32(raw, 160), "TotalContributors"
        assert dist.distributed_rewards_count == read_u32(raw, 164), "DistributedRewardsCount"
        assert dist.collected_prepaid_2z_payments == read_u64(raw, 168), "CollectedPrepaid2ZPayments"
        assert dist.collected_2z_converted_from_sol == read_u64(raw, 176), "Collected2ZConvertedFromSOL"
        assert dist.uncollectible_sol_debt == read_u64(raw, 184), "UncollectibleSOLDebt"
        assert dist.distributed_2z_amount == read_u64(raw, 216), "Distributed2ZAmount"
        assert dist.burned_2z_amount == read_u64(raw, 224), "Burned2ZAmount"


class TestCompatJournal:
    def test_deserialize(self) -> None:
        skip_unless_compat()
        client = compat_client()

        journal = client.fetch_journal()

        # Fetch raw bytes.
        from revdist.config import PROGRAM_ID
        from revdist.pda import derive_journal_pda

        addr, _ = derive_journal_pda(Pubkey.from_string(PROGRAM_ID))
        raw = fetch_raw_account(addr)

        assert journal.bump_seed == read_u8(raw, 8), "BumpSeed"
        assert journal.total_sol_balance == read_u64(raw, 16), "TotalSOLBalance"
        assert journal.total_2z_balance == read_u64(raw, 24), "Total2ZBalance"
        assert journal.swap_2z_destination_balance == read_u64(raw, 32), "Swap2ZDestinationBalance"
        assert journal.swapped_sol_amount == read_u64(raw, 40), "SwappedSOLAmount"
        assert journal.next_dz_epoch_to_sweep_tokens == read_u64(raw, 48), "NextDZEpochToSweepTokens"


class TestCompatValidatorDeposits:
    def test_fetch_all(self) -> None:
        skip_unless_compat()
        client = compat_client()

        deposits = client.fetch_all_validator_deposits()
        assert len(deposits) > 0, "no deposits found on mainnet"

        # Verify single lookup matches list entry.
        first = deposits[0]
        single = client.fetch_validator_deposit(first.node_id)
        assert single.node_id == first.node_id
        assert single.written_off_sol_debt == first.written_off_sol_debt


class TestCompatContributorRewards:
    def test_fetch_all(self) -> None:
        skip_unless_compat()
        client = compat_client()

        rewards = client.fetch_all_contributor_rewards()
        assert len(rewards) > 0, "no contributor rewards found on mainnet"

        # Verify single lookup matches list entry.
        first = rewards[0]
        single = client.fetch_contributor_rewards(first.service_key)
        assert single.service_key == first.service_key
        assert single.rewards_manager_key == first.rewards_manager_key
        assert single.flags == first.flags
