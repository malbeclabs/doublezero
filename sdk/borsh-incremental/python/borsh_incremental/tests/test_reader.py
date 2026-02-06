import struct

import pytest

from borsh_incremental import IncrementalReader


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _pack_u16(v: int) -> bytes:
    return struct.pack("<H", v)


def _pack_u32(v: int) -> bytes:
    return struct.pack("<I", v)


def _pack_u64(v: int) -> bytes:
    return struct.pack("<Q", v)


def _pack_u128(v: int) -> bytes:
    low = v & ((1 << 64) - 1)
    high = v >> 64
    return struct.pack("<QQ", low, high)


def _pack_f64(v: float) -> bytes:
    return struct.pack("<d", v)


def _pack_string(s: str) -> bytes:
    encoded = s.encode("utf-8")
    return _pack_u32(len(encoded)) + encoded


# ===========================================================================
# 1. Happy path for every read_* method
# ===========================================================================

class TestReadHappyPath:
    def test_read_u8(self):
        r = IncrementalReader(bytes([42]))
        assert r.read_u8() == 42
        assert r.offset == 1
        assert r.remaining == 0

    def test_read_bool_true(self):
        r = IncrementalReader(bytes([1]))
        assert r.read_bool() is True
        assert r.offset == 1

    def test_read_bool_false(self):
        r = IncrementalReader(bytes([0]))
        assert r.read_bool() is False
        assert r.offset == 1

    def test_read_bool_nonzero_is_true(self):
        r = IncrementalReader(bytes([255]))
        assert r.read_bool() is True

    def test_read_u16(self):
        r = IncrementalReader(_pack_u16(5000))
        assert r.read_u16() == 5000
        assert r.offset == 2
        assert r.remaining == 0

    def test_read_u32(self):
        r = IncrementalReader(_pack_u32(123456))
        assert r.read_u32() == 123456
        assert r.offset == 4
        assert r.remaining == 0

    def test_read_u64(self):
        r = IncrementalReader(_pack_u64(2**40 + 7))
        assert r.read_u64() == 2**40 + 7
        assert r.offset == 8

    def test_read_u128(self):
        val = (2**100) + 42
        r = IncrementalReader(_pack_u128(val))
        assert r.read_u128() == val
        assert r.offset == 16

    def test_read_f64(self):
        r = IncrementalReader(_pack_f64(3.14))
        assert r.read_f64() == pytest.approx(3.14)
        assert r.offset == 8

    def test_read_pubkey_raw(self):
        buf = bytes(range(32))
        r = IncrementalReader(buf)
        assert r.read_pubkey_raw() == buf
        assert r.offset == 32

    def test_read_ipv4(self):
        buf = bytes([10, 0, 0, 1])
        r = IncrementalReader(buf)
        assert r.read_ipv4() == buf
        assert r.offset == 4

    def test_read_network_v4(self):
        buf = bytes([192, 168, 1, 0, 24])
        r = IncrementalReader(buf)
        assert r.read_network_v4() == buf
        assert r.offset == 5

    def test_read_string(self):
        r = IncrementalReader(_pack_string("hello"))
        assert r.read_string() == "hello"
        assert r.offset == 4 + 5

    def test_read_bytes(self):
        data = bytes([1, 2, 3, 4, 5])
        r = IncrementalReader(data)
        assert r.read_bytes(3) == bytes([1, 2, 3])
        assert r.offset == 3
        assert r.remaining == 2

    def test_read_pubkey_raw_vec(self):
        pk1 = bytes(range(32))
        pk2 = bytes(range(32, 64))
        buf = _pack_u32(2) + pk1 + pk2
        r = IncrementalReader(buf)
        result = r.read_pubkey_raw_vec()
        assert len(result) == 2
        assert result[0] == pk1
        assert result[1] == pk2
        assert r.offset == 4 + 64

    def test_read_network_v4_vec(self):
        n1 = bytes([10, 0, 0, 0, 8])
        n2 = bytes([172, 16, 0, 0, 12])
        buf = _pack_u32(2) + n1 + n2
        r = IncrementalReader(buf)
        result = r.read_network_v4_vec()
        assert len(result) == 2
        assert result[0] == n1
        assert result[1] == n2
        assert r.offset == 4 + 10


# ===========================================================================
# 2. ValueError on empty buffer for every read_* method
# ===========================================================================

class TestReadEmptyBufferError:
    def test_read_u8(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_u8()

    def test_read_bool(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_bool()

    def test_read_u16(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_u16()

    def test_read_u32(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_u32()

    def test_read_u64(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_u64()

    def test_read_u128(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_u128()

    def test_read_f64(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_f64()

    def test_read_pubkey_raw(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_pubkey_raw()

    def test_read_ipv4(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_ipv4()

    def test_read_network_v4(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_network_v4()

    def test_read_string(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_string()

    def test_read_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_bytes(1)

    def test_read_pubkey_raw_vec(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_pubkey_raw_vec()

    def test_read_network_v4_vec(self):
        with pytest.raises(ValueError):
            IncrementalReader(b"").read_network_v4_vec()


# ===========================================================================
# 3. Partial data ValueError for multi-byte read_* methods
# ===========================================================================

class TestReadPartialDataError:
    def test_read_u16_one_byte(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(1)).read_u16()

    def test_read_u32_two_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(2)).read_u32()

    def test_read_u64_four_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(4)).read_u64()

    def test_read_u128_eight_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(8)).read_u128()

    def test_read_f64_four_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(4)).read_f64()

    def test_read_pubkey_raw_sixteen_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(16)).read_pubkey_raw()

    def test_read_ipv4_two_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(2)).read_ipv4()

    def test_read_network_v4_three_bytes(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(3)).read_network_v4()

    def test_read_string_truncated_body(self):
        # Length prefix says 10 bytes, but only 3 available.
        buf = _pack_u32(10) + bytes(3)
        with pytest.raises(ValueError):
            IncrementalReader(buf).read_string()

    def test_read_bytes_partial(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(5)).read_bytes(10)


# ===========================================================================
# 4. try_read_* returns default on empty buffer
# ===========================================================================

class TestTryReadEmptyDefault:
    def test_try_read_u8(self):
        assert IncrementalReader(b"").try_read_u8(99) == 99

    def test_try_read_bool(self):
        assert IncrementalReader(b"").try_read_bool(True) is True

    def test_try_read_u16(self):
        assert IncrementalReader(b"").try_read_u16(1234) == 1234

    def test_try_read_u32(self):
        assert IncrementalReader(b"").try_read_u32(777) == 777

    def test_try_read_u64(self):
        assert IncrementalReader(b"").try_read_u64(888) == 888

    def test_try_read_u128(self):
        assert IncrementalReader(b"").try_read_u128(999) == 999

    def test_try_read_f64(self):
        assert IncrementalReader(b"").try_read_f64(1.5) == 1.5

    def test_try_read_pubkey_raw(self):
        default = b"\xff" * 32
        assert IncrementalReader(b"").try_read_pubkey_raw(default) == default

    def test_try_read_ipv4(self):
        default = bytes([127, 0, 0, 1])
        assert IncrementalReader(b"").try_read_ipv4(default) == default

    def test_try_read_network_v4(self):
        default = bytes([0, 0, 0, 0, 0])
        assert IncrementalReader(b"").try_read_network_v4(default) == default

    def test_try_read_string(self):
        assert IncrementalReader(b"").try_read_string("fallback") == "fallback"

    def test_try_read_pubkey_raw_vec(self):
        assert IncrementalReader(b"").try_read_pubkey_raw_vec() == []

    def test_try_read_pubkey_raw_vec_custom_default(self):
        sentinel = [b"\xab" * 32]
        assert IncrementalReader(b"").try_read_pubkey_raw_vec(sentinel) is sentinel

    def test_try_read_network_v4_vec(self):
        assert IncrementalReader(b"").try_read_network_v4_vec() == []

    def test_try_read_network_v4_vec_custom_default(self):
        sentinel = [bytes(5)]
        assert IncrementalReader(b"").try_read_network_v4_vec(sentinel) is sentinel


# ===========================================================================
# 5. try_read_* returns actual value when data exists
# ===========================================================================

class TestTryReadWithData:
    def test_try_read_u8(self):
        assert IncrementalReader(bytes([42])).try_read_u8(0) == 42

    def test_try_read_bool(self):
        assert IncrementalReader(bytes([1])).try_read_bool(False) is True

    def test_try_read_u16(self):
        assert IncrementalReader(_pack_u16(300)).try_read_u16(0) == 300

    def test_try_read_u32(self):
        assert IncrementalReader(_pack_u32(70000)).try_read_u32(0) == 70000

    def test_try_read_u64(self):
        assert IncrementalReader(_pack_u64(2**50)).try_read_u64(0) == 2**50

    def test_try_read_u128(self):
        val = 2**100
        assert IncrementalReader(_pack_u128(val)).try_read_u128(0) == val

    def test_try_read_f64(self):
        assert IncrementalReader(_pack_f64(2.718)).try_read_f64(0.0) == pytest.approx(2.718)

    def test_try_read_pubkey_raw(self):
        pk = bytes(range(32))
        assert IncrementalReader(pk).try_read_pubkey_raw(b"\x00" * 32) == pk

    def test_try_read_ipv4(self):
        ip = bytes([8, 8, 8, 8])
        assert IncrementalReader(ip).try_read_ipv4(bytes(4)) == ip

    def test_try_read_network_v4(self):
        net = bytes([10, 0, 0, 0, 24])
        assert IncrementalReader(net).try_read_network_v4(bytes(5)) == net

    def test_try_read_string(self):
        assert IncrementalReader(_pack_string("hi")).try_read_string("x") == "hi"

    def test_try_read_pubkey_raw_vec(self):
        pk = bytes(range(32))
        buf = _pack_u32(1) + pk
        result = IncrementalReader(buf).try_read_pubkey_raw_vec()
        assert result == [pk]

    def test_try_read_network_v4_vec(self):
        net = bytes([10, 0, 0, 0, 8])
        buf = _pack_u32(1) + net
        result = IncrementalReader(buf).try_read_network_v4_vec()
        assert result == [net]


# ===========================================================================
# 6. try_read_* with partial data returns default
# ===========================================================================

class TestTryReadPartialDefault:
    def test_try_read_u16_one_byte(self):
        assert IncrementalReader(bytes(1)).try_read_u16(55) == 55

    def test_try_read_u32_two_bytes(self):
        assert IncrementalReader(bytes(2)).try_read_u32(55) == 55

    def test_try_read_u64_four_bytes(self):
        assert IncrementalReader(bytes(4)).try_read_u64(55) == 55

    def test_try_read_u128_eight_bytes(self):
        assert IncrementalReader(bytes(8)).try_read_u128(55) == 55

    def test_try_read_f64_four_bytes(self):
        assert IncrementalReader(bytes(4)).try_read_f64(1.0) == 1.0

    def test_try_read_pubkey_raw_sixteen_bytes(self):
        default = b"\xff" * 32
        assert IncrementalReader(bytes(16)).try_read_pubkey_raw(default) == default

    def test_try_read_ipv4_two_bytes(self):
        assert IncrementalReader(bytes(2)).try_read_ipv4(bytes(4)) == bytes(4)

    def test_try_read_network_v4_three_bytes(self):
        assert IncrementalReader(bytes(3)).try_read_network_v4(bytes(5)) == bytes(5)

    def test_try_read_string_two_bytes(self):
        # Need at least 4 bytes for the length prefix.
        assert IncrementalReader(bytes(2)).try_read_string("nope") == "nope"

    def test_try_read_pubkey_raw_vec_two_bytes(self):
        assert IncrementalReader(bytes(2)).try_read_pubkey_raw_vec() == []

    def test_try_read_network_v4_vec_two_bytes(self):
        assert IncrementalReader(bytes(2)).try_read_network_v4_vec() == []


# ===========================================================================
# 7. Sequential reads with offset tracking
# ===========================================================================

class TestSequentialReads:
    def test_u8_u16_u32_u64(self):
        buf = bytes([7]) + _pack_u16(500) + _pack_u32(100000) + _pack_u64(2**48)
        r = IncrementalReader(buf)
        assert r.offset == 0
        assert r.read_u8() == 7
        assert r.offset == 1
        assert r.read_u16() == 500
        assert r.offset == 3
        assert r.read_u32() == 100000
        assert r.offset == 7
        assert r.read_u64() == 2**48
        assert r.offset == 15
        assert r.remaining == 0

    def test_string_then_pubkey(self):
        pk = bytes(range(32))
        buf = _pack_string("test") + pk
        r = IncrementalReader(buf)
        assert r.read_string() == "test"
        assert r.offset == 8  # 4 len + 4 chars
        assert r.read_pubkey_raw() == pk
        assert r.offset == 40
        assert r.remaining == 0

    def test_bool_then_f64_then_ipv4(self):
        buf = bytes([1]) + _pack_f64(9.81) + bytes([192, 168, 0, 1])
        r = IncrementalReader(buf)
        assert r.read_bool() is True
        assert r.offset == 1
        assert r.read_f64() == pytest.approx(9.81)
        assert r.offset == 9
        assert r.read_ipv4() == bytes([192, 168, 0, 1])
        assert r.offset == 13


# ===========================================================================
# 8. Trailing optional fields scenario
# ===========================================================================

class TestTrailingFields:
    def test_required_then_missing_optional(self):
        buf = bytes([1]) + _pack_u32(42)
        r = IncrementalReader(buf)
        assert r.read_u8() == 1
        assert r.read_u32() == 42
        # All remaining try_reads should return defaults.
        assert r.try_read_u16(9999) == 9999
        assert r.try_read_string("none") == "none"
        assert r.try_read_f64(-1.0) == -1.0
        assert r.try_read_pubkey_raw_vec() == []
        assert r.remaining == 0

    def test_required_then_present_optional(self):
        buf = bytes([1]) + _pack_u32(42) + _pack_u16(7) + _pack_string("opt")
        r = IncrementalReader(buf)
        assert r.read_u8() == 1
        assert r.read_u32() == 42
        assert r.try_read_u16(9999) == 7
        assert r.try_read_string("none") == "opt"
        assert r.remaining == 0

    def test_partial_optional_falls_back(self):
        # After required fields, only 1 byte remains -- not enough for u32.
        buf = bytes([1]) + bytes([0xFF])
        r = IncrementalReader(buf)
        assert r.read_u8() == 1
        assert r.try_read_u32(0) == 0
        # The leftover byte is still unconsumed.
        assert r.remaining == 1


# ===========================================================================
# 9. Vec methods
# ===========================================================================

class TestVecMethods:
    # --- pubkey_raw_vec ---

    def test_pubkey_raw_vec_empty(self):
        buf = _pack_u32(0)
        r = IncrementalReader(buf)
        assert r.read_pubkey_raw_vec() == []
        assert r.offset == 4

    def test_pubkey_raw_vec_single(self):
        pk = bytes(range(32))
        buf = _pack_u32(1) + pk
        r = IncrementalReader(buf)
        assert r.read_pubkey_raw_vec() == [pk]

    def test_pubkey_raw_vec_multiple(self):
        pks = [bytes([i] * 32) for i in range(3)]
        buf = _pack_u32(3) + b"".join(pks)
        r = IncrementalReader(buf)
        assert r.read_pubkey_raw_vec() == pks

    def test_pubkey_raw_vec_truncated_length(self):
        # Only 2 bytes, not enough for the u32 length prefix.
        with pytest.raises(ValueError):
            IncrementalReader(bytes(2)).read_pubkey_raw_vec()

    def test_pubkey_raw_vec_truncated_elements(self):
        # Says 2 elements but only has data for 1.
        pk = bytes(range(32))
        buf = _pack_u32(2) + pk
        with pytest.raises(ValueError):
            IncrementalReader(buf).read_pubkey_raw_vec()

    # --- network_v4_vec ---

    def test_network_v4_vec_empty(self):
        buf = _pack_u32(0)
        r = IncrementalReader(buf)
        assert r.read_network_v4_vec() == []

    def test_network_v4_vec_single(self):
        net = bytes([10, 0, 0, 0, 8])
        buf = _pack_u32(1) + net
        r = IncrementalReader(buf)
        assert r.read_network_v4_vec() == [net]

    def test_network_v4_vec_multiple(self):
        nets = [bytes([10, i, 0, 0, 24]) for i in range(3)]
        buf = _pack_u32(3) + b"".join(nets)
        r = IncrementalReader(buf)
        assert r.read_network_v4_vec() == nets
        assert r.offset == 4 + 15

    def test_network_v4_vec_truncated_length(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(2)).read_network_v4_vec()

    def test_network_v4_vec_truncated_elements(self):
        # Says 2 elements but only has data for 1.
        net = bytes([10, 0, 0, 0, 8])
        buf = _pack_u32(2) + net
        with pytest.raises(ValueError):
            IncrementalReader(buf).read_network_v4_vec()


# ===========================================================================
# 10. String edge cases
# ===========================================================================

class TestStringEdgeCases:
    def test_empty_string(self):
        buf = _pack_u32(0)
        r = IncrementalReader(buf)
        assert r.read_string() == ""
        assert r.offset == 4

    def test_normal_string(self):
        r = IncrementalReader(_pack_string("borsh"))
        assert r.read_string() == "borsh"

    def test_string_truncated_body(self):
        buf = _pack_u32(100) + b"short"
        with pytest.raises(ValueError):
            IncrementalReader(buf).read_string()

    def test_string_truncated_length_prefix(self):
        with pytest.raises(ValueError):
            IncrementalReader(bytes(2)).read_string()

    def test_utf8_string(self):
        text = "cafe\u0301"  # cafe with combining accent
        r = IncrementalReader(_pack_string(text))
        assert r.read_string() == text


# ===========================================================================
# 11. U128 byte order (little-endian)
# ===========================================================================

class TestU128ByteOrder:
    def test_one(self):
        buf = bytes([1]) + bytes(15)
        r = IncrementalReader(buf)
        assert r.read_u128() == 1

    def test_low_64_only(self):
        val = 0xDEADBEEFCAFEBABE
        buf = struct.pack("<Q", val) + bytes(8)
        r = IncrementalReader(buf)
        assert r.read_u128() == val

    def test_high_64_only(self):
        val = 0xDEADBEEFCAFEBABE
        buf = bytes(8) + struct.pack("<Q", val)
        r = IncrementalReader(buf)
        assert r.read_u128() == val << 64

    def test_max_u128(self):
        buf = b"\xff" * 16
        r = IncrementalReader(buf)
        assert r.read_u128() == (1 << 128) - 1


# ===========================================================================
# 12. F64 known values
# ===========================================================================

class TestF64KnownValues:
    def test_zero(self):
        r = IncrementalReader(_pack_f64(0.0))
        assert r.read_f64() == 0.0

    def test_negative(self):
        r = IncrementalReader(_pack_f64(-273.15))
        assert r.read_f64() == pytest.approx(-273.15)

    def test_large(self):
        r = IncrementalReader(_pack_f64(1e300))
        assert r.read_f64() == pytest.approx(1e300)

    def test_small_fraction(self):
        r = IncrementalReader(_pack_f64(1e-10))
        assert r.read_f64() == pytest.approx(1e-10)


# ===========================================================================
# 13. offset and remaining correctness
# ===========================================================================

class TestOffsetAndRemaining:
    def test_initial_state(self):
        data = bytes(10)
        r = IncrementalReader(data)
        assert r.offset == 0
        assert r.remaining == 10

    def test_empty_buffer(self):
        r = IncrementalReader(b"")
        assert r.offset == 0
        assert r.remaining == 0

    def test_offset_advances_correctly(self):
        buf = bytes(20)
        r = IncrementalReader(buf)
        r.read_u8()
        assert r.offset == 1
        assert r.remaining == 19
        r.read_u32()
        assert r.offset == 5
        assert r.remaining == 15
        r.read_u64()
        assert r.offset == 13
        assert r.remaining == 7

    def test_try_read_does_not_advance_on_default(self):
        r = IncrementalReader(b"")
        r.try_read_u32(0)
        assert r.offset == 0
        assert r.remaining == 0

    def test_try_read_advances_on_success(self):
        r = IncrementalReader(_pack_u32(1))
        r.try_read_u32(0)
        assert r.offset == 4
        assert r.remaining == 0

    def test_remaining_after_full_consumption(self):
        r = IncrementalReader(bytes([1, 2]))
        r.read_u8()
        r.read_u8()
        assert r.remaining == 0
        with pytest.raises(ValueError):
            r.read_u8()


# ===========================================================================
# 14. DefensiveReader returns defaults on truncated/missing data
# ===========================================================================

from borsh_incremental import DefensiveReader


class TestDefensiveReaderReturnsDefaults:
    """DefensiveReader should return zero/empty defaults when data is missing."""

    def test_empty_buffer_returns_defaults(self):
        r = DefensiveReader(b"")
        assert r.read_u8() == 0
        assert r.read_u16() == 0
        assert r.read_u32() == 0
        assert r.read_u64() == 0
        assert r.read_u128() == 0
        assert r.read_f64() == 0.0
        assert r.read_bool() is False
        assert r.read_string() == ""
        assert r.read_pubkey_raw() == b"\x00" * 32
        assert r.read_ipv4() == b"\x00" * 4
        assert r.read_network_v4() == b"\x00" * 5
        assert r.read_pubkey_raw_vec() == []
        assert r.read_network_v4_vec() == []
        assert r.read_bytes(10) == b"\x00" * 10

    def test_partial_data_returns_defaults_for_missing(self):
        # Buffer has only 2 bytes, should return defaults for larger types
        r = DefensiveReader(bytes([0x42, 0x43]))
        assert r.read_u8() == 0x42  # succeeds
        assert r.read_u8() == 0x43  # succeeds
        assert r.read_u8() == 0     # default - no more data
        assert r.read_u32() == 0    # default - no more data

    def test_simulates_struct_with_new_trailing_field(self):
        # Simulate reading an "old" account that doesn't have a new trailing field.
        # Old struct: u32 + u64 = 12 bytes
        # New struct: u32 + u64 + u32 (new field) = 16 bytes
        old_data = _pack_u32(100) + struct.pack("<Q", 200)  # 12 bytes
        r = DefensiveReader(old_data)

        # Read the "old" fields successfully
        assert r.read_u32() == 100
        assert r.read_u64() == 200

        # The "new" trailing field should return default (0) without error
        assert r.read_u32() == 0

    def test_does_not_throw_on_truncated_vec(self):
        # Empty buffer - vec read should return empty list, not throw
        r = DefensiveReader(b"")
        assert r.read_pubkey_raw_vec() == []
        assert r.read_network_v4_vec() == []

    def test_offset_and_remaining(self):
        r = DefensiveReader(bytes([1, 2, 3, 4]))
        assert r.offset == 0
        assert r.remaining == 4
        r.read_u8()
        assert r.offset == 1
        assert r.remaining == 3
