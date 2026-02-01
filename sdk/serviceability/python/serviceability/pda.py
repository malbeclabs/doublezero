"""PDA derivation for serviceability program accounts."""

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

SEED_GLOBAL_STATE = b"global_state"
SEED_GLOBAL_CONFIG = b"global_config"
SEED_PROGRAM_CONFIG = b"program_config"


def derive_global_state_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_GLOBAL_STATE], program_id)


def derive_global_config_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_GLOBAL_CONFIG], program_id)


def derive_program_config_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_PROGRAM_CONFIG], program_id)
