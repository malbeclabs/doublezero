"""Test that enum __str__ outputs match the shared fixture file."""

import json
from pathlib import Path

import pytest

from serviceability.state import (
    AccessPassStatus,
    AccessPassTypeTag,
    ContributorStatus,
    CyoaType,
    DeviceDesiredStatus,
    DeviceDeviceType,
    DeviceHealth,
    DeviceStatus,
    ExchangeStatus,
    InterfaceStatus,
    InterfaceType,
    LinkDesiredStatus,
    LinkHealth,
    LinkLinkType,
    LinkStatus,
    LocationStatus,
    LoopbackType,
    MulticastGroupStatus,
    UserStatus,
    UserUserType,
)

FIXTURE_PATH = Path(__file__).resolve().parent.parent.parent.parent / "testdata" / "enum_strings.json"

ENUM_MAP = {
    "LocationStatus": LocationStatus,
    "ExchangeStatus": ExchangeStatus,
    "DeviceDeviceType": DeviceDeviceType,
    "DeviceStatus": DeviceStatus,
    "DeviceHealth": DeviceHealth,
    "DeviceDesiredStatus": DeviceDesiredStatus,
    "InterfaceStatus": InterfaceStatus,
    "InterfaceType": InterfaceType,
    "LoopbackType": LoopbackType,
    "LinkLinkType": LinkLinkType,
    "LinkStatus": LinkStatus,
    "LinkHealth": LinkHealth,
    "LinkDesiredStatus": LinkDesiredStatus,
    "ContributorStatus": ContributorStatus,
    "UserUserType": UserUserType,
    "CyoaType": CyoaType,
    "UserStatus": UserStatus,
    "MulticastGroupStatus": MulticastGroupStatus,
    "AccessPassTypeTag": AccessPassTypeTag,
    "AccessPassStatus": AccessPassStatus,
}


def _load_fixture() -> dict:
    return json.loads(FIXTURE_PATH.read_text())


def _enum_test_cases() -> list[tuple[str, int, str]]:
    """Yield (enum_name, int_value, expected_string) tuples."""
    fixture = _load_fixture()
    cases = []
    for enum_name, mappings in fixture.items():
        if enum_name not in ENUM_MAP:
            continue
        for str_value, expected in mappings.items():
            cases.append((enum_name, int(str_value), expected))
    return cases


@pytest.mark.parametrize(
    "enum_name,int_value,expected",
    _enum_test_cases(),
    ids=lambda x: str(x),
)
def test_enum_str(enum_name: str, int_value: int, expected: str) -> None:
    enum_cls = ENUM_MAP[enum_name]
    try:
        instance = enum_cls(int_value)
    except ValueError:
        # Unknown value not defined as a member; skip since Python IntEnum
        # cannot instantiate undefined members. Go/TS tests cover this case.
        pytest.skip(f"{enum_name}({int_value}) is not a valid member")
        return
    assert str(instance) == expected, (
        f"{enum_name}({int_value}): expected {expected!r}, got {str(instance)!r}"
    )


def test_all_enum_members_in_fixture() -> None:
    """Every Python IntEnum member must appear in the fixture.

    This catches the case where a variant is added in Python but not in
    enum_strings.json. Since the fixture is shared across Go, Python, and
    TypeScript, updating it will cause the other languages' tests to fail
    until they add the new variant too.
    """
    fixture = _load_fixture()
    missing = []
    for enum_name, enum_cls in ENUM_MAP.items():
        fixture_values = fixture.get(enum_name, {})
        for member in enum_cls:
            if str(member.value) not in fixture_values:
                missing.append(f"{enum_name}.{member.name} ({member.value})")
    assert not missing, (
        "Enum members missing from enum_strings.json fixture — add them so "
        "other languages stay in sync:\n  " + "\n  ".join(missing)
    )
