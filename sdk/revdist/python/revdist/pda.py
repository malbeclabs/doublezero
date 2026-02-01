"""PDA derivation for revenue distribution program accounts."""

import struct

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

SEED_PROGRAM_CONFIG = b"program_config"
SEED_DISTRIBUTION = b"distribution"
SEED_SOLANA_VALIDATOR_DEPOSIT = b"solana_validator_deposit"
SEED_CONTRIBUTOR_REWARDS = b"contributor_rewards"
SEED_JOURNAL = b"journal"
SEED_SOLANA_VALIDATOR_DEBT = b"solana_validator_debt"
SEED_REWARD_SHARE = b"reward_share"


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


def derive_validator_debt_pda(
    program_id: Pubkey, epoch: int
) -> tuple[Pubkey, int]:
    epoch_bytes = struct.pack("<Q", epoch)
    return Pubkey.find_program_address(
        [SEED_SOLANA_VALIDATOR_DEBT, epoch_bytes], program_id
    )


def derive_reward_share_pda(
    program_id: Pubkey, epoch: int
) -> tuple[Pubkey, int]:
    epoch_bytes = struct.pack("<Q", epoch)
    return Pubkey.find_program_address(
        [SEED_REWARD_SHARE, epoch_bytes], program_id
    )
