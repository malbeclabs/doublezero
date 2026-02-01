import { PublicKey } from "@solana/web3.js";

const SEED_PROGRAM_CONFIG = Buffer.from("program_config");
const SEED_DISTRIBUTION = Buffer.from("distribution");
const SEED_SOLANA_VALIDATOR_DEPOSIT = Buffer.from(
  "solana_validator_deposit",
);
const SEED_CONTRIBUTOR_REWARDS = Buffer.from("contributor_rewards");
const SEED_JOURNAL = Buffer.from("journal");
const SEED_SOLANA_VALIDATOR_DEBT = Buffer.from("solana_validator_debt");
const SEED_REWARD_SHARE = Buffer.from("reward_share");

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

export function deriveValidatorDebtPda(
  programId: PublicKey,
  epoch: bigint,
): [PublicKey, number] {
  const epochBuf = Buffer.alloc(8);
  epochBuf.writeBigUInt64LE(epoch);
  return PublicKey.findProgramAddressSync(
    [SEED_SOLANA_VALIDATOR_DEBT, epochBuf],
    programId,
  );
}

export function deriveRewardSharePda(
  programId: PublicKey,
  epoch: bigint,
): [PublicKey, number] {
  const epochBuf = Buffer.alloc(8);
  epochBuf.writeBigUInt64LE(epoch);
  return PublicKey.findProgramAddressSync(
    [SEED_REWARD_SHARE, epochBuf],
    programId,
  );
}
