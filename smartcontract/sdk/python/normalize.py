# normalize.py
import base58, base64, ipaddress
from typing import Any, Callable, Dict, Tuple

def _pk(x):
  if isinstance(x, str): return x
  return base58.b58encode(bytes(x)).decode("ascii")

def _ip4(x):
  if isinstance(x, str): return x
  return str(ipaddress.IPv4Address(bytes(x)))

def _cidr(x):
  if isinstance(x, str): return x
  from serviceability_borsh import netv4_to_str
  return netv4_to_str(bytes(x))

def _bytes_default(b: bytes) -> str:
  return "base64:" + base64.b64encode(b).decode("ascii")

def _clean_and_convert(x: Any):
  if hasattr(x, "items"):
    return {k: _clean_and_convert(v) for k,v in x.items() if not str(k).startswith("_")}
  if isinstance(x, dict):
    return {k: _clean_and_convert(v) for k,v in x.items() if not str(k).startswith("_")}
  if isinstance(x, list):
    return [_clean_and_convert(v) for v in x]
  if isinstance(x, tuple):
    return [_clean_and_convert(v) for v in x]
  if isinstance(x, (bytes, bytearray)):
    b = bytes(x)
    if len(b) == 32: return _pk(b)
    if len(b) == 4:  return _ip4(b)
    if len(b) == 5:  return _cidr(b)
    return _bytes_default(b)
  return x

def _drop_fields_recursive(x: Any, drop: set):
  if isinstance(x, dict):
    return {k: _drop_fields_recursive(v, drop) for k,v in x.items() if k not in drop}
  if isinstance(x, list):
    return [_drop_fields_recursive(v, drop) for v in x]
  return x

def _enum_map(values):
  def f(v):
    if isinstance(v, str): return v
    if isinstance(v, int) and 0 <= v < len(values): return values[v]
    return v
  return f

# ---- enum tables matching the Go String() methods ----
ExchangeStatus = _enum_map(["pending","activated","suspended","deleted"])
DeviceDeviceType = _enum_map(["hybrid","transit","edge"])
DeviceStatus = _enum_map(["pending","activated","suspended","deleted","rejected"])
LinkLinkType = (lambda v: "WAN" if v == 1 else ("DZX" if v == 127 else v))
LinkStatus = _enum_map(["pending","activated","suspended","deleted","rejected","requested","hard-drained","soft-drained"])
ContributorStatus = _enum_map(["pending","activated","suspended","deleted"])
UserUserType = _enum_map(["ibrl","ibrl_with_allocated_ip","edge_filtering","multicast"])
CyoaType = _enum_map(["unknown","gre_over_dia","gre_over_fabric","gre_over_private_peering","gre_over_public_peering","gre_over_cable"])
UserStatus = _enum_map(["pending","activated","suspended","deleted","rejected","pending_ban","banned","updating"])

InterfaceStatus = _enum_map(["invalid","unmanaged","pending","activated","deleting","rejecting","unlinked"])
InterfaceType = _enum_map(["invalid","loopback","physical"])
LoopbackType = _enum_map(["none","vpnv4","ipv4","pim_rp_addr","reserved"])
InterfaceCYOA = _enum_map(["none","gre_over_dia","gre_over_fabric","gre_over_private_peering","gre_over_public_peering","gre_over_cable"])
InterfaceDIA = _enum_map(["none","dia"])
RoutingMode = _enum_map(["static","bgp"])

def _apply_path_enum(obj: Any, path: str, fn: Callable[[Any], Any]):
  parts = path.split(".")
  def rec(cur, i):
    if i == len(parts): return fn(cur)
    p = parts[i]
    if p.endswith("[]"):
      key = p[:-2]
      if isinstance(cur, dict) and isinstance(cur.get(key), list):
        cur[key] = [rec(item, i+1) for item in cur[key]]
      return cur
    else:
      if isinstance(cur, dict) and p in cur:
        cur[p] = rec(cur[p], i+1)
      return cur
  return rec(obj, 0)

def _apply_enum_paths(obj: dict, enum_paths: Dict[str, Callable[[Any], Any]]):
  for path, fn in enum_paths.items():
    _apply_path_enum(obj, path, fn)
  return obj

def normalize(obj: dict, spec: dict) -> dict:
  obj = _clean_and_convert(obj)

  # global drops
  obj = _drop_fields_recursive(obj, {"account_type", "bump_seed", "index"})

  # per-type drops
  per_drop = set(spec.get("__drop__", []))
  if per_drop:
    obj = _drop_fields_recursive(obj, per_drop)

  out = {}
  for k,v in obj.items():
    if k in spec and not k.startswith("__"):
      fn = spec[k]
      if isinstance(v, list):
        out[k] = [fn(x) for x in v]
      else:
        out[k] = fn(v)
    else:
      out[k] = v

  enum_paths = spec.get("__enum_paths__", None)
  if enum_paths:
    out = _apply_enum_paths(out, enum_paths)

  return out

# ---- per-type selection: getter + spec ----
def _getter(attr):
  return lambda pd: getattr(pd, attr)

NORM_BY_TYPE: Dict[str, Tuple[Callable[[Any], list], dict]] = {
  "devices": (_getter("devices"), {
    "__enum_paths__": {
      "device_type": DeviceDeviceType,
      "status": DeviceStatus,
      "interfaces[].body.status": InterfaceStatus,
      "interfaces[].body.interface_type": InterfaceType,
      "interfaces[].body.loopback_type": LoopbackType,
      "interfaces[].body.interface_cyoa": InterfaceCYOA,
      "interfaces[].body.interface_dia": InterfaceDIA,
      "interfaces[].body.routing_mode": RoutingMode,
    }
  }),
  "users": (_getter("users"), {
    "__enum_paths__": {
      "user_type": UserUserType,
      "cyoa_type": CyoaType,
      "status": UserStatus,
    }
  }),
  "links": (_getter("links"), {
    "__enum_paths__": {
      "link_type": LinkLinkType,
      "status": LinkStatus,
    }
  }),
  "locations": (_getter("locations"), {
    "__drop__": ["status"],
  }),
  "exchanges": (_getter("exchanges"), {
    "__enum_paths__": {"status": ExchangeStatus}
  }),
  "contributors": (_getter("contributors"), {
    "__enum_paths__": {"status": ContributorStatus}
  }),
  "multicast_groups": (_getter("multicast_groups"), {
    "__drop__": ["status"],
  }),

}
