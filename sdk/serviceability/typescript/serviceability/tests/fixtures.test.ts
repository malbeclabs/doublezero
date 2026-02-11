/**
 * Fixture-based compatibility tests.
 */

import { describe, expect, test } from "bun:test";
import { readFileSync } from "fs";
import { join } from "path";
import { PublicKey } from "@solana/web3.js";
import {
  deserializeGlobalState,
  deserializeGlobalConfig,
  deserializeLocation,
  deserializeExchange,
  deserializeDevice,
  deserializeLink,
  deserializeUser,
  deserializeMulticastGroup,
  deserializeProgramConfig,
  deserializeContributor,
  deserializeAccessPass,
  deserializeTenant,
} from "../state.js";

const FIXTURES_DIR = join(
  __dirname,
  "..",
  "..",
  "..",
  "testdata",
  "fixtures",
);

interface FieldValue {
  name: string;
  value: string;
  typ: string;
}

interface FixtureMeta {
  name: string;
  account_type: number;
  fields: FieldValue[];
}

function loadFixture(name: string): [Uint8Array, FixtureMeta] {
  const binData = new Uint8Array(
    readFileSync(join(FIXTURES_DIR, `${name}.bin`)),
  );
  const meta: FixtureMeta = JSON.parse(
    readFileSync(join(FIXTURES_DIR, `${name}.json`), "utf-8"),
  );
  return [binData, meta];
}

function formatIPv4(bytes: Uint8Array): string {
  return `${bytes[0]}.${bytes[1]}.${bytes[2]}.${bytes[3]}`;
}

function formatNetworkV4(bytes: Uint8Array): string {
  return `${bytes[0]}.${bytes[1]}.${bytes[2]}.${bytes[3]}/${bytes[4]}`;
}

function assertFields(
  fields: FieldValue[],
  got: Record<string, unknown>,
): void {
  for (const f of fields) {
    if (!(f.name in got)) continue;
    const actual = got[f.name];
    if (f.typ === "u8" || f.typ === "u16" || f.typ === "u32") {
      expect(actual).toBe(Number(f.value));
    } else if (f.typ === "u64" || f.typ === "u128") {
      expect(actual).toBe(BigInt(f.value));
    } else if (f.typ === "pubkey") {
      expect((actual as PublicKey).toBase58()).toBe(f.value);
    } else if (f.typ === "string") {
      expect(actual).toBe(f.value);
    } else if (f.typ === "bool") {
      expect(actual).toBe(f.value === "true");
    }
  }
}

describe("GlobalState fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("global_state");
    const gs = deserializeGlobalState(data);
    assertFields(meta.fields, {
      AccountType: gs.accountType,
      BumpSeed: gs.bumpSeed,
      ContributorAirdropLamports: gs.contributorAirdropLamports,
      UserAirdropLamports: gs.userAirdropLamports,
      ActivatorAuthorityPk: gs.activatorAuthorityPk,
      SentinelAuthorityPk: gs.sentinelAuthorityPk,
      HealthOraclePk: gs.healthOraclePk,
    });
  });
});

describe("GlobalConfig fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("global_config");
    const gc = deserializeGlobalConfig(data);
    assertFields(meta.fields, {
      AccountType: gc.accountType,
      Owner: gc.owner,
      BumpSeed: gc.bumpSeed,
      LocalAsn: gc.localAsn,
      RemoteAsn: gc.remoteAsn,
      NextBgpCommunity: gc.nextBgpCommunity,
    });
  });
});

describe("Location fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("location");
    const loc = deserializeLocation(data);
    assertFields(meta.fields, {
      AccountType: loc.accountType,
      Owner: loc.owner,
      BumpSeed: loc.bumpSeed,
      LocId: loc.locId,
      Status: loc.status,
      ReferenceCount: loc.referenceCount,
    });
  });
});

describe("Exchange fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("exchange");
    const ex = deserializeExchange(data);
    assertFields(meta.fields, {
      AccountType: ex.accountType,
      Owner: ex.owner,
      BumpSeed: ex.bumpSeed,
      BgpCommunity: ex.bgpCommunity,
      Status: ex.status,
      ReferenceCount: ex.referenceCount,
      Device1Pk: ex.device1Pk,
      Device2Pk: ex.device2Pk,
    });
  });
});

describe("Device fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("device");
    const dev = deserializeDevice(data);
    assertFields(meta.fields, {
      AccountType: dev.accountType,
      Owner: dev.owner,
      Index: dev.index,
      BumpSeed: dev.bumpSeed,
      DeviceType: dev.deviceType,
      Status: dev.status,
      Code: dev.code,
      MgmtVrf: dev.mgmtVrf,
      ReferenceCount: dev.referenceCount,
      UsersCount: dev.usersCount,
      MaxUsers: dev.maxUsers,
      DeviceHealth: dev.deviceHealth,
      DesiredStatus: dev.deviceDesiredStatus,
      MetricsPublisherPk: dev.metricsPublisherPubKey,
      ContributorPk: dev.contributorPubKey,
    });

    // index
    expect(dev.index).toBe(7n);

    // publicIp
    expect(formatIPv4(dev.publicIp)).toBe("203.0.113.1");

    // code
    expect(dev.code).toBe("dz1");

    // mgmtVrf
    expect(dev.mgmtVrf).toBe("mgmt");

    // dzPrefixes
    expect(dev.dzPrefixes).toHaveLength(1);
    expect(formatNetworkV4(dev.dzPrefixes[0])).toBe("10.10.0.0/24");

    // interfaces
    expect(dev.interfaces).toHaveLength(2);

    // Interface 0 (V1 format, version byte 0)
    const iface0 = dev.interfaces[0];
    expect(iface0.version).toBe(0);
    expect(iface0.status).toBe(3);
    expect(iface0.name).toBe("Loopback0");
    expect(iface0.interfaceType).toBe(1);
    expect(iface0.loopbackType).toBe(1);
    expect(iface0.vlanId).toBe(0);
    expect(formatNetworkV4(iface0.ipNet)).toBe("10.0.0.1/32");
    expect(iface0.nodeSegmentIdx).toBe(100);
    expect(iface0.userTunnelEndpoint).toBe(false);

    // Interface 1 (V2 format, version byte 1)
    const iface1 = dev.interfaces[1];
    expect(iface1.version).toBe(1);
    expect(iface1.status).toBe(3);
    expect(iface1.name).toBe("Ethernet1");
    expect(iface1.interfaceType).toBe(2);
    expect(iface1.interfaceCyoa).toBe(1);
    expect(iface1.interfaceDia).toBe(1);
    expect(iface1.loopbackType).toBe(0);
    expect(iface1.bandwidth).toBe(10000000000n);
    expect(iface1.cir).toBe(5000000000n);
    expect(iface1.mtu).toBe(9000);
    expect(iface1.routingMode).toBe(1);
    expect(iface1.vlanId).toBe(100);
    expect(formatNetworkV4(iface1.ipNet)).toBe("172.16.0.1/30");
    expect(iface1.nodeSegmentIdx).toBe(200);
    expect(iface1.userTunnelEndpoint).toBe(true);
  });
});

describe("Link fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("link");
    const lk = deserializeLink(data);
    assertFields(meta.fields, {
      AccountType: lk.accountType,
      Owner: lk.owner,
      BumpSeed: lk.bumpSeed,
      LinkType: lk.linkType,
      Bandwidth: lk.bandwidth,
      Mtu: lk.mtu,
      DelayNs: lk.delayNs,
      JitterNs: lk.jitterNs,
      TunnelId: lk.tunnelId,
      Status: lk.status,
      ContributorPk: lk.contributorPubKey,
      DelayOverrideNs: lk.delayOverrideNs,
      LinkHealth: lk.linkHealth,
      DesiredStatus: lk.linkDesiredStatus,
    });
  });
});

describe("User fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("user");
    const u = deserializeUser(data);
    assertFields(meta.fields, {
      AccountType: u.accountType,
      Owner: u.owner,
      BumpSeed: u.bumpSeed,
      UserType: u.userType,
      TenantPk: u.tenantPubKey,
      DevicePk: u.devicePubKey,
      CyoaType: u.cyoaType,
      TunnelId: u.tunnelId,
      Status: u.status,
      ValidatorPubkey: u.validatorPubKey,
    });
  });
});

describe("MulticastGroup fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("multicast_group");
    const mg = deserializeMulticastGroup(data);
    assertFields(meta.fields, {
      AccountType: mg.accountType,
      Owner: mg.owner,
      BumpSeed: mg.bumpSeed,
      TenantPk: mg.tenantPubKey,
      MaxBandwidth: mg.maxBandwidth,
      Status: mg.status,
      PublisherCount: mg.publisherCount,
      SubscriberCount: mg.subscriberCount,
    });
  });
});

describe("ProgramConfig fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("program_config");
    const pc = deserializeProgramConfig(data);
    assertFields(meta.fields, {
      AccountType: pc.accountType,
      BumpSeed: pc.bumpSeed,
      VersionMajor: pc.version.major,
      VersionMinor: pc.version.minor,
      VersionPatch: pc.version.patch,
    });
  });
});

describe("Contributor fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("contributor");
    const c = deserializeContributor(data);
    assertFields(meta.fields, {
      AccountType: c.accountType,
      Owner: c.owner,
      BumpSeed: c.bumpSeed,
      Status: c.status,
      ReferenceCount: c.referenceCount,
      OpsManagerPk: c.opsManagerPk,
    });
  });
});

describe("Tenant fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("tenant");
    const t = deserializeTenant(data);
    assertFields(meta.fields, {
      AccountType: t.accountType,
      Owner: t.owner,
      BumpSeed: t.bumpSeed,
      Code: t.code,
      VrfId: t.vrfId,
      ReferenceCount: t.referenceCount,
      PaymentStatus: t.paymentStatus,
      TokenAccount: t.tokenAccount,
      MetroRoute: t.metroRoute,
      RouteAliveness: t.routeAliveness,
    });
  });
});

describe("AccessPass fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("access_pass");
    const ap = deserializeAccessPass(data);
    assertFields(meta.fields, {
      AccountType: ap.accountType,
      Owner: ap.owner,
      BumpSeed: ap.bumpSeed,
      AccessPassType: ap.accessPassType,
      UserPayer: ap.userPayer,
      LastAccessEpoch: ap.lastAccessEpoch,
      ConnectionCount: ap.connectionCount,
      Status: ap.status,
      Flags: ap.flags,
    });
  });
});

describe("AccessPassValidator fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("access_pass_validator");
    const ap = deserializeAccessPass(data);
    assertFields(meta.fields, {
      AccountType: ap.accountType,
      Owner: ap.owner,
      BumpSeed: ap.bumpSeed,
      AccessPassType: ap.accessPassType,
      AccessPassTypeValidatorPubkey: ap.associatedPubkey,
      UserPayer: ap.userPayer,
      LastAccessEpoch: ap.lastAccessEpoch,
      ConnectionCount: ap.connectionCount,
      Status: ap.status,
      Flags: ap.flags,
    });

    // Verify specific values
    expect(ap.accountType).toBe(11);
    expect(ap.bumpSeed).toBe(243);
    expect(ap.accessPassType).toBe(1);
    expect(ap.associatedPubkey).not.toBeNull();
    expect(ap.associatedPubkey!.toBase58()).toBe(
      "BuP3jEYfnTCfB4UqQk9L37k2vaXsNuVsbWxrYbGDmL6s",
    );
    expect(formatIPv4(ap.clientIp)).toBe("10.0.0.50");
    expect(ap.lastAccessEpoch).toBe(1000n);
    expect(ap.connectionCount).toBe(1);
    expect(ap.status).toBe(1);
    expect(ap.flags).toBe(3);

    // Allowlists
    expect(ap.mGroupPubAllowlist).toHaveLength(1);
    expect(ap.mGroupPubAllowlist[0].toBase58()).toBe(
      "ByHTNjGkgHhNakbovFQmw3VGBb6e5rbnBPGk3naDV8mD",
    );
    expect(ap.mGroupSubAllowlist).toHaveLength(1);
    expect(ap.mGroupSubAllowlist[0].toBase58()).toBe(
      "C3Bs2Dzqa8C5zSinRkgDpyEVSbfQnohgmFadYytDCwRZ",
    );
  });
});
