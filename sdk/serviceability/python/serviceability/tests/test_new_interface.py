"""Hand-built byte-vector tests for the size-prefixed NewInterface reader.

The wire format mirrors smartcontract/programs/doublezero-serviceability::state::device:
each NewInterface element is (u16 size, u8 version, body), where size includes
the 3-byte prefix. The Device account stores this vec immediately after
max_multicast_publishers.
"""

from __future__ import annotations

import struct

import pytest

from serviceability.state import (
    CURRENT_INTERFACE_VERSION,
    AccountTypeEnum,
    Device,
)


def _u16(v: int) -> bytes:
    return struct.pack("<H", v)


def _u32(v: int) -> bytes:
    return struct.pack("<I", v)


def _u64(v: int) -> bytes:
    return struct.pack("<Q", v)


def _string(s: str) -> bytes:
    return _u32(len(s)) + s.encode("utf-8")


def _new_interface_body(name: str) -> bytes:
    """Body bytes for a minimal V4 NewInterface with caller-provided name."""
    parts: list[bytes] = []
    parts.append(b"\x00")  # status
    parts.append(_string(name))
    parts.append(b"\x00")  # interface_type
    parts.append(b"\x00")  # interface_cyoa
    parts.append(b"\x00")  # interface_dia
    parts.append(b"\x00")  # loopback_type
    parts.append(_u64(0))  # bandwidth
    parts.append(_u64(0))  # cir
    parts.append(_u16(0))  # mtu
    parts.append(b"\x00")  # routing_mode
    parts.append(_u16(0))  # vlan_id
    parts.append(b"\x00" * 5)  # ip_net (NetworkV4: 4 bytes IP + 1 byte prefix)
    parts.append(_u16(0))  # node_segment_idx
    parts.append(b"\x00")  # user_tunnel_endpoint
    parts.append(_u32(0))  # flex_algo_node_segments len = 0
    return b"".join(parts)


def _new_interface_sized(name: str, version: int = CURRENT_INTERFACE_VERSION,
                         body_override: bytes | None = None) -> bytes:
    body = body_override if body_override is not None else _new_interface_body(name)
    size = 3 + len(body)
    return _u16(size) + bytes([version]) + body


def _legacy_interface_v1(name: str) -> bytes:
    """A single V1-disc legacy enum interface with caller-provided name.

    V1 is used over V2 here because Python's V2 reader also consumes a trailing
    flex_algo_node_segments u32 that the Go V2 reader does not — V1 sidesteps
    that asymmetry without affecting what we're testing (the trailing vec).
    """
    return (
        bytes([0])  # enum disc V1
        + b"\x00"  # status
        + _string(name)
        + b"\x00"  # interface_type
        + b"\x00"  # loopback_type
        + _u16(0)  # vlan_id
        + b"\x00" * 5  # ip_net
        + _u16(0)  # node_segment_idx
        + b"\x00"  # user_tunnel_endpoint
    )


def _device(num_legacy: int, names: list[str], trailing: bytes | None) -> bytes:
    parts: list[bytes] = []
    parts.append(bytes([int(AccountTypeEnum.DEVICE)]))  # account_type
    parts.append(b"\x00" * 32)  # owner
    parts.append(_u64(0) + _u64(1))  # index (u128 little-endian as two u64s)
    parts.append(b"\xff")  # bump_seed
    parts.append(b"\x00" * 32)  # location_pk
    parts.append(b"\x00" * 32)  # exchange_pk
    parts.append(b"\x00")  # device_type
    parts.append(b"\x01\x02\x03\x04")  # public_ip
    parts.append(b"\x01")  # status (Activated)
    parts.append(_string("dev-test"))  # code
    parts.append(_u32(0))  # dz_prefixes (empty)
    parts.append(b"\x00" * 32)  # metrics_publisher_pk
    parts.append(b"\x00" * 32)  # contributor_pk
    parts.append(_string("default"))  # mgmt_vrf
    parts.append(_u32(num_legacy))
    for name in names:
        parts.append(_legacy_interface_v1(name))
    parts.append(_u32(0))  # reference_count
    parts.append(_u16(0))  # users_count
    parts.append(_u16(0))  # max_users
    parts.append(b"\x00")  # device_health
    parts.append(b"\x00")  # device_desired_status
    parts.append(_u16(0))  # unicast_users_count
    parts.append(_u16(0))  # multicast_subscribers_count
    parts.append(_u16(0))  # max_unicast_users
    parts.append(_u16(0))  # max_multicast_subscribers
    parts.append(_u16(0))  # reserved_seats
    parts.append(_u16(0))  # multicast_publishers_count
    parts.append(_u16(0))  # max_multicast_publishers
    if trailing is not None:
        parts.append(trailing)
    return b"".join(parts)


def test_populated_trailing_vec():
    # Cross-language framing assertion: empty-name body length is
    # 1+4+1+1+1+1+8+8+2+1+2+5+2+1+4 = 42, so size = 3 + 42 = 45.
    assert 3 + len(_new_interface_body("")) == 45

    trailing = _u32(2) + _new_interface_sized("Eth1") + _new_interface_sized("Lo0")
    raw = _device(2, ["Eth1", "Lo0"], trailing)

    dev = Device.from_bytes(raw)
    assert len(dev.interfaces) == 2
    assert len(dev.new_interfaces) == 2
    assert dev.new_interfaces[0].name == "Eth1"
    assert dev.new_interfaces[1].name == "Lo0"
    assert dev.new_interfaces[0].version == CURRENT_INTERFACE_VERSION
    for i, ni in enumerate(dev.new_interfaces):
        expected_size = 3 + len(_new_interface_body(ni.name))
        assert ni.size == expected_size, (
            f"size mismatch on element {i}: expected {expected_size}, got {ni.size}"
        )


def test_legacy_account_rebuilds_new_interfaces():
    raw = _device(2, ["Eth1", "Lo0"], trailing=None)

    dev = Device.from_bytes(raw)
    assert len(dev.interfaces) == 2
    assert len(dev.new_interfaces) == 2
    assert dev.new_interfaces[0].name == "Eth1"
    assert dev.new_interfaces[1].name == "Lo0"
    # Rebuilt entries are stamped with the current schema version and zero size
    # (callers don't need on-disk size for a rebuild).
    for ni in dev.new_interfaces:
        assert ni.version == CURRENT_INTERFACE_VERSION
        assert ni.size == 0


def test_trailing_length_mismatch_raises():
    trailing = _u32(1) + _new_interface_sized("Eth1")  # only 1, but legacy has 2
    raw = _device(2, ["Eth1", "Lo0"], trailing)

    with pytest.raises(ValueError, match="length 1 != interfaces length 2"):
        Device.from_bytes(raw)


def test_future_version_skips_trailing_bytes():
    # Forge an element with version=5 and 8 trailing junk bytes appended past
    # the known body. The reader must advance past start+size and leave the
    # next element readable.
    body = _new_interface_body("Future1") + bytes([0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE])
    sized = _new_interface_sized("Future1", version=5, body_override=body)
    trailing = _u32(1) + sized
    raw = _device(1, ["Future1"], trailing)

    dev = Device.from_bytes(raw)
    assert len(dev.new_interfaces) == 1
    assert dev.new_interfaces[0].version == 5
    assert dev.new_interfaces[0].size == 3 + len(body)
    # Body fields up to known shape are still parsed.
    assert dev.new_interfaces[0].name == "Future1"
