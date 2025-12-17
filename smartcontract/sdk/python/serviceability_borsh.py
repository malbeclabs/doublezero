# serviceability_borsh.py
from __future__ import annotations
from dataclasses import dataclass
from enum import IntEnum
from typing import List, Optional, Protocol, Sequence, Tuple
import io
import struct

from borsh_construct import U8, U16, U32, U64, F64, Vec
from construct import Struct as CStruct, Switch, this, Bytes as CBytes
from construct.core import Construct, ConstructError

Bytes32 = bytes

class AccountType(IntEnum):
  GlobalState=1; ConfigType=2; LocationType=3; ExchangeType=4; DeviceType=5; LinkType=6
  UserType=7; MulticastGroupType=8; ProgramConfigType=9; ContributorType=10; AccessPassType=11

CURRENT_INTERFACE_VERSION = 2

Pubkey = CBytes(32)
IPv4   = CBytes(4)
NetV4  = CBytes(5)

Uint128 = CStruct("high" / U64, "low" / U64)

def netv4_to_str(n5: bytes) -> str:
  if len(n5) != 5: return ""
  plen = n5[4]
  if plen == 0 or plen > 32: return ""
  import ipaddress
  return f"{ipaddress.IPv4Address(n5[:4])}/{plen}"

# -------- lenient stream + safe variable-length constructs --------

class ZeroPadBytesIO(io.BytesIO):
  # Pads short fixed-size reads with zeros, so fixed-size fields don't raise StreamError.
  def read(self, n=-1):
    b = super().read(n)
    if n is None or n < 0:
      return b
    if len(b) < n:
      b += b"\x00" * (n - len(b))
    return b

class SafeBorshString(Construct):
  # u32 little-endian length + bytes; if length > remaining, returns "" (and consumes remaining).
  def _parse(self, stream, context, path):
    raw_len = stream.read(4)
    if len(raw_len) != 4:
      return ""
    (n,) = struct.unpack("<I", raw_len)
    if n == 0:
      return ""
    # remaining bytes in the underlying buffer (not the padded view)
    pos = stream.tell()
    end = len(stream.getbuffer())
    remaining = max(0, end - pos)
    if n > remaining:
      # consume whatever remains and return empty (Go's ReadString() returns "" on bounds fail)
      stream.seek(end)
      return ""
    data = stream.read(n)
    return data.decode("utf-8", "replace")

  def _build(self, obj, stream, context, path):
    raise NotImplementedError("read-only")

  def _sizeof(self, context, path):
    raise NotImplementedError("variable")

SafeString = SafeBorshString()

class SafeFixedVec(Construct):
  # u32 len + len * elem_size bytes, parsed as elem_parser on each chunk.
  # If not enough bytes for all elems, returns [] (and does not attempt to read elems).
  def __init__(self, elem_size: int, elem_parser: Construct):
    super().__init__()
    self.elem_size = elem_size
    self.elem_parser = elem_parser

  def _parse(self, stream, context, path):
    raw_len = stream.read(4)
    if len(raw_len) != 4:
      return []
    (n,) = struct.unpack("<I", raw_len)
    if n == 0:
      return []
    pos = stream.tell()
    end = len(stream.getbuffer())
    remaining = max(0, end - pos)
    need = n * self.elem_size
    if need > remaining:
      return []
    out = []
    for _ in range(n):
      out.append(self.elem_parser.parse_stream(stream))
    return out

  def _build(self, obj, stream, context, path):
    raise NotImplementedError("read-only")

  def _sizeof(self, context, path):
    raise NotImplementedError("variable")

SafePubkeyVec = SafeFixedVec(32, Pubkey)
SafeNetV4Vec  = SafeFixedVec(5, NetV4)

def parse_lenient(schema: Construct, data: bytes) -> dict:
  # Generic lenient parse for any schema composed of:
  # - fixed-size primitives (now safe due to ZeroPadBytesIO)
  # - SafeString / SafeFixedVec for variable-length
  return schema.parse_stream(ZeroPadBytesIO(data))

# -------- schemas (mirror your Go Deserialize* order) --------

ConfigSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "bump_seed" / U8,
  "local_asn" / U32,
  "remote_asn" / U32,
  "tunnel_tunnel_block" / NetV4,
  "user_tunnel_block" / NetV4,
  "multicast_group_block" / NetV4,
  "pubkey" / Pubkey,
)

LocationSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "index" / Uint128,
  "bump_seed" / U8,
  "lat" / F64,
  "lng" / F64,
  "loc_id" / U32,
  "status" / U8,
  "code" / SafeString,
  "name" / SafeString,
  "country" / SafeString,
  "pubkey" / Pubkey,
)

ExchangeSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "index" / Uint128,
  "bump_seed" / U8,
  "lat" / F64,
  "lng" / F64,
  "bgp_community" / U16,
  "_unused_padding" / U16,
  "status" / U8,
  "code" / SafeString,
  "name" / SafeString,
  "ref_count" / U32,
  "device1_pk" / Pubkey,
  "device2_pk" / Pubkey,
)

ContributorSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "index" / Uint128,
  "bump_seed" / U8,
  "status" / U8,
  "code" / SafeString,
  "name" / SafeString,
  "pubkey" / Pubkey,
)

InterfaceV1 = CStruct(
  "status" / U8,
  "name" / SafeString,
  "interface_type" / U8,
  "loopback_type" / U8,
  "vlan_id" / U16,
  "ip_net" / NetV4,
  "node_segment_idx" / U16,
  "user_tunnel_endpoint" / U8,
)

InterfaceV2 = CStruct(
  "status" / U8,
  "name" / SafeString,
  "interface_type" / U8,
  "interface_cyoa" / U8,
  "interface_dia" / U8,
  "loopback_type" / U8,
  "bandwidth" / U64,
  "cir" / U64,
  "mtu" / U16,
  "routing_mode" / U8,
  "vlan_id" / U16,
  "ip_net" / NetV4,
  "node_segment_idx" / U16,
  "user_tunnel_endpoint" / U8,
)

InterfaceSchema = CStruct(
  "version" / U8,
  "body" / Switch(this.version, {0: InterfaceV1, 1: InterfaceV2}, default=CStruct()),
)

DeviceSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "index" / Uint128,
  "bump_seed" / U8,
  "location_pubkey" / Pubkey,
  "exchange_pubkey" / Pubkey,
  "device_type" / U8,
  "public_ip" / IPv4,
  "status" / U8,
  "code" / SafeString,
  "dz_prefixes" / SafeNetV4Vec,              # <- lenient fixed vec
  "metrics_publisher_pubkey" / Pubkey,
  "contributor_pubkey" / Pubkey,
  "mgmt_vrf" / SafeString,
  "interfaces" / Vec(InterfaceSchema),       # variable-length elems; still strict-ish, but strings are safe
  "reference_count" / U32,
  "users_count" / U16,
  "max_users" / U16,
)

LinkSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "index" / Uint128,
  "bump_seed" / U8,
  "side_a_pubkey" / Pubkey,
  "side_z_pubkey" / Pubkey,
  "link_type" / U8,
  "bandwidth" / U64,
  "mtu" / U32,
  "delay_ns" / U64,
  "jitter_ns" / U64,
  "tunnel_id" / U16,
  "tunnel_net" / NetV4,
  "status" / U8,
  "code" / SafeString,
  "contributor_pubkey" / Pubkey,
  "side_a_iface_name" / SafeString,
  "side_z_iface_name" / SafeString,
  "delay_override_ns" / U64,
  "pubkey" / Pubkey,
)

UserSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "index" / Uint128,
  "bump_seed" / U8,
  "user_type" / U8,
  "tenant_pubkey" / Pubkey,
  "device_pubkey" / Pubkey,
  "cyoa_type" / U8,
  "client_ip" / IPv4,
  "dz_ip" / IPv4,
  "tunnel_id" / U16,
  "tunnel_net" / NetV4,
  "status" / U8,
  "publishers" / SafePubkeyVec,              # <- lenient fixed vec
  "subscribers" / SafePubkeyVec,             # <- lenient fixed vec
  "validator_pubkey" / Pubkey,               # <- padded to zeros if missing
  "pubkey" / Pubkey,                         # <- padded to zeros if missing
)

MulticastGroupSchema = CStruct(
  "account_type" / U8,
  "owner" / Pubkey,
  "index" / Uint128,
  "bump_seed" / U8,
  "tenant_pubkey" / Pubkey,
  "multicast_ip" / IPv4,
  "max_bandwidth" / U64,
  "status" / U8,
  "code" / SafeString,
  "pubkey" / Pubkey,
)

ProgramVersionSchema = CStruct("major" / U32, "minor" / U32, "patch" / U32)
ProgramConfigSchema = CStruct(
  "account_type" / U8,
  "bump_seed" / U8,
  "version" / ProgramVersionSchema,
)

# -------- client / loader --------

@dataclass
class ProgramData:
  config: Optional[dict] = None
  locations: List[dict] = None
  exchanges: List[dict] = None
  contributors: List[dict] = None
  devices: List[dict] = None
  links: List[dict] = None
  users: List[dict] = None
  multicast_groups: List[dict] = None
  program_config: Optional[dict] = None
  parse_errors: List[tuple] = None  # (acct_pubkey_bytes32, account_type_byte, data_len, error_str)

  def __post_init__(self):
    self.locations = self.locations or []
    self.exchanges = self.exchanges or []
    self.contributors = self.contributors or []
    self.devices = self.devices or []
    self.links = self.links or []
    self.users = self.users or []
    self.multicast_groups = self.multicast_groups or []
    self.parse_errors = self.parse_errors or []

class RPCClient(Protocol):
  async def get_program_accounts(self, program_id: Bytes32) -> Sequence[Tuple[Bytes32, bytes]]: ...

class Client:
  def __init__(self, rpc: RPCClient, program_id: Bytes32):
    if len(program_id) != 32: raise ValueError("program_id must be 32 bytes")
    self.rpc = rpc
    self.program_id = program_id

  async def get_program_data(self) -> ProgramData:
    out = await self.rpc.get_program_accounts(self.program_id)
    pd = ProgramData()

    for acct_pubkey, data in out:
      if not data: continue
      t = data[0]
      try:
        if t == int(AccountType.ConfigType):
          x = parse_lenient(ConfigSchema, data); x["pubkey_account"] = acct_pubkey; pd.config = x
        elif t == int(AccountType.LocationType):
          x = parse_lenient(LocationSchema, data); x["pubkey_account"] = acct_pubkey; pd.locations.append(x)
        elif t == int(AccountType.ExchangeType):
          x = parse_lenient(ExchangeSchema, data); x["pubkey_account"] = acct_pubkey; pd.exchanges.append(x)
        elif t == int(AccountType.ContributorType):
          x = parse_lenient(ContributorSchema, data); x["pubkey_account"] = acct_pubkey; pd.contributors.append(x)
        elif t == int(AccountType.DeviceType):
          x = parse_lenient(DeviceSchema, data); x["pubkey_account"] = acct_pubkey; pd.devices.append(x)
        elif t == int(AccountType.LinkType):
          x = parse_lenient(LinkSchema, data); x["pubkey_account"] = acct_pubkey; pd.links.append(x)
        elif t == int(AccountType.UserType):
          x = parse_lenient(UserSchema, data); x["pubkey_account"] = acct_pubkey; pd.users.append(x)
        elif t == int(AccountType.MulticastGroupType):
          x = parse_lenient(MulticastGroupSchema, data); x["pubkey_account"] = acct_pubkey; pd.multicast_groups.append(x)
        elif t == int(AccountType.ProgramConfigType):
          x = parse_lenient(ProgramConfigSchema, data); pd.program_config = x
      except ConstructError as e:
        pd.parse_errors.append((acct_pubkey, t, len(data), str(e)))
        continue

    return pd

def pretty_pubkey(pk32: bytes) -> str:
  import base58
  return base58.b58encode(pk32).decode("ascii")
