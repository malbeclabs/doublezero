"""RPC client for fetching revenue distribution program accounts."""

from __future__ import annotations

import struct
from typing import Protocol

from solders.pubkey import Pubkey  # type: ignore[import-untyped]
from solders.rpc.responses import GetAccountInfoResp  # type: ignore[import-untyped]

from revdist.discriminator import (
    DISCRIMINATOR_CONTRIBUTOR_REWARDS,
    DISCRIMINATOR_DISTRIBUTION,
    DISCRIMINATOR_JOURNAL,
    DISCRIMINATOR_PROGRAM_CONFIG,
    DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT,
)
from revdist.pda import (
    derive_config_pda,
    derive_contributor_rewards_pda,
    derive_distribution_pda,
    derive_journal_pda,
    derive_validator_deposit_pda,
)
from revdist.state import (
    ContributorRewards,
    Distribution,
    Journal,
    ProgramConfig,
    SolanaValidatorDeposit,
)


class SolanaClient(Protocol):
    def get_account_info(self, pubkey: Pubkey) -> GetAccountInfoResp: ...


class Client:
    """Read-only client for revenue distribution program accounts."""

    def __init__(self, rpc: SolanaClient, program_id: Pubkey) -> None:
        self._rpc = rpc
        self._program_id = program_id

    def fetch_config(self) -> ProgramConfig:
        addr, _ = derive_config_pda(self._program_id)
        data = self._fetch_account_data(addr)
        return ProgramConfig.from_bytes(data, DISCRIMINATOR_PROGRAM_CONFIG)

    def fetch_distribution(self, epoch: int) -> Distribution:
        addr, _ = derive_distribution_pda(self._program_id, epoch)
        data = self._fetch_account_data(addr)
        return Distribution.from_bytes(data, DISCRIMINATOR_DISTRIBUTION)

    def fetch_journal(self) -> Journal:
        addr, _ = derive_journal_pda(self._program_id)
        data = self._fetch_account_data(addr)
        return Journal.from_bytes(data, DISCRIMINATOR_JOURNAL)

    def fetch_validator_deposit(
        self, node_id: Pubkey
    ) -> SolanaValidatorDeposit:
        addr, _ = derive_validator_deposit_pda(self._program_id, node_id)
        data = self._fetch_account_data(addr)
        return SolanaValidatorDeposit.from_bytes(
            data, DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT
        )

    def fetch_contributor_rewards(
        self, service_key: Pubkey
    ) -> ContributorRewards:
        addr, _ = derive_contributor_rewards_pda(self._program_id, service_key)
        data = self._fetch_account_data(addr)
        return ContributorRewards.from_bytes(
            data, DISCRIMINATOR_CONTRIBUTOR_REWARDS
        )

    def _fetch_account_data(self, addr: Pubkey) -> bytes:
        resp = self._rpc.get_account_info(addr)
        if resp.value is None:
            raise ValueError(f"account not found: {addr}")
        return bytes(resp.value.data)
