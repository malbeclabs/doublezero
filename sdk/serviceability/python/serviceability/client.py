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
    def from_env(cls, env: str) -> Client:
        """Create a client configured for the given environment.

        Args:
            env: Environment name ("mainnet-beta", "testnet", "devnet", "localnet")
        """
        return cls(
            new_rpc_client(LEDGER_RPC_URLS[env]),
            Pubkey.from_string(PROGRAM_IDS[env]),
        )

    @classmethod
    def mainnet_beta(cls) -> Client:
        return cls.from_env("mainnet-beta")

    @classmethod
    def testnet(cls) -> Client:
        return cls.from_env("testnet")

    @classmethod
    def devnet(cls) -> Client:
        return cls.from_env("devnet")

    @classmethod
    def localnet(cls) -> Client:
        return cls.from_env("localnet")

    def get_program_data(self) -> ProgramData:
        """Fetch all program accounts and deserialize them by type."""
        from solana.rpc.types import MemcmpOpts  # type: ignore[import-untyped]
        from serviceability.state import AccountTypeEnum

        resp = self._solana_rpc.get_program_accounts(
            self._program_id,
            encoding="base64",
        )

        pd = ProgramData()
        for acct in resp.value:
            data = bytes(acct.account.data)
            if len(data) == 0:
                continue

            account_type = data[0]
            pubkey = acct.pubkey

            if account_type == AccountTypeEnum.GLOBAL_STATE:
                pd.global_state = GlobalState.from_bytes(data)
            elif account_type == AccountTypeEnum.GLOBAL_CONFIG:
                pd.global_config = GlobalConfig.from_bytes(data)
            elif account_type == AccountTypeEnum.LOCATION:
                loc = Location.from_bytes(data)
                pd.locations.append(loc)
            elif account_type == AccountTypeEnum.EXCHANGE:
                ex = Exchange.from_bytes(data)
                pd.exchanges.append(ex)
            elif account_type == AccountTypeEnum.DEVICE:
                dev = Device.from_bytes(data)
                pd.devices.append(dev)
            elif account_type == AccountTypeEnum.LINK:
                lk = Link.from_bytes(data)
                pd.links.append(lk)
            elif account_type == AccountTypeEnum.USER:
                user = User.from_bytes(data)
                pd.users.append(user)
            elif account_type == AccountTypeEnum.MULTICAST_GROUP:
                mg = MulticastGroup.from_bytes(data)
                pd.multicast_groups.append(mg)
            elif account_type == AccountTypeEnum.PROGRAM_CONFIG:
                pd.program_config = ProgramConfig.from_bytes(data)
            elif account_type == AccountTypeEnum.CONTRIBUTOR:
                contrib = Contributor.from_bytes(data)
                pd.contributors.append(contrib)
            elif account_type == AccountTypeEnum.ACCESS_PASS:
                ap = AccessPass.from_bytes(data)
                pd.access_passes.append(ap)

        return pd
