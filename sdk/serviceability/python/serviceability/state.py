"""On-chain account data structures for the serviceability program.

Binary layout uses Borsh serialization with a 1-byte AccountType discriminator
as the first byte. Deserialization uses cursor-based DefensiveReader from
borsh_incremental which returns defaults on missing data.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import IntEnum

from borsh_incremental import DefensiveReader
from solders.pubkey import Pubkey  # type: ignore[import-untyped]


def _read_pubkey(r: DefensiveReader) -> Pubkey:
    return Pubkey.from_bytes(r.read_pubkey_raw())


def _read_pubkey_vec(r: DefensiveReader) -> list[Pubkey]:
    raw = r.read_pubkey_raw_vec()
    return [Pubkey.from_bytes(b) for b in raw]


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
    TENANT = 13


# ---------------------------------------------------------------------------
# Status / type enums
# ---------------------------------------------------------------------------


class LocationStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    SUSPENDED = 2

    def __str__(self) -> str:
        _names = {0: "pending", 1: "activated", 2: "suspended"}
        return _names.get(self.value, "unknown")


class ExchangeStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    SUSPENDED = 2

    def __str__(self) -> str:
        _names = {0: "pending", 1: "activated", 2: "suspended"}
        return _names.get(self.value, "unknown")


class DeviceDeviceType(IntEnum):
    HYBRID = 0
    TRANSIT = 1
    EDGE = 2

    def __str__(self) -> str:
        _names = {0: "hybrid", 1: "transit", 2: "edge"}
        return _names.get(self.value, "unknown")


class DeviceStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    DELETING = 2
    REJECTED = 3
    DRAINED = 4
    DEVICE_PROVISIONING = 5
    LINK_PROVISIONING = 6

    def __str__(self) -> str:
        _names = {
            0: "pending",
            1: "activated",
            2: "deleting",
            3: "rejected",
            4: "drained",
            5: "device-provisioning",
            6: "link-provisioning",
        }
        return _names.get(self.value, "unknown")


class DeviceHealth(IntEnum):
    UNKNOWN = 0
    PENDING = 1
    READY_FOR_LINKS = 2
    READY_FOR_USERS = 3
    IMPAIRED = 4

    def __str__(self) -> str:
        _names = {
            0: "unknown",
            1: "pending",
            2: "ready_for_links",
            3: "ready_for_users",
            4: "impaired",
        }
        return _names.get(self.value, "unknown")


class DeviceDesiredStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    DRAINED = 6

    def __str__(self) -> str:
        _names = {0: "pending", 1: "activated", 6: "drained"}
        return _names.get(self.value, "unknown")


class InterfaceStatus(IntEnum):
    INVALID = 0
    UNMANAGED = 1
    PENDING = 2
    ACTIVATED = 3
    DELETING = 4
    REJECTING = 5
    UNLINKED = 6

    def __str__(self) -> str:
        _names = {
            0: "invalid",
            1: "unmanaged",
            2: "pending",
            3: "activated",
            4: "deleting",
            5: "rejecting",
            6: "unlinked",
        }
        return _names.get(self.value, "unknown")


class InterfaceType(IntEnum):
    INVALID = 0
    LOOPBACK = 1
    PHYSICAL = 2

    def __str__(self) -> str:
        _names = {0: "invalid", 1: "loopback", 2: "physical"}
        return _names.get(self.value, "unknown")


class LoopbackType(IntEnum):
    NONE = 0
    VPNV4 = 1
    IPV4 = 2
    PIM_RP_ADDR = 3
    RESERVED = 4

    def __str__(self) -> str:
        _names = {0: "none", 1: "vpnv4", 2: "ipv4", 3: "pim_rp_addr", 4: "reserved"}
        return _names.get(self.value, "unknown")


class InterfaceCYOA(IntEnum):
    NONE = 0
    GRE_OVER_DIA = 1
    GRE_OVER_FABRIC = 2
    GRE_OVER_PRIVATE_PEER = 3
    GRE_OVER_PUBLIC_PEER = 4
    GRE_OVER_CABLE = 5

    def __str__(self) -> str:
        _names = {
            0: "none",
            1: "gre_over_dia",
            2: "gre_over_fabric",
            3: "gre_over_private_peering",
            4: "gre_over_public_peering",
            5: "gre_over_cable",
        }
        return _names.get(self.value, "unknown")


class InterfaceDIA(IntEnum):
    NONE = 0
    DIA = 1

    def __str__(self) -> str:
        _names = {0: "none", 1: "dia"}
        return _names.get(self.value, "unknown")


class RoutingMode(IntEnum):
    STATIC = 0
    BGP = 1

    def __str__(self) -> str:
        _names = {0: "static", 1: "bgp"}
        return _names.get(self.value, "unknown")


class LinkLinkType(IntEnum):
    WAN = 1
    DZX = 127

    def __str__(self) -> str:
        _names = {1: "WAN", 127: "DZX"}
        return _names.get(self.value, "")


class LinkStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    DELETING = 3
    REJECTED = 4
    REQUESTED = 5
    HARD_DRAINED = 6
    SOFT_DRAINED = 7
    PROVISIONING = 8

    def __str__(self) -> str:
        _names = {
            0: "pending",
            1: "activated",
            3: "deleting",
            4: "rejected",
            5: "requested",
            6: "hard-drained",
            7: "soft-drained",
            8: "provisioning",
        }
        return _names.get(self.value, "unknown")


class LinkHealth(IntEnum):
    UNKNOWN = 0
    PENDING = 1
    READY_FOR_SERVICE = 2
    IMPAIRED = 3

    def __str__(self) -> str:
        _names = {0: "unknown", 1: "pending", 2: "ready_for_service", 3: "impaired"}
        return _names.get(self.value, "unknown")


class LinkDesiredStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    HARD_DRAINED = 6
    SOFT_DRAINED = 7

    def __str__(self) -> str:
        _names = {0: "pending", 1: "activated", 6: "hard-drained", 7: "soft-drained"}
        return _names.get(self.value, "unknown")


class ContributorStatus(IntEnum):
    NONE = 0
    ACTIVATED = 1
    SUSPENDED = 2
    DELETING = 3

    def __str__(self) -> str:
        _names = {0: "none", 1: "activated", 2: "suspended", 3: "deleting"}
        return _names.get(self.value, "unknown")


class UserUserType(IntEnum):
    IBRL = 0
    IBRL_WITH_ALLOC_IP = 1
    EDGE_FILTERING = 2
    MULTICAST = 3

    def __str__(self) -> str:
        _names = {
            0: "ibrl",
            1: "ibrl_with_allocated_ip",
            2: "edge_filtering",
            3: "multicast",
        }
        return _names.get(self.value, "unknown")


class CyoaType(IntEnum):
    NONE = 0
    GRE_OVER_DIA = 1
    GRE_OVER_FABRIC = 2
    GRE_OVER_PRIVATE_PEER = 3
    GRE_OVER_PUBLIC_PEER = 4
    GRE_OVER_CABLE = 5

    def __str__(self) -> str:
        _names = {
            0: "none",
            1: "gre_over_dia",
            2: "gre_over_fabric",
            3: "gre_over_private_peering",
            4: "gre_over_public_peering",
            5: "gre_over_cable",
        }
        return _names.get(self.value, "unknown")


class UserStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    DELETING = 3
    REJECTED = 4
    PENDING_BAN = 5
    BANNED = 6
    UPDATING = 7
    OUT_OF_CREDITS = 8

    def __str__(self) -> str:
        _names = {
            0: "pending",
            1: "activated",
            3: "deleting",
            4: "rejected",
            5: "pending_ban",
            6: "banned",
            7: "updating",
            8: "out_of_credits",
        }
        return _names.get(self.value, "unknown")


class MulticastGroupStatus(IntEnum):
    PENDING = 0
    ACTIVATED = 1
    SUSPENDED = 2
    DELETING = 3
    REJECTED = 4

    def __str__(self) -> str:
        _names = {
            0: "pending",
            1: "activated",
            2: "suspended",
            3: "deleting",
            4: "rejected",
        }
        return _names.get(self.value, "unknown")


class AccessPassTypeTag(IntEnum):
    PREPAID = 0
    SOLANA_VALIDATOR = 1
    SOLANA_RPC = 2
    SOLANA_MULTICAST_PUBLISHER = 3
    SOLANA_MULTICAST_SUBSCRIBER = 4
    OTHERS = 5

    def __str__(self) -> str:
        _names = {
            0: "prepaid",
            1: "solana_validator",
            2: "solana_rpc",
            3: "solana_multicast_publisher",
            4: "solana_multicast_subscriber",
            5: "others",
        }
        return _names.get(self.value, "unknown")


class AccessPassStatus(IntEnum):
    REQUESTED = 0
    CONNECTED = 1
    DISCONNECTED = 2
    EXPIRED = 3

    def __str__(self) -> str:
        _names = {0: "requested", 1: "connected", 2: "disconnected", 3: "expired"}
        return _names.get(self.value, "unknown")


# ---------------------------------------------------------------------------
# Account dataclasses
# ---------------------------------------------------------------------------

CURRENT_INTERFACE_VERSION = 2


@dataclass
class Interface:
    version: int = 0
    status: InterfaceStatus = InterfaceStatus.INVALID
    name: str = ""
    interface_type: InterfaceType = InterfaceType.INVALID
    interface_cyoa: InterfaceCYOA = InterfaceCYOA.NONE
    interface_dia: InterfaceDIA = InterfaceDIA.NONE
    loopback_type: LoopbackType = LoopbackType.NONE
    bandwidth: int = 0
    cir: int = 0
    mtu: int = 0
    routing_mode: RoutingMode = RoutingMode.STATIC
    vlan_id: int = 0
    ip_net: bytes = b"\x00" * 5
    node_segment_idx: int = 0
    user_tunnel_endpoint: bool = False

    @classmethod
    def from_reader(cls, r: IncrementalReader) -> Interface:
        iface = cls()
        iface.version = r.read_u8()
        if iface.version > CURRENT_INTERFACE_VERSION - 1:
            return iface
        if iface.version == 0:  # V1
            iface.status = InterfaceStatus(r.read_u8())
            iface.name = r.read_string()
            iface.interface_type = InterfaceType(r.read_u8())
            iface.loopback_type = LoopbackType(r.read_u8())
            iface.vlan_id = r.read_u16()
            iface.ip_net = r.read_network_v4()
            iface.node_segment_idx = r.read_u16()
            iface.user_tunnel_endpoint = r.read_bool()
        elif iface.version == 1:  # V2
            iface.status = InterfaceStatus(r.read_u8())
            iface.name = r.read_string()
            iface.interface_type = InterfaceType(r.read_u8())
            iface.interface_cyoa = InterfaceCYOA(r.read_u8())
            iface.interface_dia = InterfaceDIA(r.read_u8())
            iface.loopback_type = LoopbackType(r.read_u8())
            iface.bandwidth = r.read_u64()
            iface.cir = r.read_u64()
            iface.mtu = r.read_u16()
            iface.routing_mode = RoutingMode(r.read_u8())
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
    feature_flags: int = 0
    reservation_authority_pk: Pubkey = Pubkey.default()

    @classmethod
    def from_bytes(cls, data: bytes) -> GlobalState:
        r = DefensiveReader(data)
        gs = cls()
        gs.account_type = r.read_u8()
        gs.bump_seed = r.read_u8()
        gs.account_index = r.read_u128()
        gs.foundation_allowlist = _read_pubkey_vec(r)
        _read_pubkey_vec(r)  # deprecated device_allowlist
        _read_pubkey_vec(r)  # deprecated user_allowlist
        gs.activator_authority_pk = _read_pubkey(r)
        gs.sentinel_authority_pk = _read_pubkey(r)
        gs.contributor_airdrop_lamports = r.read_u64()
        gs.user_airdrop_lamports = r.read_u64()
        gs.health_oracle_pk = _read_pubkey(r)
        gs.qa_allowlist = _read_pubkey_vec(r)
        gs.feature_flags = r.read_u128()
        gs.reservation_authority_pk = _read_pubkey(r)
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
    multicast_publisher_block: bytes = b"\x00" * 5

    @classmethod
    def from_bytes(cls, data: bytes) -> GlobalConfig:
        r = DefensiveReader(data)
        gc = cls()
        gc.account_type = r.read_u8()
        gc.owner = _read_pubkey(r)
        gc.bump_seed = r.read_u8()
        gc.local_asn = r.read_u32()
        gc.remote_asn = r.read_u32()
        gc.device_tunnel_block = r.read_network_v4()
        gc.user_tunnel_block = r.read_network_v4()
        gc.multicast_group_block = r.read_network_v4()
        gc.next_bgp_community = r.read_u16()
        gc.multicast_publisher_block = r.read_network_v4()
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
    status: LocationStatus = LocationStatus.PENDING
    code: str = ""
    name: str = ""
    country: str = ""
    reference_count: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> Location:
        r = DefensiveReader(data)
        loc = cls()
        loc.account_type = r.read_u8()
        loc.owner = _read_pubkey(r)
        loc.index = r.read_u128()
        loc.bump_seed = r.read_u8()
        loc.lat = r.read_f64()
        loc.lng = r.read_f64()
        loc.loc_id = r.read_u32()
        loc.status = LocationStatus(r.read_u8())
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
    status: ExchangeStatus = ExchangeStatus.PENDING
    code: str = ""
    name: str = ""
    reference_count: int = 0
    device1_pk: Pubkey = Pubkey.default()
    device2_pk: Pubkey = Pubkey.default()

    @classmethod
    def from_bytes(cls, data: bytes) -> Exchange:
        r = DefensiveReader(data)
        ex = cls()
        ex.account_type = r.read_u8()
        ex.owner = _read_pubkey(r)
        ex.index = r.read_u128()
        ex.bump_seed = r.read_u8()
        ex.lat = r.read_f64()
        ex.lng = r.read_f64()
        ex.bgp_community = r.read_u16()
        r.read_u16()  # reserved padding
        ex.status = ExchangeStatus(r.read_u8())
        ex.code = r.read_string()
        ex.name = r.read_string()
        ex.reference_count = r.read_u32()
        ex.device1_pk = _read_pubkey(r)
        ex.device2_pk = _read_pubkey(r)
        return ex


@dataclass
class Device:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    location_pub_key: Pubkey = Pubkey.default()
    exchange_pub_key: Pubkey = Pubkey.default()
    device_type: DeviceDeviceType = DeviceDeviceType.HYBRID
    public_ip: bytes = b"\x00" * 4
    status: DeviceStatus = DeviceStatus.PENDING
    code: str = ""
    dz_prefixes: list[bytes] = field(default_factory=list)
    metrics_publisher_pub_key: Pubkey = Pubkey.default()
    contributor_pub_key: Pubkey = Pubkey.default()
    mgmt_vrf: str = ""
    interfaces: list[Interface] = field(default_factory=list)
    reference_count: int = 0
    users_count: int = 0
    max_users: int = 0
    device_health: DeviceHealth = DeviceHealth.UNKNOWN
    device_desired_status: DeviceDesiredStatus = DeviceDesiredStatus.PENDING
    unicast_users_count: int = 0
    multicast_users_count: int = 0
    max_unicast_users: int = 0
    max_multicast_users: int = 0
    reserved_seats: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> Device:
        r = DefensiveReader(data)
        dev = cls()
        dev.account_type = r.read_u8()
        dev.owner = _read_pubkey(r)
        dev.index = r.read_u128()
        dev.bump_seed = r.read_u8()
        dev.location_pub_key = _read_pubkey(r)
        dev.exchange_pub_key = _read_pubkey(r)
        dev.device_type = DeviceDeviceType(r.read_u8())
        dev.public_ip = r.read_ipv4()
        dev.status = DeviceStatus(r.read_u8())
        dev.code = r.read_string()
        dev.dz_prefixes = r.read_network_v4_vec()
        dev.metrics_publisher_pub_key = _read_pubkey(r)
        dev.contributor_pub_key = _read_pubkey(r)
        dev.mgmt_vrf = r.read_string()
        iface_len = r.read_u32()
        dev.interfaces = [Interface.from_reader(r) for _ in range(iface_len)]
        dev.reference_count = r.read_u32()
        dev.users_count = r.read_u16()
        dev.max_users = r.read_u16()
        dev.device_health = DeviceHealth(r.read_u8())
        dev.device_desired_status = DeviceDesiredStatus(r.read_u8())
        dev.unicast_users_count = r.read_u16()
        dev.multicast_users_count = r.read_u16()
        dev.max_unicast_users = r.read_u16()
        dev.max_multicast_users = r.read_u16()
        dev.reserved_seats = r.read_u16()
        return dev


@dataclass
class Link:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    side_a_pub_key: Pubkey = Pubkey.default()
    side_z_pub_key: Pubkey = Pubkey.default()
    link_type: LinkLinkType = LinkLinkType.WAN
    bandwidth: int = 0
    mtu: int = 0
    delay_ns: int = 0
    jitter_ns: int = 0
    tunnel_id: int = 0
    tunnel_net: bytes = b"\x00" * 5
    status: LinkStatus = LinkStatus.PENDING
    code: str = ""
    contributor_pub_key: Pubkey = Pubkey.default()
    side_a_iface_name: str = ""
    side_z_iface_name: str = ""
    delay_override_ns: int = 0
    link_health: LinkHealth = LinkHealth.UNKNOWN
    link_desired_status: LinkDesiredStatus = LinkDesiredStatus.PENDING

    @classmethod
    def from_bytes(cls, data: bytes) -> Link:
        r = DefensiveReader(data)
        lk = cls()
        lk.account_type = r.read_u8()
        lk.owner = _read_pubkey(r)
        lk.index = r.read_u128()
        lk.bump_seed = r.read_u8()
        lk.side_a_pub_key = _read_pubkey(r)
        lk.side_z_pub_key = _read_pubkey(r)
        lk.link_type = LinkLinkType(r.read_u8())
        lk.bandwidth = r.read_u64()
        lk.mtu = r.read_u32()
        lk.delay_ns = r.read_u64()
        lk.jitter_ns = r.read_u64()
        lk.tunnel_id = r.read_u16()
        lk.tunnel_net = r.read_network_v4()
        lk.status = LinkStatus(r.read_u8())
        lk.code = r.read_string()
        lk.contributor_pub_key = _read_pubkey(r)
        lk.side_a_iface_name = r.read_string()
        lk.side_z_iface_name = r.read_string()
        lk.delay_override_ns = r.read_u64()
        lk.link_health = LinkHealth(r.read_u8())
        lk.link_desired_status = LinkDesiredStatus(r.read_u8())
        return lk


@dataclass
class User:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    index: int = 0
    bump_seed: int = 0
    user_type: UserUserType = UserUserType.IBRL
    tenant_pub_key: Pubkey = Pubkey.default()
    device_pub_key: Pubkey = Pubkey.default()
    cyoa_type: CyoaType = CyoaType.NONE
    client_ip: bytes = b"\x00" * 4
    dz_ip: bytes = b"\x00" * 4
    tunnel_id: int = 0
    tunnel_net: bytes = b"\x00" * 5
    status: UserStatus = UserStatus.PENDING
    publishers: list[Pubkey] = field(default_factory=list)
    subscribers: list[Pubkey] = field(default_factory=list)
    validator_pub_key: Pubkey = Pubkey.default()

    @classmethod
    def from_bytes(cls, data: bytes) -> User:
        r = DefensiveReader(data)
        u = cls()
        u.account_type = r.read_u8()
        u.owner = _read_pubkey(r)
        u.index = r.read_u128()
        u.bump_seed = r.read_u8()
        u.user_type = UserUserType(r.read_u8())
        u.tenant_pub_key = _read_pubkey(r)
        u.device_pub_key = _read_pubkey(r)
        u.cyoa_type = CyoaType(r.read_u8())
        u.client_ip = r.read_ipv4()
        u.dz_ip = r.read_ipv4()
        u.tunnel_id = r.read_u16()
        u.tunnel_net = r.read_network_v4()
        u.status = UserStatus(r.read_u8())
        u.publishers = _read_pubkey_vec(r)
        u.subscribers = _read_pubkey_vec(r)
        u.validator_pub_key = _read_pubkey(r)
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
    status: MulticastGroupStatus = MulticastGroupStatus.PENDING
    code: str = ""
    publisher_count: int = 0
    subscriber_count: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> MulticastGroup:
        r = DefensiveReader(data)
        mg = cls()
        mg.account_type = r.read_u8()
        mg.owner = _read_pubkey(r)
        mg.index = r.read_u128()
        mg.bump_seed = r.read_u8()
        mg.tenant_pub_key = _read_pubkey(r)
        mg.multicast_ip = r.read_ipv4()
        mg.max_bandwidth = r.read_u64()
        mg.status = MulticastGroupStatus(r.read_u8())
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
        r = DefensiveReader(data)
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
    status: ContributorStatus = ContributorStatus.NONE
    code: str = ""
    reference_count: int = 0
    ops_manager_pk: Pubkey = Pubkey.default()

    @classmethod
    def from_bytes(cls, data: bytes) -> Contributor:
        r = DefensiveReader(data)
        c = cls()
        c.account_type = r.read_u8()
        c.owner = _read_pubkey(r)
        c.index = r.read_u128()
        c.bump_seed = r.read_u8()
        c.status = ContributorStatus(r.read_u8())
        c.code = r.read_string()
        c.reference_count = r.read_u32()
        c.ops_manager_pk = _read_pubkey(r)
        return c


class TenantPaymentStatus(IntEnum):
    DELINQUENT = 0
    PAID = 1

    def __str__(self) -> str:
        _names = {0: "delinquent", 1: "paid"}
        return _names.get(self.value, "unknown")


@dataclass
class Tenant:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    bump_seed: int = 0
    code: str = ""
    vrf_id: int = 0
    reference_count: int = 0
    administrators: list[Pubkey] = field(default_factory=list)
    payment_status: int = 0
    token_account: Pubkey = Pubkey.default()
    metro_routing: bool = False
    route_liveness: bool = False
    billing_discriminant: int = 0
    billing_rate: int = 0
    billing_last_deduction_dz_epoch: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> Tenant:
        r = DefensiveReader(data)
        t = cls()
        t.account_type = r.read_u8()
        t.owner = _read_pubkey(r)
        t.bump_seed = r.read_u8()
        t.code = r.read_string()
        t.vrf_id = r.read_u16()
        t.reference_count = r.read_u32()
        t.administrators = _read_pubkey_vec(r)
        t.payment_status = r.read_u8()
        t.token_account = _read_pubkey(r)
        t.metro_routing = r.read_u8() != 0
        t.route_liveness = r.read_u8() != 0
        t.billing_discriminant = r.read_u8()
        t.billing_rate = r.read_u64()
        t.billing_last_deduction_dz_epoch = r.read_u64()
        return t


@dataclass
class AccessPass:
    account_type: int = 0
    owner: Pubkey = Pubkey.default()
    bump_seed: int = 0
    access_pass_type_tag: AccessPassTypeTag = AccessPassTypeTag.PREPAID
    associated_pubkey: Pubkey | None = None  # for SolanaValidator, SolanaRPC, SolanaMulticast*
    others_type_name: str = ""  # for Others variant
    others_key: str = ""  # for Others variant
    client_ip: bytes = b"\x00" * 4
    user_payer: Pubkey = Pubkey.default()
    last_access_epoch: int = 0
    connection_count: int = 0
    status: AccessPassStatus = AccessPassStatus.REQUESTED
    mgroup_pub_allowlist: list[Pubkey] = field(default_factory=list)
    mgroup_sub_allowlist: list[Pubkey] = field(default_factory=list)
    flags: int = 0

    @classmethod
    def from_bytes(cls, data: bytes) -> AccessPass:
        r = DefensiveReader(data)
        ap = cls()
        ap.account_type = r.read_u8()
        ap.owner = _read_pubkey(r)
        ap.bump_seed = r.read_u8()
        tag = r.read_u8()
        try:
            ap.access_pass_type_tag = AccessPassTypeTag(tag)
        except ValueError:
            ap.access_pass_type_tag = AccessPassTypeTag.PREPAID
        # Variants 1-4 have an associated pubkey
        if tag in (1, 2, 3, 4):
            ap.associated_pubkey = _read_pubkey(r)
        # Variant 5 (Others) has two strings
        elif tag == 5:
            ap.others_type_name = r.read_string()
            ap.others_key = r.read_string()
        ap.client_ip = r.read_ipv4()
        ap.user_payer = _read_pubkey(r)
        ap.last_access_epoch = r.read_u64()
        ap.connection_count = r.read_u16()
        ap.status = AccessPassStatus(r.read_u8())
        ap.mgroup_pub_allowlist = _read_pubkey_vec(r)
        ap.mgroup_sub_allowlist = _read_pubkey_vec(r)
        ap.flags = r.read_u8()
        return ap
