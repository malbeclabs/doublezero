"""On-chain account data structures for the serviceability program.

Binary layout uses Borsh serialization with a 1-byte AccountType discriminator
as the first byte. Deserialization uses cursor-based BorshReader.
"""

from __future__ import annotations

import struct
from dataclasses import dataclass, field
from enum import IntEnum

from solders.pubkey import Pubkey  # type: ignore[import-untyped]


# ---------------------------------------------------------------------------
# BorshReader — cursor-based binary reader (little-endian)
# ---------------------------------------------------------------------------


class BorshReader:
    def __init__(self, data: bytes) -> None:
        self._data = data
        self._offset = 0

    def read_u8(self) -> int:
        v = self._data[self._offset]
        self._offset += 1
        return v

    def read_u16(self) -> int:
        (v,) = struct.unpack_from("<H", self._data, self._offset)
        self._offset += 2
        return v

    def read_u32(self) -> int:
        (v,) = struct.unpack_from("<I", self._data, self._offset)
        self._offset += 4
        return v

    def read_u64(self) -> int:
        (v,) = struct.unpack_from("<Q", self._data, self._offset)
        self._offset += 8
        return v

    def read_u128(self) -> int:
        low, high = struct.unpack_from("<QQ", self._data, self._offset)
        self._offset += 16
        return low | (high << 64)

    def read_f64(self) -> float:
        (v,) = struct.unpack_from("<d", self._data, self._offset)
        self._offset += 8
        return v

    def read_bool(self) -> bool:
        return self.read_u8() != 0

    def read_pubkey(self) -> Pubkey:
        pk = Pubkey.from_bytes(self._data[self._offset : self._offset + 32])
        self._offset += 32
        return pk

    def read_string(self) -> str:
        length = self.read_u32()
        if length == 0:
            return ""
        s = self._data[self._offset : self._offset + length].decode("utf-8")
        self._offset += length
        return s

    def read_ipv4(self) -> bytes:
        b = self._data[self._offset : self._offset + 4]
        self._offset += 4
        return bytes(b)

    def read_network_v4(self) -> bytes:
        b = self._data[self._offset : self._offset + 5]
        self._offset += 5
        return bytes(b)

    def read_pubkey_vec(self) -> list[Pubkey]:
        length = self.read_u32()
        return [self.read_pubkey() for _ in range(length)]

    def read_network_v4_vec(self) -> list[bytes]:
        length = self.read_u32()
        return [self.read_network_v4() for _ in range(length)]

    @property
    def remaining(self) -> int:
        return len(self._data) - self._offset


# ---------------------------------------------------------------------------
# Account type discriminants
# ---------------------------------------------------------------------------


class AccountTypeEnum(IntEnum):
    GLOBAL_STATE = 1
    GLOBAL_CONFIG = 2
    LOCATION = 3
    EXCHANGE = 4
    DEVICE = 5
    LINK = 6
    USER = 7
    MULTICAST_GROUP = 8
    PROGRAM_CONFIG = 9
    CONTRIBUTOR = 10
    ACCESS_PASS = 11


# ---------------------------------------------------------------------------
# Account dataclasses
# ---------------------------------------------------------------------------

CURRENT_INTERFACE_VERSION = 2


@dataclass
class Interface:
    version: int = 0
    status: int = 0
    name: str = ""
    interface_type: int = 0
    interface_cyoa: int = 0
    interface_dia: int = 0
    loopback_type: int = 0
    bandwidth: int = 0
    cir: int = 0
    mtu: int = 0
    routing_mode: int = 0
    vlan_id: int = 0
    ip_net: bytes = b"\x00" * 5
    node_segment_idx: int = 0
    user_tunnel_endpoint: bool = False

    @classmethod
    def from_reader(cls, r: BorshReader) -> Interface:
        iface = cls()
        iface.version = r.read_u8()
        if iface.version > CURRENT_INTERFACE_VERSION - 1:
            return iface
        if iface.version == 0:  # V1
            iface.status = r.read_u8()
            iface.name = r.read_string()
            iface.interface_type = r.read_u8()
            iface.loopback_type = r.read_u8()
            iface.vlan_id = r.read_u16()
            iface.ip_net = r.read_network_v4()
            iface.node_segment_idx = r.read_u16()
            iface.user_tunnel_endpoint = r.read_bool()
        elif iface.version == 1:  # V2
            iface.status = r.read_u8()
            iface.name = r.read_string()
            iface.interface_type = r.read_u8()
            iface.interface_cyoa = r.read_u8()
            iface.interface_dia = r.read_u8()
            iface.loopback_type = r.read_u8()
            iface.bandwidth = r.read_u64()
            iface.cir = r.read_u64()
            iface.mtu = r.read_u16()
            iface.routing_mode = r.read_u8()
            iface.vlan_id = r.read_u16()
            iface.ip_net = r.read_network_v4()
            iface.node_segment_idx = r.read_u16()
            iface.user_tunnel_endpoint = r.read_bool()
        return iface


@dataclass
class GlobalState:
    account_type: int = 0
    bump_seed: int = 0
    account_index: int = 0
    foundation_allowlist: list[Pubkey] = field(default_factory=list)
    activator_authority_pk: Pubkey = Pubkey.default()
    sentinel_authority_pk: Pubkey = Pubkey.default()
    contributor_airdrop_lamports: int = 0
    user_airdrop_lamports: int = 0
    health_oracle_pk: Pubkey = Pubkey.default()
    qa_allowlist: list[Pubkey] = field(default_factory=list)

    @classmethod
    def from_bytes(cls, data: bytes) -> GlobalState:
        r = BorshReader(data)
        gs = cls()
        gs.account_type = r.read_u8()
        gs.bump_seed = r.read_u8()
        gs.account_index = r.read_u128()
        gs.foundation_allowlist = r.read_pubkey_vec()
        r.read_pubkey_vec()  # deprecated device_allowlist
        r.read_pubkey_vec()  # deprecated user_allowlist
        gs.activator_authority_pk = r.read_pubkey()
        gs.sentinel_authority_pk = r.read_pubkey()
        gs.contributor_airdrop_lamports = r.read_u64()
        gs.user_airdrop_lamports = r.read_u64()
        gs.health_oracle_pk = r.read_pubkey()
        gs.qa_allowlist = r.read_pubkey_vec()
        return gs


@dataclass
class GlobalConfig:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    bump_seed: int = 0
    local_asn: int = 0
    remote_asn: int = 0
    device_tunnel_block: bytes = b"\x00" * 5
    user_tunnel_block: bytes = b"\x00" * 5
    multicast_group_block: bytes = b"\x00" * 5
    next_bgp_community: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> GlobalConfig:
        r = BorshReader(data)
        gc = cls()
        gc.account_type = r.read_u8()
        gc.owner = r.read_pubkey()
        gc.bump_seed = r.read_u8()
        gc.local_asn = r.read_u32()
        gc.remote_asn = r.read_u32()
        gc.device_tunnel_block = r.read_network_v4()
        gc.user_tunnel_block = r.read_network_v4()
        gc.multicast_group_block = r.read_network_v4()
        gc.next_bgp_community = r.read_u16()
        return gc


@dataclass
class Location:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    lat: float = 0.0
    lng: float = 0.0
    loc_id: int = 0
    status: int = 0
    code: str = ""
    name: str = ""
    country: str = ""
    reference_count: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> Location:
        r = BorshReader(data)
        loc = cls()
        loc.account_type = r.read_u8()
        loc.owner = r.read_pubkey()
        loc.index = r.read_u128()
        loc.bump_seed = r.read_u8()
        loc.lat = r.read_f64()
        loc.lng = r.read_f64()
        loc.loc_id = r.read_u32()
        loc.status = r.read_u8()
        loc.code = r.read_string()
        loc.name = r.read_string()
        loc.country = r.read_string()
        loc.reference_count = r.read_u32()
        return loc


@dataclass
class Exchange:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    lat: float = 0.0
    lng: float = 0.0
    bgp_community: int = 0
    status: int = 0
    code: str = ""
    name: str = ""
    reference_count: int = 0
    device1_pk: Pubkey = Pubkey.default()
    device2_pk: Pubkey = Pubkey.default()

    @classmethod
    def from_bytes(cls, data: bytes) -> Exchange:
        r = BorshReader(data)
        ex = cls()
        ex.account_type = r.read_u8()
        ex.owner = r.read_pubkey()
        ex.index = r.read_u128()
        ex.bump_seed = r.read_u8()
        ex.lat = r.read_f64()
        ex.lng = r.read_f64()
        ex.bgp_community = r.read_u16()
        r.read_u16()  # unused padding
        ex.status = r.read_u8()
        ex.code = r.read_string()
        ex.name = r.read_string()
        ex.reference_count = r.read_u32()
        ex.device1_pk = r.read_pubkey()
        ex.device2_pk = r.read_pubkey()
        return ex


@dataclass
class Device:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    location_pub_key: Pubkey = Pubkey.default()
    exchange_pub_key: Pubkey = Pubkey.default()
    device_type: int = 0
    public_ip: bytes = b"\x00" * 4
    status: int = 0
    code: str = ""
    dz_prefixes: list[bytes] = field(default_factory=list)
    metrics_publisher_pub_key: Pubkey = Pubkey.default()
    contributor_pub_key: Pubkey = Pubkey.default()
    mgmt_vrf: str = ""
    interfaces: list[Interface] = field(default_factory=list)
    reference_count: int = 0
    users_count: int = 0
    max_users: int = 0
    device_health: int = 0
    device_desired_status: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> Device:
        r = BorshReader(data)
        dev = cls()
        dev.account_type = r.read_u8()
        dev.owner = r.read_pubkey()
        dev.index = r.read_u128()
        dev.bump_seed = r.read_u8()
        dev.location_pub_key = r.read_pubkey()
        dev.exchange_pub_key = r.read_pubkey()
        dev.device_type = r.read_u8()
        dev.public_ip = r.read_ipv4()
        dev.status = r.read_u8()
        dev.code = r.read_string()
        dev.dz_prefixes = r.read_network_v4_vec()
        dev.metrics_publisher_pub_key = r.read_pubkey()
        dev.contributor_pub_key = r.read_pubkey()
        dev.mgmt_vrf = r.read_string()
        iface_len = r.read_u32()
        dev.interfaces = [Interface.from_reader(r) for _ in range(iface_len)]
        dev.reference_count = r.read_u32()
        dev.users_count = r.read_u16()
        dev.max_users = r.read_u16()
        dev.device_health = r.read_u8()
        dev.device_desired_status = r.read_u8()
        return dev


@dataclass
class Link:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    side_a_pub_key: Pubkey = Pubkey.default()
    side_z_pub_key: Pubkey = Pubkey.default()
    link_type: int = 0
    bandwidth: int = 0
    mtu: int = 0
    delay_ns: int = 0
    jitter_ns: int = 0
    tunnel_id: int = 0
    tunnel_net: bytes = b"\x00" * 5
    status: int = 0
    code: str = ""
    contributor_pub_key: Pubkey = Pubkey.default()
    side_a_iface_name: str = ""
    side_z_iface_name: str = ""
    delay_override_ns: int = 0
    link_health: int = 0
    link_desired_status: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> Link:
        r = BorshReader(data)
        lk = cls()
        lk.account_type = r.read_u8()
        lk.owner = r.read_pubkey()
        lk.index = r.read_u128()
        lk.bump_seed = r.read_u8()
        lk.side_a_pub_key = r.read_pubkey()
        lk.side_z_pub_key = r.read_pubkey()
        lk.link_type = r.read_u8()
        lk.bandwidth = r.read_u64()
        lk.mtu = r.read_u32()
        lk.delay_ns = r.read_u64()
        lk.jitter_ns = r.read_u64()
        lk.tunnel_id = r.read_u16()
        lk.tunnel_net = r.read_network_v4()
        lk.status = r.read_u8()
        lk.code = r.read_string()
        lk.contributor_pub_key = r.read_pubkey()
        lk.side_a_iface_name = r.read_string()
        lk.side_z_iface_name = r.read_string()
        lk.delay_override_ns = r.read_u64()
        lk.link_health = r.read_u8()
        lk.link_desired_status = r.read_u8()
        return lk


@dataclass
class User:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    user_type: int = 0
    tenant_pub_key: Pubkey = Pubkey.default()
    device_pub_key: Pubkey = Pubkey.default()
    cyoa_type: int = 0
    client_ip: bytes = b"\x00" * 4
    dz_ip: bytes = b"\x00" * 4
    tunnel_id: int = 0
    tunnel_net: bytes = b"\x00" * 5
    status: int = 0
    publishers: list[Pubkey] = field(default_factory=list)
    subscribers: list[Pubkey] = field(default_factory=list)
    validator_pub_key: Pubkey = Pubkey.default()

    @classmethod
    def from_bytes(cls, data: bytes) -> User:
        r = BorshReader(data)
        u = cls()
        u.account_type = r.read_u8()
        u.owner = r.read_pubkey()
        u.index = r.read_u128()
        u.bump_seed = r.read_u8()
        u.user_type = r.read_u8()
        u.tenant_pub_key = r.read_pubkey()
        u.device_pub_key = r.read_pubkey()
        u.cyoa_type = r.read_u8()
        u.client_ip = r.read_ipv4()
        u.dz_ip = r.read_ipv4()
        u.tunnel_id = r.read_u16()
        u.tunnel_net = r.read_network_v4()
        u.status = r.read_u8()
        u.publishers = r.read_pubkey_vec()
        u.subscribers = r.read_pubkey_vec()
        u.validator_pub_key = r.read_pubkey()
        return u


@dataclass
class MulticastGroup:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    tenant_pub_key: Pubkey = Pubkey.default()
    multicast_ip: bytes = b"\x00" * 4
    max_bandwidth: int = 0
    status: int = 0
    code: str = ""
    publisher_count: int = 0
    subscriber_count: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> MulticastGroup:
        r = BorshReader(data)
        mg = cls()
        mg.account_type = r.read_u8()
        mg.owner = r.read_pubkey()
        mg.index = r.read_u128()
        mg.bump_seed = r.read_u8()
        mg.tenant_pub_key = r.read_pubkey()
        mg.multicast_ip = r.read_ipv4()
        mg.max_bandwidth = r.read_u64()
        mg.status = r.read_u8()
        mg.code = r.read_string()
        mg.publisher_count = r.read_u32()
        mg.subscriber_count = r.read_u32()
        return mg


@dataclass
class ProgramVersion:
    major: int = 0
    minor: int = 0
    patch: int = 0


@dataclass
class ProgramConfig:
    account_type: int = 0
    bump_seed: int = 0
    version: ProgramVersion = field(default_factory=ProgramVersion)
    min_compat_version: ProgramVersion = field(default_factory=ProgramVersion)

    @classmethod
    def from_bytes(cls, data: bytes) -> ProgramConfig:
        r = BorshReader(data)
        pc = cls()
        pc.account_type = r.read_u8()
        pc.bump_seed = r.read_u8()
        pc.version = ProgramVersion(r.read_u32(), r.read_u32(), r.read_u32())
        pc.min_compat_version = ProgramVersion(r.read_u32(), r.read_u32(), r.read_u32())
        return pc


@dataclass
class Contributor:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    status: int = 0
    code: str = ""
    reference_count: int = 0
    ops_manager_pk: Pubkey = Pubkey.default()

    @classmethod
    def from_bytes(cls, data: bytes) -> Contributor:
        r = BorshReader(data)
        c = cls()
        c.account_type = r.read_u8()
        c.owner = r.read_pubkey()
        c.index = r.read_u128()
        c.bump_seed = r.read_u8()
        c.status = r.read_u8()
        c.code = r.read_string()
        c.reference_count = r.read_u32()
        c.ops_manager_pk = r.read_pubkey()
        return c


@dataclass
class AccessPass:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    bump_seed: int = 0
    access_pass_type_tag: int = 0
    validator_pub_key: Pubkey | None = None
    client_ip: bytes = b"\x00" * 4
    user_payer: Pubkey = Pubkey.default()
    last_access_epoch: int = 0
    connection_count: int = 0
    status: int = 0
    mgroup_pub_allowlist: list[Pubkey] = field(default_factory=list)
    mgroup_sub_allowlist: list[Pubkey] = field(default_factory=list)
    flags: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> AccessPass:
        r = BorshReader(data)
        ap = cls()
        ap.account_type = r.read_u8()
        ap.owner = r.read_pubkey()
        ap.bump_seed = r.read_u8()
        ap.access_pass_type_tag = r.read_u8()
        if ap.access_pass_type_tag == 1:  # SolanaValidator
            ap.validator_pub_key = r.read_pubkey()
        ap.client_ip = r.read_ipv4()
        ap.user_payer = r.read_pubkey()
        ap.last_access_epoch = r.read_u64()
        ap.connection_count = r.read_u16()
        ap.status = r.read_u8()
        ap.mgroup_pub_allowlist = r.read_pubkey_vec()
        ap.mgroup_sub_allowlist = r.read_pubkey_vec()
        ap.flags = r.read_u8()
        return ap
