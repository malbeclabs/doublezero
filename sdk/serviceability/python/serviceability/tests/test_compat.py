"""Mainnet compatibility tests.

These tests fetch live mainnet-beta data and verify that our struct
deserialization works against real on-chain accounts.

Run with:
    SERVICEABILITY_COMPAT_TEST=1 cd sdk/serviceability/python && uv run pytest -k compat -v

Requires network access to Solana mainnet RPC.
"""

import os
import struct

import pytest
from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from serviceability.config import PROGRAM_IDS, LEDGER_RPC_URLS
from serviceability.pda import (
    derive_global_config_pda,
    derive_global_state_pda,
    derive_program_config_pda,
)
from serviceability.state import GlobalConfig, GlobalState, ProgramConfig


def skip_unless_compat() -> None:
    if not os.environ.get("SERVICEABILITY_COMPAT_TEST"):
        pytest.skip("set SERVICEABILITY_COMPAT_TEST=1 to run compatibility tests against mainnet")


def _rpc_url() -> str:
    return os.environ.get("SOLANA_RPC_URL", LEDGER_RPC_URLS["mainnet-beta"])


def _program_id() -> Pubkey:
    return Pubkey.from_string(PROGRAM_IDS["mainnet-beta"])


def fetch_raw_account(addr: Pubkey) -> bytes:
    from serviceability.rpc import new_rpc_client

    rpc = new_rpc_client(_rpc_url())
    resp = rpc.get_account_info(addr)
    assert resp.value is not None, f"account not found: {addr}"
    return bytes(resp.value.data)


def read_u8(raw: bytes, offset: int) -> int:
    return raw[offset]


def read_u16(raw: bytes, offset: int) -> int:
    return struct.unpack_from("<H", raw, offset)[0]


def read_u32(raw: bytes, offset: int) -> int:
    return struct.unpack_from("<I", raw, offset)[0]


def read_pubkey(raw: bytes, offset: int) -> Pubkey:
    return Pubkey.from_bytes(raw[offset : offset + 32])


class TestCompatProgramConfig:
    def test_deserialize(self) -> None:
        skip_unless_compat()

        program_id = _program_id()
        addr, _ = derive_program_config_pda(program_id)
        raw = fetch_raw_account(addr)

        pc = ProgramConfig.from_bytes(raw)

        # ProgramConfig layout (all fixed-size):
        # offset 0: AccountType (u8)
        # offset 1: BumpSeed (u8)
        # offset 2: Version.Major (u32)
        # offset 6: Version.Minor (u32)
        # offset 10: Version.Patch (u32)
        # offset 14: MinCompatVersion.Major (u32)
        # offset 18: MinCompatVersion.Minor (u32)
        # offset 22: MinCompatVersion.Patch (u32)
        assert pc.account_type == read_u8(raw, 0), "AccountType"
        assert pc.bump_seed == read_u8(raw, 1), "BumpSeed"
        assert pc.version.major == read_u32(raw, 2), "Version.Major"
        assert pc.version.minor == read_u32(raw, 6), "Version.Minor"
        assert pc.version.patch == read_u32(raw, 10), "Version.Patch"
        assert pc.min_compat_version.major == read_u32(raw, 14), "MinCompatVersion.Major"
        assert pc.min_compat_version.minor == read_u32(raw, 18), "MinCompatVersion.Minor"
        assert pc.min_compat_version.patch == read_u32(raw, 22), "MinCompatVersion.Patch"

        assert pc.account_type == 9


class TestCompatGlobalConfig:
    def test_deserialize(self) -> None:
        skip_unless_compat()

        program_id = _program_id()
        addr, _ = derive_global_config_pda(program_id)
        raw = fetch_raw_account(addr)

        gc = GlobalConfig.from_bytes(raw)

        # GlobalConfig layout (all fixed-size):
        # offset 0: AccountType (u8)
        # offset 1: Owner (32 bytes)
        # offset 33: BumpSeed (u8)
        # offset 34: LocalASN (u32)
        # offset 38: RemoteASN (u32)
        # offset 57: NextBGPCommunity (u16)
        assert gc.account_type == read_u8(raw, 0), "AccountType"
        assert gc.owner == read_pubkey(raw, 1), "Owner"
        assert gc.bump_seed == read_u8(raw, 33), "BumpSeed"
        assert gc.local_asn == read_u32(raw, 34), "LocalASN"
        assert gc.remote_asn == read_u32(raw, 38), "RemoteASN"
        assert gc.next_bgp_community == read_u16(raw, 57), "NextBGPCommunity"

        assert gc.account_type == 2
        assert gc.local_asn > 0, "LocalASN should be > 0 on mainnet"


class TestCompatGlobalState:
    def test_deserialize(self) -> None:
        skip_unless_compat()

        program_id = _program_id()
        addr, _ = derive_global_state_pda(program_id)
        raw = fetch_raw_account(addr)

        gs = GlobalState.from_bytes(raw)

        # GlobalState fixed layout (first 18 bytes before variable-length vecs):
        # offset 0: AccountType (u8)
        # offset 1: BumpSeed (u8)
        assert gs.account_type == read_u8(raw, 0), "AccountType"
        assert gs.bump_seed == read_u8(raw, 1), "BumpSeed"

        assert gs.account_type == 1

        # Sanity checks.
        assert gs.activator_authority_pk != Pubkey.default(), "ActivatorAuthorityPK is zero"
        assert gs.sentinel_authority_pk != Pubkey.default(), "SentinelAuthorityPK is zero"
        # health_oracle_pk may be zero on mainnet


class TestCompatGetProgramData:
    """Test fetching and deserializing all program accounts.

    This is the most comprehensive compat test - it fetches every account
    owned by the program and deserializes them all, including AccessPass
    accounts which may have various enum variants and trailing fields.
    """

    def test_deserialize_all_accounts(self) -> None:
        skip_unless_compat()

        from serviceability.client import Client

        client = Client.mainnet_beta()
        pd = client.get_program_data()

        assert pd.global_state is not None, "GlobalState is None"
        assert pd.global_config is not None, "GlobalConfig is None"
        assert pd.program_config is not None, "ProgramConfig is None"
        assert len(pd.locations) > 0, "no locations found on mainnet"
        assert len(pd.exchanges) > 0, "no exchanges found on mainnet"
        assert len(pd.devices) > 0, "no devices found on mainnet"
        assert len(pd.links) > 0, "no links found on mainnet"
        assert len(pd.contributors) > 0, "no contributors found on mainnet"

        # Log summary for debugging.
        print(
            f"\nProgramData: {len(pd.locations)} locations, {len(pd.exchanges)} exchanges, "
            f"{len(pd.devices)} devices, {len(pd.links)} links, {len(pd.users)} users, "
            f"{len(pd.contributors)} contributors, {len(pd.access_passes)} access passes"
        )
