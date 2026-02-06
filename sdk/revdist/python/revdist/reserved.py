"""Wrapper for reserved / padding bytes in on-chain account layouts."""

from __future__ import annotations


class Reserved(bytes):
    """Thin wrapper around ``bytes`` that marks padding or storage-gap fields.

    Behaves exactly like ``bytes`` but makes the intent explicit when used as
    a type annotation in dataclasses::

        reserved0: Reserved  # [3]u8 padding
    """

    def __new__(cls, data: bytes) -> Reserved:
        return super().__new__(cls, data)

    def __repr__(self) -> str:
        return f"Reserved({len(self)})"
