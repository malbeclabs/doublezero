"""RPC client for fetching revenue distribution program accounts."""

from __future__ import annotations

import struct
from typing import Protocol

from solana.rpc.api import Client as SolanaHTTPClient  # type: ignore[import-untyped]

from revdist.rpc import new_rpc_client
from solders.pubkey import Pubkey  # type: ignore[import-untyped]
from solders.rpc.responses import GetAccountInfoResp  # type: ignore[import-untyped]

from revdist.config import LEDGER_RPC_URLS, PROGRAM_ID, SOLANA_RPC_URLS
from revdist.discriminator import (
    DISCRIMINATOR_CONTRIBUTOR_REWARDS,
    DISCRIMINATOR_DISTRIBUTION,
    DISCRIMINATOR_JOURNAL,
    DISCRIMINATOR_PROGRAM_CONFIG,
    DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT,
)
from revdist.pda import (
    RECORD_HEADER_SIZE,
    derive_config_pda,
    derive_contributor_rewards_pda,
    derive_distribution_pda,
    derive_journal_pda,
    derive_reward_share_record_key,
    derive_validator_debt_record_key,
    derive_validator_deposit_pda,
)
from revdist.state import (
    ComputedSolanaValidatorDebts,
    ContributorRewards,
    Distribution,
    Journal,
    ProgramConfig,
    ShapleyOutputStorage,
    SolanaValidatorDeposit,
)


class SolanaClient(Protocol):
    def get_account_info(self, pubkey: Pubkey) -> GetAccountInfoResp: ...


class Client:
    """Read-only client for revenue distribution program accounts."""

    def __init__(
        self,
        solana_rpc: SolanaClient,
        ledger_rpc: SolanaClient,
        program_id: Pubkey,
    ) -> None:
        self._solana_rpc = solana_rpc
        self._ledger_rpc = ledger_rpc
        self._program_id = program_id

    @classmethod
    def mainnet_beta(cls) -> Client:
        """Create a client configured for mainnet-beta."""
        return cls(
            new_rpc_client(SOLANA_RPC_URLS["mainnet-beta"]),
            new_rpc_client(LEDGER_RPC_URLS["mainnet-beta"]),
            Pubkey.from_string(PROGRAM_ID),
        )

    @classmethod
    def testnet(cls) -> Client:
        """Create a client configured for testnet."""
        return cls(
            new_rpc_client(SOLANA_RPC_URLS["testnet"]),
            new_rpc_client(LEDGER_RPC_URLS["testnet"]),
            Pubkey.from_string(PROGRAM_ID),
        )

    @classmethod
    def devnet(cls) -> Client:
        """Create a client configured for devnet."""
        return cls(
            new_rpc_client(SOLANA_RPC_URLS["devnet"]),
            new_rpc_client(LEDGER_RPC_URLS["devnet"]),
            Pubkey.from_string(PROGRAM_ID),
        )

    @classmethod
    def localnet(cls) -> Client:
        """Create a client configured for localnet."""
        return cls(
            new_rpc_client(SOLANA_RPC_URLS["localnet"]),
            new_rpc_client(LEDGER_RPC_URLS["localnet"]),
            Pubkey.from_string(PROGRAM_ID),
        )

    # -- Solana RPC (on-chain accounts) --

    def fetch_config(self) -> ProgramConfig:
        addr, _ = derive_config_pda(self._program_id)
        data = self._fetch_solana_account_data(addr)
        return ProgramConfig.from_bytes(data, DISCRIMINATOR_PROGRAM_CONFIG)

    def fetch_distribution(self, epoch: int) -> Distribution:
        addr, _ = derive_distribution_pda(self._program_id, epoch)
        data = self._fetch_solana_account_data(addr)
        return Distribution.from_bytes(data, DISCRIMINATOR_DISTRIBUTION)

    def fetch_journal(self) -> Journal:
        addr, _ = derive_journal_pda(self._program_id)
        data = self._fetch_solana_account_data(addr)
        return Journal.from_bytes(data, DISCRIMINATOR_JOURNAL)

    def fetch_validator_deposit(
        self, node_id: Pubkey
    ) -> SolanaValidatorDeposit:
        addr, _ = derive_validator_deposit_pda(self._program_id, node_id)
        data = self._fetch_solana_account_data(addr)
        return SolanaValidatorDeposit.from_bytes(
            data, DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT
        )

    def fetch_contributor_rewards(
        self, service_key: Pubkey
    ) -> ContributorRewards:
        addr, _ = derive_contributor_rewards_pda(self._program_id, service_key)
        data = self._fetch_solana_account_data(addr)
        return ContributorRewards.from_bytes(
            data, DISCRIMINATOR_CONTRIBUTOR_REWARDS
        )

    def fetch_all_validator_deposits(self) -> list[SolanaValidatorDeposit]:
        return self._fetch_all_by_discriminator(
            DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT,
            SolanaValidatorDeposit,
        )

    def fetch_all_contributor_rewards(self) -> list[ContributorRewards]:
        return self._fetch_all_by_discriminator(
            DISCRIMINATOR_CONTRIBUTOR_REWARDS,
            ContributorRewards,
        )

    # -- DZ Ledger RPC (ledger records) --

    def fetch_validator_debts(
        self, epoch: int
    ) -> ComputedSolanaValidatorDebts:
        config = self.fetch_config()
        addr = derive_validator_debt_record_key(config.debt_accountant_key, epoch)
        data = self._fetch_ledger_record_data(addr)
        return ComputedSolanaValidatorDebts.from_bytes(data[RECORD_HEADER_SIZE:])

    def fetch_reward_shares(self, epoch: int) -> ShapleyOutputStorage:
        config = self.fetch_config()
        addr = derive_reward_share_record_key(config.rewards_accountant_key, epoch)
        data = self._fetch_ledger_record_data(addr)
        return ShapleyOutputStorage.from_bytes(data[RECORD_HEADER_SIZE:])

    # -- Internal helpers --

    def _fetch_solana_account_data(self, addr: Pubkey) -> bytes:
        resp = self._solana_rpc.get_account_info(addr)
        if resp.value is None:
            raise ValueError(f"account not found: {addr}")
        return bytes(resp.value.data)

    def _fetch_ledger_record_data(self, addr: Pubkey) -> bytes:
        resp = self._ledger_rpc.get_account_info(addr)
        if resp.value is None:
            raise ValueError(f"ledger record not found: {addr}")
        return bytes(resp.value.data)

    def _fetch_all_by_discriminator(
        self,
        disc: bytes,
        cls: type,
    ) -> list:
        from solana.rpc.types import MemcmpOpts  # type: ignore[import-untyped]

        import base58  # type: ignore[import-untyped]

        filters = [MemcmpOpts(offset=0, bytes=base58.b58encode(disc).decode())]
        resp = self._solana_rpc.get_program_accounts(
            self._program_id,
            encoding="base64",
            filters=filters,
        )
        results = []
        for acct in resp.value:
            data = bytes(acct.account.data)
            results.append(cls.from_bytes(data, disc))
        return results
