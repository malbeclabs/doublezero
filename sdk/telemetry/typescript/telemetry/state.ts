/** Account state types and deserialization for the telemetry program. */

import { PublicKey } from "@solana/web3.js";
import { DefensiveReader } from "borsh-incremental";

const DEVICE_LATENCY_HEADER_SIZE = 1 + 8 + 32 * 6 + 8 + 8 + 4 + 128;
const MAX_DEVICE_LATENCY_SAMPLES = 35_000;
const MAX_INTERNET_LATENCY_SAMPLES = 3_000;
const TIMESTAMP_INDEX_HEADER_SIZE = 1 + 32 + 4 + 64;
const TIMESTAMP_INDEX_ENTRY_SIZE = 4 + 8;
const MAX_TIMESTAMP_INDEX_ENTRIES = 10_000;

export interface DeviceLatencySamples {
  accountType: number;
  epoch: bigint;
  originDeviceAgentPK: PublicKey;
  originDevicePK: PublicKey;
  targetDevicePK: PublicKey;
  originDeviceLocationPK: PublicKey;
  targetDeviceLocationPK: PublicKey;
  linkPK: PublicKey;
  samplingIntervalMicroseconds: bigint;
  startTimestampMicroseconds: bigint;
  nextSampleIndex: number;
  samples: number[];
}

export interface InternetLatencySamples {
  accountType: number;
  epoch: bigint;
  dataProviderName: string;
  oracleAgentPK: PublicKey;
  originExchangePK: PublicKey;
  targetExchangePK: PublicKey;
  samplingIntervalMicroseconds: bigint;
  startTimestampMicroseconds: bigint;
  nextSampleIndex: number;
  samples: number[];
}

function readPubkey(r: DefensiveReader): PublicKey {
  return new PublicKey(r.readPubkeyRaw());
}

export function deserializeDeviceLatencySamples(
  data: Uint8Array,
): DeviceLatencySamples {
  if (data.length < DEVICE_LATENCY_HEADER_SIZE) {
    throw new Error(
      `data too short for device latency header: ${data.length} < ${DEVICE_LATENCY_HEADER_SIZE}`,
    );
  }

  const r = new DefensiveReader(data);

  const accountType = r.readU8();
  const epoch = r.readU64();
  const originDeviceAgentPK = readPubkey(r);
  const originDevicePK = readPubkey(r);
  const targetDevicePK = readPubkey(r);
  const originDeviceLocationPK = readPubkey(r);
  const targetDeviceLocationPK = readPubkey(r);
  const linkPK = readPubkey(r);
  const samplingIntervalMicroseconds = r.readU64();
  const startTimestampMicroseconds = r.readU64();
  const nextSampleIndex = r.readU32();

  r.readBytes(128); // _unused

  const count = Math.min(nextSampleIndex, MAX_DEVICE_LATENCY_SAMPLES);
  const samples: number[] = [];
  for (let i = 0; i < count; i++) {
    if (r.remaining < 4) break;
    samples.push(r.readU32());
  }

  return {
    accountType,
    epoch,
    originDeviceAgentPK,
    originDevicePK,
    targetDevicePK,
    originDeviceLocationPK,
    targetDeviceLocationPK,
    linkPK,
    samplingIntervalMicroseconds,
    startTimestampMicroseconds,
    nextSampleIndex,
    samples,
  };
}

export function deserializeInternetLatencySamples(
  data: Uint8Array,
): InternetLatencySamples {
  if (data.length < 10) {
    throw new Error("data too short");
  }

  const r = new DefensiveReader(data);

  const accountType = r.readU8();
  const epoch = r.readU64();
  const dataProviderName = r.readString();
  const oracleAgentPK = readPubkey(r);
  const originExchangePK = readPubkey(r);
  const targetExchangePK = readPubkey(r);
  const samplingIntervalMicroseconds = r.readU64();
  const startTimestampMicroseconds = r.readU64();
  const nextSampleIndex = r.readU32();

  r.readBytes(128); // _unused

  const count = Math.min(nextSampleIndex, MAX_INTERNET_LATENCY_SAMPLES);
  const samples: number[] = [];
  for (let i = 0; i < count; i++) {
    if (r.remaining < 4) break;
    samples.push(r.readU32());
  }

  return {
    accountType,
    epoch,
    dataProviderName,
    oracleAgentPK,
    originExchangePK,
    targetExchangePK,
    samplingIntervalMicroseconds,
    startTimestampMicroseconds,
    nextSampleIndex,
    samples,
  };
}

export interface TimestampIndexEntry {
  sampleIndex: number;
  timestampMicroseconds: bigint;
}

export interface TimestampIndex {
  accountType: number;
  samplesAccountPK: PublicKey;
  nextEntryIndex: number;
  entries: TimestampIndexEntry[];
}

export function deserializeTimestampIndex(
  data: Uint8Array,
): TimestampIndex {
  if (data.length < TIMESTAMP_INDEX_HEADER_SIZE) {
    throw new Error(
      `data too short for timestamp index header: ${data.length} < ${TIMESTAMP_INDEX_HEADER_SIZE}`,
    );
  }

  const r = new DefensiveReader(data);

  const accountType = r.readU8();
  const samplesAccountPK = readPubkey(r);
  const nextEntryIndex = r.readU32();

  r.readBytes(64); // _unused

  if (nextEntryIndex > MAX_TIMESTAMP_INDEX_ENTRIES) {
    throw new Error(
      `next_entry_index ${nextEntryIndex} exceeds max ${MAX_TIMESTAMP_INDEX_ENTRIES}`,
    );
  }

  const count = nextEntryIndex;
  if (r.remaining < count * TIMESTAMP_INDEX_ENTRY_SIZE) {
    throw new Error(
      `data too short for ${count} timestamp index entries: ${r.remaining} < ${count * TIMESTAMP_INDEX_ENTRY_SIZE}`,
    );
  }

  const entries: TimestampIndexEntry[] = [];
  for (let i = 0; i < count; i++) {
    entries.push({
      sampleIndex: r.readU32(),
      timestampMicroseconds: r.readU64(),
    });
  }

  return {
    accountType,
    samplesAccountPK,
    nextEntryIndex,
    entries,
  };
}

/**
 * Returns the wall-clock timestamp (microseconds) for a sample at the given
 * index. Uses binary search over entries — O(log m). Falls back to the
 * implicit model when no entries are available.
 */
export function reconstructTimestamp(
  entries: TimestampIndexEntry[],
  sampleIndex: number,
  startTimestampMicroseconds: bigint,
  samplingIntervalMicroseconds: bigint,
): bigint {
  if (entries.length === 0) {
    return startTimestampMicroseconds + BigInt(sampleIndex) * samplingIntervalMicroseconds;
  }

  // Binary search: find the last entry where sampleIndex <= target.
  let lo = 0;
  let hi = entries.length - 1;
  while (lo < hi) {
    const mid = lo + Math.ceil((hi - lo) / 2);
    if (entries[mid].sampleIndex <= sampleIndex) {
      lo = mid;
    } else {
      hi = mid - 1;
    }
  }

  const entry = entries[lo];
  if (entry.sampleIndex > sampleIndex) {
    return startTimestampMicroseconds + BigInt(sampleIndex) * samplingIntervalMicroseconds;
  }
  return entry.timestampMicroseconds + BigInt(sampleIndex - entry.sampleIndex) * samplingIntervalMicroseconds;
}

/**
 * Returns wall-clock timestamps (microseconds) for all samples.
 * Single-pass O(n + m) where n is sampleCount and m is the number of entries.
 */
export function reconstructTimestamps(
  sampleCount: number,
  entries: TimestampIndexEntry[],
  startTimestampMicroseconds: bigint,
  samplingIntervalMicroseconds: bigint,
): bigint[] {
  const timestamps: bigint[] = [];
  if (entries.length === 0) {
    for (let i = 0; i < sampleCount; i++) {
      timestamps.push(startTimestampMicroseconds + BigInt(i) * samplingIntervalMicroseconds);
    }
    return timestamps;
  }

  let entryIdx = 0;
  for (let i = 0; i < sampleCount; i++) {
    while (entryIdx + 1 < entries.length && entries[entryIdx + 1].sampleIndex <= i) {
      entryIdx++;
    }
    const e = entries[entryIdx];
    if (e.sampleIndex > i) {
      timestamps.push(startTimestampMicroseconds + BigInt(i) * samplingIntervalMicroseconds);
    } else {
      timestamps.push(e.timestampMicroseconds + BigInt(i - e.sampleIndex) * samplingIntervalMicroseconds);
    }
  }
  return timestamps;
}
