"""PDA derivation tests."""

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from revdist.pda import (
    derive_config_pda,
    derive_contributor_rewards_pda,
    derive_distribution_pda,
    derive_journal_pda,
    derive_validator_deposit_pda,
)

PROGRAM_ID = Pubkey.from_string("dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4")


def test_derive_config_pda():
    addr, bump = derive_config_pda(PROGRAM_ID)
    assert addr != Pubkey.default()
    addr2, bump2 = derive_config_pda(PROGRAM_ID)
    assert addr == addr2 and bump == bump2


def test_derive_distribution_pda_different_epochs():
    addr1, _ = derive_distribution_pda(PROGRAM_ID, 1)
    addr2, _ = derive_distribution_pda(PROGRAM_ID, 2)
    assert addr1 != addr2


def test_derive_journal_pda():
    addr, _ = derive_journal_pda(PROGRAM_ID)
    assert addr != Pubkey.default()


def test_derive_validator_deposit_pda():
    node_id = Pubkey.from_string("4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM")
    addr, _ = derive_validator_deposit_pda(PROGRAM_ID, node_id)
    assert addr != Pubkey.default()


def test_derive_contributor_rewards_pda():
    service_key = Pubkey.from_string("4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM")
    addr, _ = derive_contributor_rewards_pda(PROGRAM_ID, service_key)
    assert addr != Pubkey.default()
