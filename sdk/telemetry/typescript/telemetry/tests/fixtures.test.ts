/**
 * Fixture-based compatibility tests.
 */

import { describe, expect, test } from "bun:test";
import { readFileSync } from "fs";
import { join } from "path";
import { PublicKey } from "@solana/web3.js";
import {
  deserializeDeviceLatencySamples,
  deserializeInternetLatencySamples,
  deserializeTimestampIndex,
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
    } else if (f.typ === "string") {
      expect(actual).toBe(f.value);
    }
  }
}

describe("DeviceLatencySamples fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("device_latency_samples");
    const d = deserializeDeviceLatencySamples(data);
    assertFields(meta.fields, {
      AccountType: d.accountType,
      Epoch: d.epoch,
      OriginDeviceAgentPK: d.originDeviceAgentPK,
      OriginDevicePK: d.originDevicePK,
      TargetDevicePK: d.targetDevicePK,
      OriginDeviceLocationPK: d.originDeviceLocationPK,
      TargetDeviceLocationPK: d.targetDeviceLocationPK,
      LinkPK: d.linkPK,
      SamplingIntervalMicroseconds: d.samplingIntervalMicroseconds,
      StartTimestampMicroseconds: d.startTimestampMicroseconds,
      NextSampleIndex: d.nextSampleIndex,
      SamplesCount: d.samples.length,
    });
  });
});

describe("InternetLatencySamples fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("internet_latency_samples");
    const d = deserializeInternetLatencySamples(data);
    assertFields(meta.fields, {
      AccountType: d.accountType,
      Epoch: d.epoch,
      DataProviderName: d.dataProviderName,
      OracleAgentPK: d.oracleAgentPK,
      OriginExchangePK: d.originExchangePK,
      TargetExchangePK: d.targetExchangePK,
      SamplingIntervalMicroseconds: d.samplingIntervalMicroseconds,
      StartTimestampMicroseconds: d.startTimestampMicroseconds,
      NextSampleIndex: d.nextSampleIndex,
      SamplesCount: d.samples.length,
    });
  });
});

describe("TimestampIndex fixture", () => {
  test("deserialize", () => {
    const [data, meta] = loadFixture("timestamp_index");
    const d = deserializeTimestampIndex(data);
    const got: Record<string, unknown> = {
      AccountType: d.accountType,
      SamplesAccountPK: d.samplesAccountPK,
      NextEntryIndex: d.nextEntryIndex,
      EntriesCount: d.entries.length,
    };
    if (d.entries.length > 0) {
      got.Entry0SampleIndex = d.entries[0].sampleIndex;
      got.Entry0Timestamp = d.entries[0].timestampMicroseconds;
    }
    if (d.entries.length > 1) {
      got.Entry1SampleIndex = d.entries[1].sampleIndex;
      got.Entry1Timestamp = d.entries[1].timestampMicroseconds;
    }
    if (d.entries.length > 2) {
      got.Entry2SampleIndex = d.entries[2].sampleIndex;
      got.Entry2Timestamp = d.entries[2].timestampMicroseconds;
    }
    assertFields(meta.fields, got);
  });
});
