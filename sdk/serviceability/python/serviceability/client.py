"""RPC client for fetching serviceability program accounts."""

from __future__ import annotations

from typing import Protocol

from solders.pubkey import Pubkey  # type: ignore[import-untyped]
from solders.rpc.responses import GetAccountInfoResp  # type: ignore[import-untyped]

from serviceability.config import PROGRAM_IDS, LEDGER_RPC_URLS
from serviceability.rpc import new_rpc_client
from serviceability.state import (
    AccessPass,
    Contributor,
    Device,
    Exchange,
    GlobalConfig,
    GlobalState,
    Link,
    Location,
    MulticastGroup,
    ProgramConfig,
    User,
)


class SolanaClient(Protocol):
    def get_account_info(self, pubkey: Pubkey) -> GetAccountInfoResp: ...


class ProgramData:
    """Aggregate of all serviceability program accounts."""

    def __init__(self) -> None:
        self.global_state: GlobalState | None = None
        self.global_config: GlobalConfig | None = None
        self.program_config: ProgramConfig | None = None
        self.locations: list[Location] = []
        self.exchanges: list[Exchange] = []
        self.devices: list[Device] = []
        self.links: list[Link] = []
        self.users: list[User] = []
        self.multicast_groups: list[MulticastGroup] = []
        self.contributors: list[Contributor] = []
        self.access_passes: list[AccessPass] = []


class Client:
    """Read-only client for serviceability program accounts."""

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
