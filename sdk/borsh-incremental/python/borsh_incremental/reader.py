"""Borsh incremental deserialization reader.

Provides cursor-based reading of Borsh-serialized binary data with
backward-compatible trailing field support via try_read_* methods.
"""

from __future__ import annotations

import struct


class IncrementalReader:
    """Cursor-based Borsh binary reader with incremental deserialization."""

    def __init__(self, data: bytes) -> None:
        self._data = data
        self._offset = 0

    @property
    def offset(self) -> int:
        return self._offset

    @property
    def remaining(self) -> int:
        return len(self._data) - self._offset

    # --- Strict read methods (raise on insufficient data) ---

    def read_u8(self) -> int:
        if self._offset + 1 > len(self._data):
            raise ValueError(f"borsh: not enough data for u8 at offset {self._offset}")
        v = self._data[self._offset]
        self._offset += 1
        return v

    def read_bool(self) -> bool:
        return self.read_u8() != 0

    def read_u16(self) -> int:
        if self._offset + 2 > len(self._data):
            raise ValueError(f"borsh: not enough data for u16 at offset {self._offset}")
        (v,) = struct.unpack_from("<H", self._data, self._offset)
        self._offset += 2
        return v

    def read_u32(self) -> int:
        if self._offset + 4 > len(self._data):
            raise ValueError(f"borsh: not enough data for u32 at offset {self._offset}")
        (v,) = struct.unpack_from("<I", self._data, self._offset)
        self._offset += 4
        return v

    def read_u64(self) -> int:
        if self._offset + 8 > len(self._data):
            raise ValueError(f"borsh: not enough data for u64 at offset {self._offset}")
        (v,) = struct.unpack_from("<Q", self._data, self._offset)
        self._offset += 8
        return v

    def read_u128(self) -> int:
        if self._offset + 16 > len(self._data):
            raise ValueError(f"borsh: not enough data for u128 at offset {self._offset}")
        low, high = struct.unpack_from("<QQ", self._data, self._offset)
        self._offset += 16
        return low | (high << 64)

    def read_f64(self) -> float:
        if self._offset + 8 > len(self._data):
            raise ValueError(f"borsh: not enough data for f64 at offset {self._offset}")
        (v,) = struct.unpack_from("<d", self._data, self._offset)
        self._offset += 8
        return v

    def read_bytes(self, n: int) -> bytes:
        if self._offset + n > len(self._data):
            raise ValueError(
                f"borsh: not enough data for {n} bytes at offset {self._offset}"
            )
        v = bytes(self._data[self._offset : self._offset + n])
        self._offset += n
        return v

    def read_pubkey_raw(self) -> bytes:
        """Read a 32-byte public key as raw bytes."""
        return self.read_bytes(32)

    def read_ipv4(self) -> bytes:
        return self.read_bytes(4)

    def read_network_v4(self) -> bytes:
        return self.read_bytes(5)

    def read_string(self) -> str:
        length = self.read_u32()
        if length == 0:
            return ""
        if self._offset + length > len(self._data):
            raise ValueError(
                f"borsh: not enough data for string of length {length} at offset {self._offset}"
            )
        s = self._data[self._offset : self._offset + length].decode("utf-8")
        self._offset += length
        return s

    def read_pubkey_raw_vec(self) -> list[bytes]:
        length = self.read_u32()
        return [self.read_pubkey_raw() for _ in range(length)]

    def read_network_v4_vec(self) -> list[bytes]:
        length = self.read_u32()
        return [self.read_network_v4() for _ in range(length)]

    # --- Try variants (return default when no bytes available) ---

    def try_read_u8(self, default: int = 0) -> int:
        if self.remaining < 1:
            return default
        return self.read_u8()

    def try_read_bool(self, default: bool = False) -> bool:
        if self.remaining < 1:
            return default
        return self.read_bool()

    def try_read_u16(self, default: int = 0) -> int:
        if self.remaining < 2:
            return default
        return self.read_u16()

    def try_read_u32(self, default: int = 0) -> int:
        if self.remaining < 4:
            return default
        return self.read_u32()

    def try_read_u64(self, default: int = 0) -> int:
        if self.remaining < 8:
            return default
        return self.read_u64()

    def try_read_u128(self, default: int = 0) -> int:
        if self.remaining < 16:
            return default
        return self.read_u128()

    def try_read_f64(self, default: float = 0.0) -> float:
        if self.remaining < 8:
            return default
        return self.read_f64()

    def try_read_pubkey_raw(self, default: bytes = b"\x00" * 32) -> bytes:
        if self.remaining < 32:
            return default
        return self.read_pubkey_raw()

    def try_read_ipv4(self, default: bytes = b"\x00" * 4) -> bytes:
        if self.remaining < 4:
            return default
        return self.read_ipv4()

    def try_read_network_v4(self, default: bytes = b"\x00" * 5) -> bytes:
        if self.remaining < 5:
            return default
        return self.read_network_v4()

    def try_read_string(self, default: str = "") -> str:
        if self.remaining < 4:
            return default
        return self.read_string()

    def try_read_pubkey_raw_vec(self, default: list[bytes] | None = None) -> list[bytes]:
        if self.remaining < 4:
            return default if default is not None else []
        return self.read_pubkey_raw_vec()

    def try_read_network_v4_vec(self, default: list[bytes] | None = None) -> list[bytes]:
        if self.remaining < 4:
            return default if default is not None else []
        return self.read_network_v4_vec()


class DefensiveReader:
    """Wrapper around IncrementalReader that uses try_read for all operations.

    All read methods return zero/empty defaults on insufficient data, matching
    Go's ByteReader behavior. This makes deserialization resilient to schema
    changes where new fields are added to the end of structs.
    """

    def __init__(self, data: bytes) -> None:
        self._r = IncrementalReader(data)

    @property
    def offset(self) -> int:
        return self._r.offset

    @property
    def remaining(self) -> int:
        return self._r.remaining

    def read_u8(self) -> int:
        return self._r.try_read_u8(0)

    def read_bool(self) -> bool:
        return self._r.try_read_bool(False)

    def read_u16(self) -> int:
        return self._r.try_read_u16(0)

    def read_u32(self) -> int:
        return self._r.try_read_u32(0)

    def read_u64(self) -> int:
        return self._r.try_read_u64(0)

    def read_u128(self) -> int:
        return self._r.try_read_u128(0)

    def read_f64(self) -> float:
        return self._r.try_read_f64(0.0)

    def read_pubkey_raw(self) -> bytes:
        return self._r.try_read_pubkey_raw(b"\x00" * 32)

    def read_ipv4(self) -> bytes:
        return self._r.try_read_ipv4(b"\x00" * 4)

    def read_network_v4(self) -> bytes:
        return self._r.try_read_network_v4(b"\x00" * 5)

    def read_string(self) -> str:
        return self._r.try_read_string("")

    def read_pubkey_raw_vec(self) -> list[bytes]:
        return self._r.try_read_pubkey_raw_vec([])

    def read_network_v4_vec(self) -> list[bytes]:
        return self._r.try_read_network_v4_vec([])

    def read_bytes(self, n: int) -> bytes:
        """Read n bytes, returning zero bytes if insufficient data."""
        if self._r.remaining < n:
            return b"\x00" * n
        return self._r.read_bytes(n)
