/** Account state types and deserialization for the telemetry program. */

import { PublicKey } from "@solana/web3.js";

const DEVICE_LATENCY_HEADER_SIZE = 1 + 8 + 32 * 6 + 8 + 8 + 4 + 128;
const MAX_DEVICE_LATENCY_SAMPLES = 35_000;
const MAX_INTERNET_LATENCY_SAMPLES = 3_000;

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

function readPubkey(dv: DataView, raw: Uint8Array, off: number): PublicKey {
  return new PublicKey(raw.slice(off, off + 32));
}

export function deserializeDeviceLatencySamples(
  data: Uint8Array,
): DeviceLatencySamples {
  if (data.length < DEVICE_LATENCY_HEADER_SIZE) {
    throw new Error(
      `data too short for device latency header: ${data.length} < ${DEVICE_LATENCY_HEADER_SIZE}`,
    );
  }

  const dv = new DataView(data.buffer, data.byteOffset, data.byteLength);
  let off = 0;

  const accountType = dv.getUint8(off);
  off += 1;

  const epoch = dv.getBigUint64(off, true);
  off += 8;

  const originDeviceAgentPK = readPubkey(dv, data, off);
  off += 32;
  const originDevicePK = readPubkey(dv, data, off);
  off += 32;
  const targetDevicePK = readPubkey(dv, data, off);
  off += 32;
  const originDeviceLocationPK = readPubkey(dv, data, off);
  off += 32;
  const targetDeviceLocationPK = readPubkey(dv, data, off);
  off += 32;
  const linkPK = readPubkey(dv, data, off);
  off += 32;

  const samplingIntervalMicroseconds = dv.getBigUint64(off, true);
  off += 8;
  const startTimestampMicroseconds = dv.getBigUint64(off, true);
  off += 8;
  const nextSampleIndex = dv.getUint32(off, true);
  off += 4;

  off += 128; // _unused

  const count = Math.min(nextSampleIndex, MAX_DEVICE_LATENCY_SAMPLES);
  const samples: number[] = [];
  for (let i = 0; i < count; i++) {
    if (off + 4 > data.length) break;
    samples.push(dv.getUint32(off, true));
    off += 4;
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

  const dv = new DataView(data.buffer, data.byteOffset, data.byteLength);
  let off = 0;

  const accountType = dv.getUint8(off);
  off += 1;

  const epoch = dv.getBigUint64(off, true);
  off += 8;

  // Borsh string: 4-byte LE length + UTF-8
  const nameLen = dv.getUint32(off, true);
  off += 4;
  const dataProviderName = new TextDecoder().decode(
    data.slice(off, off + nameLen),
  );
  off += nameLen;

  const oracleAgentPK = readPubkey(dv, data, off);
  off += 32;
  const originExchangePK = readPubkey(dv, data, off);
  off += 32;
  const targetExchangePK = readPubkey(dv, data, off);
  off += 32;

  const samplingIntervalMicroseconds = dv.getBigUint64(off, true);
  off += 8;
  const startTimestampMicroseconds = dv.getBigUint64(off, true);
  off += 8;
  const nextSampleIndex = dv.getUint32(off, true);
  off += 4;

  off += 128; // _unused

  const count = Math.min(nextSampleIndex, MAX_INTERNET_LATENCY_SAMPLES);
  const samples: number[] = [];
  for (let i = 0; i < count; i++) {
    if (off + 4 > data.length) break;
    samples.push(dv.getUint32(off, true));
    off += 4;
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
