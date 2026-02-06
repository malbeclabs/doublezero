/** PDA derivation for telemetry program accounts. */

import { PublicKey } from "@solana/web3.js";

const TELEMETRY_SEED = Buffer.from("telemetry");
const DEVICE_LATENCY_SEED = Buffer.from("dzlatency");
const INTERNET_LATENCY_SEED = Buffer.from("inetlatency");

export function deriveDeviceLatencySamplesPda(
  programId: PublicKey,
  originDevicePK: PublicKey,
  targetDevicePK: PublicKey,
  linkPK: PublicKey,
  epoch: number | bigint,
): [PublicKey, number] {
  const epochBuf = Buffer.alloc(8);
  epochBuf.writeBigUInt64LE(BigInt(epoch));

  return PublicKey.findProgramAddressSync(
    [
      TELEMETRY_SEED,
      DEVICE_LATENCY_SEED,
      originDevicePK.toBuffer(),
      targetDevicePK.toBuffer(),
      linkPK.toBuffer(),
      epochBuf,
    ],
    programId,
  );
}

export function deriveInternetLatencySamplesPda(
  programId: PublicKey,
  collectorOraclePK: PublicKey,
  dataProviderName: string,
  originLocationPK: PublicKey,
  targetLocationPK: PublicKey,
  epoch: number | bigint,
): [PublicKey, number] {
  const epochBuf = Buffer.alloc(8);
  epochBuf.writeBigUInt64LE(BigInt(epoch));

  return PublicKey.findProgramAddressSync(
    [
      TELEMETRY_SEED,
      INTERNET_LATENCY_SEED,
      collectorOraclePK.toBuffer(),
      Buffer.from(dataProviderName),
      originLocationPK.toBuffer(),
      targetLocationPK.toBuffer(),
      epochBuf,
    ],
    programId,
  );
}
