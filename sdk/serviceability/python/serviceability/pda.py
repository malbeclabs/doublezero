"""PDA derivation for serviceability program accounts."""

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

SEED_PREFIX = b"doublezero"
SEED_GLOBAL_STATE = b"globalstate"
SEED_GLOBAL_CONFIG = b"config"
SEED_PROGRAM_CONFIG = b"programconfig"
SEED_TENANT = b"tenant"


def derive_global_state_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_PREFIX, SEED_GLOBAL_STATE], program_id)


def derive_global_config_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_PREFIX, SEED_GLOBAL_CONFIG], program_id)


def derive_program_config_pda(program_id: Pubkey) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_PREFIX, SEED_PROGRAM_CONFIG], program_id)


def derive_tenant_pda(program_id: Pubkey, code: str) -> tuple[Pubkey, int]:
    return Pubkey.find_program_address([SEED_PREFIX, SEED_TENANT, code.encode()], program_id)
