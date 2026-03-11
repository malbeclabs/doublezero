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

  const count = Math.min(nextEntryIndex, MAX_TIMESTAMP_INDEX_ENTRIES);
  const entries: TimestampIndexEntry[] = [];
  for (let i = 0; i < count; i++) {
    if (r.remaining < TIMESTAMP_INDEX_ENTRY_SIZE) break;
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
 * index, using timestamp index entries to correct for gaps. Falls back to the
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

  let entry = entries[0];
  for (const e of entries) {
    if (e.sampleIndex > sampleIndex) break;
    entry = e;
  }

  return entry.timestampMicroseconds + BigInt(sampleIndex - entry.sampleIndex) * samplingIntervalMicroseconds;
}

/**
 * Returns wall-clock timestamps (microseconds) for all samples.
 */
export function reconstructTimestamps(
  sampleCount: number,
  entries: TimestampIndexEntry[],
  startTimestampMicroseconds: bigint,
  samplingIntervalMicroseconds: bigint,
): bigint[] {
  const timestamps: bigint[] = [];
  for (let i = 0; i < sampleCount; i++) {
    timestamps.push(reconstructTimestamp(entries, i, startTimestampMicroseconds, samplingIntervalMicroseconds));
  }
  return timestamps;
}
