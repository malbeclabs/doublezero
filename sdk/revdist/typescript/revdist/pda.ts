import { createHash } from "crypto";
import { PublicKey } from "@solana/web3.js";
// @ts-ignore - no type declarations available
import bs58 from "bs58";

const SEED_PROGRAM_CONFIG = Buffer.from("program_config");
const SEED_DISTRIBUTION = Buffer.from("distribution");
const SEED_SOLANA_VALIDATOR_DEPOSIT = Buffer.from(
  "solana_validator_deposit",
);
const SEED_CONTRIBUTOR_REWARDS = Buffer.from("contributor_rewards");
const SEED_JOURNAL = Buffer.from("journal");
const SEED_SOLANA_VALIDATOR_DEBT = Buffer.from("solana_validator_debt");
const SEED_DZ_CONTRIBUTOR_REWARDS = Buffer.from("dz_contributor_rewards");
const SEED_SHAPLEY_OUTPUT = Buffer.from("shapley_output");

export const RECORD_PROGRAM_ID = new PublicKey(
  "dzrecxigtaZQ3gPmt2X5mDkYigaruFR1rHCqztFTvx7",
);
export const RECORD_HEADER_SIZE = 33;

function createRecordSeedString(seeds: Buffer[]): string {
  const h = createHash("sha256");
  for (const s of seeds) {
    h.update(s);
  }
  return bs58.encode(h.digest()).slice(0, 32);
}

export async function deriveRecordKey(
  payerKey: PublicKey,
  seeds: Buffer[],
): Promise<PublicKey> {
  const seedStr = createRecordSeedString(seeds);
  return PublicKey.createWithSeed(payerKey, seedStr, RECORD_PROGRAM_ID);
}

export function deriveConfigPda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_PROGRAM_CONFIG], programId);
}

export function deriveDistributionPda(
  programId: PublicKey,
  epoch: bigint,
): [PublicKey, number] {
  const epochBuf = Buffer.alloc(8);
  epochBuf.writeBigUInt64LE(epoch);
  return PublicKey.findProgramAddressSync(
    [SEED_DISTRIBUTION, epochBuf],
    programId,
  );
}

export function deriveJournalPda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_JOURNAL], programId);
}

export function deriveValidatorDepositPda(
  programId: PublicKey,
  nodeId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync(
    [SEED_SOLANA_VALIDATOR_DEPOSIT, nodeId.toBuffer()],
    programId,
  );
}

export function deriveContributorRewardsPda(
  programId: PublicKey,
  serviceKey: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync(
    [SEED_CONTRIBUTOR_REWARDS, serviceKey.toBuffer()],
    programId,
  );
}

export async function deriveValidatorDebtRecordKey(
  debtAccountantKey: PublicKey,
  epoch: bigint,
): Promise<PublicKey> {
  const epochBuf = Buffer.alloc(8);
  epochBuf.writeBigUInt64LE(epoch);
  return deriveRecordKey(debtAccountantKey, [
    SEED_SOLANA_VALIDATOR_DEBT,
    epochBuf,
  ]);
}

export async function deriveRewardShareRecordKey(
  rewardsAccountantKey: PublicKey,
  epoch: bigint,
): Promise<PublicKey> {
  const epochBuf = Buffer.alloc(8);
  epochBuf.writeBigUInt64LE(epoch);
  return deriveRecordKey(rewardsAccountantKey, [
    SEED_DZ_CONTRIBUTOR_REWARDS,
    epochBuf,
    SEED_SHAPLEY_OUTPUT,
  ]);
}
