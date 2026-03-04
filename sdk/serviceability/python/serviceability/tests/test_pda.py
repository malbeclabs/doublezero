from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from serviceability.pda import (
    derive_global_config_pda,
    derive_global_state_pda,
    derive_program_config_pda,
)

PROGRAM_ID = Pubkey.from_string("ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv")


def test_derive_global_state_pda():
    addr, bump = derive_global_state_pda(PROGRAM_ID)
    assert addr != Pubkey.default()
    addr2, bump2 = derive_global_state_pda(PROGRAM_ID)
    assert addr == addr2
    assert bump == bump2


def test_derive_global_config_pda():
    addr, _ = derive_global_config_pda(PROGRAM_ID)
    assert addr != Pubkey.default()


def test_derive_program_config_pda():
    addr, _ = derive_program_config_pda(PROGRAM_ID)
    assert addr != Pubkey.default()


def test_pdas_are_different():
    gs, _ = derive_global_state_pda(PROGRAM_ID)
    gc, _ = derive_global_config_pda(PROGRAM_ID)
    pc, _ = derive_program_config_pda(PROGRAM_ID)
    assert gs != gc
    assert gs != pc
    assert gc != pc
