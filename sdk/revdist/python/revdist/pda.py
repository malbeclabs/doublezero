"""PDA and record key derivation for revenue distribution program accounts."""

import hashlib
import struct

import base58  # type: ignore[import-untyped]
from solders.pubkey import Pubkey  # type: ignore[import-untyped]

SEED_PROGRAM_CONFIG = b"program_config"
SEED_DISTRIBUTION = b"distribution"
SEED_SOLANA_VALIDATOR_DEPOSIT = b"solana_validator_deposit"
SEED_CONTRIBUTOR_REWARDS = b"contributor_rewards"
SEED_JOURNAL = b"journal"
SEED_SOLANA_VALIDATOR_DEBT = b"solana_validator_debt"
SEED_DZ_CONTRIBUTOR_REWARDS = b"dz_contributor_rewards"
SEED_SHAPLEY_OUTPUT = b"shapley_output"

RECORD_PROGRAM_ID = Pubkey.from_string("dzrecxigtaZQ3gPmt2X5mDkYigaruFR1rHCqztFTvx7")
RECORD_HEADER_SIZE = 33


def derive_config_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_PROGRAM_CONFIG], program_id)


def derive_distribution_pda(
    program_id: Pubkey, epoch: int
) -> tuple[Pubkey, int]:
    epoch_bytes = struct.pack("<Q", epoch)
    return Pubkey.find_program_address(
        [SEED_DISTRIBUTION, epoch_bytes], program_id
    )


def derive_journal_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_JOURNAL], program_id)


def derive_validator_deposit_pda(
    program_id: Pubkey, node_id: Pubkey
) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address(
        [SEED_SOLANA_VALIDATOR_DEPOSIT, bytes(node_id)], program_id
    )


def derive_contributor_rewards_pda(
    program_id: Pubkey, service_key: Pubkey
) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address(
        [SEED_CONTRIBUTOR_REWARDS, bytes(service_key)], program_id
    )


def _create_record_seed_string(seeds: list[bytes]) -> str:
    """Hash seeds with SHA256, encode as base58, truncate to 32 chars."""
    h = hashlib.sha256()
    for s in seeds:
        h.update(s)
    return base58.b58encode(h.digest()).decode()[:32]


def derive_record_key(payer_key: Pubkey, seeds: list[bytes]) -> Pubkey:
    """Derive a ledger record address using create-with-seed."""
    seed_str = _create_record_seed_string(seeds)
    return Pubkey.create_with_seed(payer_key, seed_str, RECORD_PROGRAM_ID)


def derive_validator_debt_record_key(
    debt_accountant_key: Pubkey, epoch: int
) -> Pubkey:
    epoch_bytes = struct.pack("<Q", epoch)
    return derive_record_key(
        debt_accountant_key, [SEED_SOLANA_VALIDATOR_DEBT, epoch_bytes]
    )


def derive_reward_share_record_key(
    rewards_accountant_key: Pubkey, epoch: int
) -> Pubkey:
    epoch_bytes = struct.pack("<Q", epoch)
    return derive_record_key(
        rewards_accountant_key,
        [SEED_DZ_CONTRIBUTOR_REWARDS, epoch_bytes, SEED_SHAPLEY_OUTPUT],
    )
