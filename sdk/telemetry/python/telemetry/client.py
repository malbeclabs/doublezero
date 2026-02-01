"""RPC client for fetching telemetry program accounts."""

from __future__ import annotations

from typing import Protocol

from solders.pubkey import Pubkey  # type: ignore[import-untyped]
from solders.rpc.responses import GetAccountInfoResp  # type: ignore[import-untyped]

from telemetry.config import PROGRAM_IDS, LEDGER_RPC_URLS
from telemetry.rpc import new_rpc_client
from telemetry.pda import (
    derive_device_latency_samples_pda,
    derive_internet_latency_samples_pda,
)
from telemetry.state import DeviceLatencySamples, InternetLatencySamples


class SolanaClient(Protocol):
    def get_account_info(self, pubkey: Pubkey) -> GetAccountInfoResp: ...


class Client:
    """Read-only client for telemetry program accounts."""

    def __init__(
        self,
        solana_rpc: SolanaClient,
        program_id: Pubkey,
    ) -> None:
        self._solana_rpc = solana_rpc
        self._program_id = program_id

    @classmethod
    def mainnet_beta(cls) -> Client:
        return cls(
            new_rpc_client(LEDGER_RPC_URLS["mainnet-beta"]),
            Pubkey.from_string(PROGRAM_IDS["mainnet-beta"]),
        )

    @classmethod
    def testnet(cls) -> Client:
        return cls(
            new_rpc_client(LEDGER_RPC_URLS["testnet"]),
            Pubkey.from_string(PROGRAM_IDS["testnet"]),
        )

    @classmethod
    def devnet(cls) -> Client:
        return cls(
            new_rpc_client(LEDGER_RPC_URLS["devnet"]),
            Pubkey.from_string(PROGRAM_IDS["devnet"]),
        )

    @classmethod
    def localnet(cls) -> Client:
        return cls(
            new_rpc_client(LEDGER_RPC_URLS["localnet"]),
            Pubkey.from_string(PROGRAM_IDS["localnet"]),
        )

    def get_device_latency_samples(
        self,
        origin_device_pk: Pubkey,
        target_device_pk: Pubkey,
        link_pk: Pubkey,
        epoch: int,
    ) -> DeviceLatencySamples:
        addr, _ = derive_device_latency_samples_pda(
            self._program_id, origin_device_pk, target_device_pk, link_pk, epoch
        )
        resp = self._solana_rpc.get_account_info(addr)
        return DeviceLatencySamples.from_bytes(resp.value.data)

    def get_internet_latency_samples(
        self,
        collector_oracle_pk: Pubkey,
        data_provider_name: str,
        origin_location_pk: Pubkey,
        target_location_pk: Pubkey,
        epoch: int,
    ) -> InternetLatencySamples:
        addr, _ = derive_internet_latency_samples_pda(
            self._program_id,
            collector_oracle_pk,
            data_provider_name,
            origin_location_pk,
            target_location_pk,
            epoch,
        )
        resp = self._solana_rpc.get_account_info(addr)
        return InternetLatencySamples.from_bytes(resp.value.data)
