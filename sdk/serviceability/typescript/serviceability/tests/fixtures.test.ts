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

function assertFields(
  fields: FieldValue[],
  got: Record<string, unknown>,
): void {
  for (const f of fields) {
    if (!(f.name in got)) continue;
    const actual = got[f.name];
    if (f.typ === "u8" || f.typ === "u16" || f.typ === "u32") {
      expect(actual).toBe(Number(f.value));
    } else if (f.typ === "u64") {
      expect(actual).toBe(BigInt(f.value));
    } else if (f.typ === "pubkey") {
      expect((actual as PublicKey).toBase58()).toBe(f.value);
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
      BumpSeed: dev.bumpSeed,
      DeviceType: dev.deviceType,
      Status: dev.status,
      ReferenceCount: dev.referenceCount,
      UsersCount: dev.usersCount,
      MaxUsers: dev.maxUsers,
      DeviceHealth: dev.deviceHealth,
      DesiredStatus: dev.deviceDesiredStatus,
      MetricsPublisherPk: dev.metricsPublisherPubKey,
      ContributorPk: dev.contributorPubKey,
    });
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
