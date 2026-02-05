"""Fixture-based compatibility tests."""

import json
from pathlib import Path

from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from serviceability.state import (
    AccessPass,
    Contributor,
    Device,
    Exchange,
    GlobalConfig,
    GlobalState,
    Link,
    Location,
    MulticastGroup,
    ProgramConfig,
    User,
)

FIXTURES_DIR = Path(__file__).resolve().parent.parent.parent.parent / "testdata" / "fixtures"


def _load_fixture(name: str) -> tuple[bytes, dict]:
    bin_data = (FIXTURES_DIR / f"{name}.bin").read_bytes()
    meta = json.loads((FIXTURES_DIR / f"{name}.json").read_text())
    return bin_data, meta


def _assert_fields(expected_fields: list[dict], got: dict) -> None:
    for f in expected_fields:
        name = f["name"]
        if name not in got:
            continue
        typ = f["typ"]
        raw = f["value"]
        actual = got[name]
        if typ in ("u8", "u16", "u32", "u64"):
            assert actual == int(raw), f"{name}: expected {raw}, got {actual}"
        elif typ == "pubkey":
            expected = Pubkey.from_string(raw)
            assert actual == expected, f"{name}: expected {expected}, got {actual}"
        elif typ == "string":
            assert actual == raw, f"{name}: expected {raw!r}, got {actual!r}"
        elif typ == "bool":
            expected = raw == "true"
            assert actual == expected, f"{name}: expected {expected}, got {actual}"
        elif typ == "ipv4":
            import ipaddress

            expected_bytes = ipaddress.IPv4Address(raw).packed
            assert actual == expected_bytes, f"{name}: expected {raw}, got {actual}"
        elif typ == "networkv4":
            import ipaddress

            net = ipaddress.IPv4Network(raw)
            expected_bytes = net.network_address.packed + bytes([net.prefixlen])
            assert actual == expected_bytes, f"{name}: expected {raw}, got {actual}"
        elif typ == "u128":
            assert actual == int(raw), f"{name}: expected {raw}, got {actual}"


class TestFixtureGlobalState:
    def test_deserialize(self):
        data, meta = _load_fixture("global_state")
        gs = GlobalState.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": gs.account_type,
                "BumpSeed": gs.bump_seed,
                "ContributorAirdropLamports": gs.contributor_airdrop_lamports,
                "UserAirdropLamports": gs.user_airdrop_lamports,
                "ActivatorAuthorityPk": gs.activator_authority_pk,
                "SentinelAuthorityPk": gs.sentinel_authority_pk,
                "HealthOraclePk": gs.health_oracle_pk,
            },
        )


class TestFixtureGlobalConfig:
    def test_deserialize(self):
        data, meta = _load_fixture("global_config")
        gc = GlobalConfig.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": gc.account_type,
                "Owner": gc.owner,
                "BumpSeed": gc.bump_seed,
                "LocalAsn": gc.local_asn,
                "RemoteAsn": gc.remote_asn,
                "NextBgpCommunity": gc.next_bgp_community,
            },
        )


class TestFixtureLocation:
    def test_deserialize(self):
        data, meta = _load_fixture("location")
        loc = Location.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": loc.account_type,
                "Owner": loc.owner,
                "BumpSeed": loc.bump_seed,
                "LocId": loc.loc_id,
                "Status": loc.status,
                "ReferenceCount": loc.reference_count,
            },
        )


class TestFixtureExchange:
    def test_deserialize(self):
        data, meta = _load_fixture("exchange")
        ex = Exchange.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": ex.account_type,
                "Owner": ex.owner,
                "BumpSeed": ex.bump_seed,
                "BgpCommunity": ex.bgp_community,
                "Status": ex.status,
                "ReferenceCount": ex.reference_count,
                "Device1Pk": ex.device1_pk,
                "Device2Pk": ex.device2_pk,
            },
        )


class TestFixtureDevice:
    def test_deserialize(self):
        data, meta = _load_fixture("device")
        dev = Device.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": dev.account_type,
                "Owner": dev.owner,
                "Index": dev.index,
                "BumpSeed": dev.bump_seed,
                "DeviceType": dev.device_type,
                "PublicIp": dev.public_ip,
                "Status": dev.status,
                "Code": dev.code,
                "MgmtVrf": dev.mgmt_vrf,
                "ReferenceCount": dev.reference_count,
                "UsersCount": dev.users_count,
                "MaxUsers": dev.max_users,
                "DeviceHealth": dev.device_health,
                "DesiredStatus": dev.device_desired_status,
                "MetricsPublisherPk": dev.metrics_publisher_pub_key,
                "ContributorPk": dev.contributor_pub_key,
            },
        )
        # Verify interfaces
        assert len(dev.interfaces) == 2
        assert dev.interfaces[0].name == "Loopback0"
        assert dev.interfaces[1].name == "Ethernet1"
        # Verify dz_prefixes
        import ipaddress

        assert len(dev.dz_prefixes) == 1
        net = ipaddress.IPv4Network("10.10.0.0/24")
        expected_prefix = net.network_address.packed + bytes([net.prefixlen])
        assert dev.dz_prefixes[0] == expected_prefix
        # Verify code, mgmt_vrf, public_ip, index
        assert dev.code == "dz1"
        assert dev.mgmt_vrf == "mgmt"
        assert dev.public_ip == ipaddress.IPv4Address("203.0.113.1").packed
        assert dev.index == 7


class TestFixtureLink:
    def test_deserialize(self):
        data, meta = _load_fixture("link")
        lk = Link.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": lk.account_type,
                "Owner": lk.owner,
                "BumpSeed": lk.bump_seed,
                "LinkType": lk.link_type,
                "Bandwidth": lk.bandwidth,
                "Mtu": lk.mtu,
                "DelayNs": lk.delay_ns,
                "JitterNs": lk.jitter_ns,
                "TunnelId": lk.tunnel_id,
                "Status": lk.status,
                "ContributorPk": lk.contributor_pub_key,
                "DelayOverrideNs": lk.delay_override_ns,
                "LinkHealth": lk.link_health,
                "DesiredStatus": lk.link_desired_status,
            },
        )


class TestFixtureUser:
    def test_deserialize(self):
        data, meta = _load_fixture("user")
        u = User.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": u.account_type,
                "Owner": u.owner,
                "BumpSeed": u.bump_seed,
                "UserType": u.user_type,
                "TenantPk": u.tenant_pub_key,
                "DevicePk": u.device_pub_key,
                "CyoaType": u.cyoa_type,
                "TunnelId": u.tunnel_id,
                "Status": u.status,
                "ValidatorPubkey": u.validator_pub_key,
            },
        )


class TestFixtureMulticastGroup:
    def test_deserialize(self):
        data, meta = _load_fixture("multicast_group")
        mg = MulticastGroup.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": mg.account_type,
                "Owner": mg.owner,
                "BumpSeed": mg.bump_seed,
                "TenantPk": mg.tenant_pub_key,
                "MaxBandwidth": mg.max_bandwidth,
                "Status": mg.status,
                "PublisherCount": mg.publisher_count,
                "SubscriberCount": mg.subscriber_count,
            },
        )


class TestFixtureContributor:
    def test_deserialize(self):
        data, meta = _load_fixture("contributor")
        c = Contributor.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": c.account_type,
                "Owner": c.owner,
                "BumpSeed": c.bump_seed,
                "Status": c.status,
                "ReferenceCount": c.reference_count,
                "OpsManagerPk": c.ops_manager_pk,
            },
        )


class TestFixtureProgramConfig:
    def test_deserialize(self):
        data, meta = _load_fixture("program_config")
        pc = ProgramConfig.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": pc.account_type,
                "BumpSeed": pc.bump_seed,
                "VersionMajor": pc.version.major,
                "VersionMinor": pc.version.minor,
                "VersionPatch": pc.version.patch,
            },
        )


class TestFixtureAccessPass:
    def test_deserialize(self):
        data, meta = _load_fixture("access_pass")
        ap = AccessPass.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": ap.account_type,
                "Owner": ap.owner,
                "BumpSeed": ap.bump_seed,
                "AccessPassType": ap.access_pass_type_tag,
                "UserPayer": ap.user_payer,
                "LastAccessEpoch": ap.last_access_epoch,
                "ConnectionCount": ap.connection_count,
                "Status": ap.status,
                "Flags": ap.flags,
            },
        )


class TestFixtureAccessPassValidator:
    def test_deserialize(self):
        data, meta = _load_fixture("access_pass_validator")
        ap = AccessPass.from_bytes(data)
        _assert_fields(
            meta["fields"],
            {
                "AccountType": ap.account_type,
                "Owner": ap.owner,
                "BumpSeed": ap.bump_seed,
                "AccessPassType": ap.access_pass_type_tag,
                "AccessPassTypeValidatorPubkey": ap.associated_pubkey,
                "ClientIp": ap.client_ip,
                "UserPayer": ap.user_payer,
                "LastAccessEpoch": ap.last_access_epoch,
                "ConnectionCount": ap.connection_count,
                "Status": ap.status,
                "Flags": ap.flags,
            },
        )
        assert ap.account_type == 11
        assert ap.bump_seed == 243
        assert ap.access_pass_type_tag == 1
        assert ap.associated_pubkey == Pubkey.from_string(
            "BuP3jEYfnTCfB4UqQk9L37k2vaXsNuVsbWxrYbGDmL6s"
        )
        import ipaddress

        assert ap.client_ip == ipaddress.IPv4Address("10.0.0.50").packed
        assert ap.last_access_epoch == 1000
        assert ap.connection_count == 1
        assert ap.status == 1
        assert len(ap.mgroup_pub_allowlist) == 1
        assert len(ap.mgroup_sub_allowlist) == 1
        assert ap.flags == 3
