/**
 * Mainnet compatibility tests.
 *
 * These tests fetch live mainnet-beta data and verify that our struct
 * deserialization works against real on-chain accounts.
 *
 * Run with:
 *   REVDIST_COMPAT_TEST=1 cd sdk/revdist/typescript && bun test --grep compat
 *
 * Requires network access to Solana mainnet RPC.
 */

import { describe, expect, test, setDefaultTimeout } from "bun:test";

// Compat tests hit public RPC endpoints which may be slow or rate-limited.
setDefaultTimeout(30_000);
import { PublicKey } from "@solana/web3.js";
import { Client } from "../client.js";
import { SOLANA_RPC_URLS, LEDGER_RPC_URLS, PROGRAM_ID } from "../config.js";
import { newConnection } from "../rpc.js";
import {
  deriveConfigPda,
  deriveDistributionPda,
  deriveJournalPda,
} from "../pda.js";

function skipUnlessCompat(): void {
  if (!process.env.REVDIST_COMPAT_TEST) {
    throw new Error("SKIP");
  }
}

function readU8(buf: Buffer, offset: number): number {
  return buf.readUInt8(offset);
}

function readU16LE(buf: Buffer, offset: number): number {
  return buf.readUInt16LE(offset);
}

function readU32LE(buf: Buffer, offset: number): number {
  return buf.readUInt32LE(offset);
}

function readU64LE(buf: Buffer, offset: number): bigint {
  return buf.readBigUInt64LE(offset);
}

function readPubkey(buf: Buffer, offset: number): PublicKey {
  return new PublicKey(buf.subarray(offset, offset + 32));
}

function rpcUrl(): string {
  return process.env.SOLANA_RPC_URL || SOLANA_RPC_URLS["mainnet-beta"];
}

function compatClient(): Client {
  return new Client(
    newConnection(rpcUrl()),
    newConnection(LEDGER_RPC_URLS["mainnet-beta"]),
    new PublicKey(PROGRAM_ID),
  );
}

async function fetchRawAccount(addr: PublicKey): Promise<Buffer> {
  const conn = newConnection(rpcUrl());
  const info = await conn.getAccountInfo(addr);
  if (info === null) throw new Error(`account not found: ${addr.toBase58()}`);
  return info.data;
}

describe("compat: ProgramConfig", () => {
  test("deserialize matches raw bytes", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const client = compatClient();
    const config = await client.fetchConfig();

    const programId = new PublicKey(PROGRAM_ID);
    const [addr] = deriveConfigPda(programId);
    const raw = await fetchRawAccount(addr);

    expect(config.flags).toBe(readU64LE(raw, 8));
    expect(config.nextCompletedDzEpoch).toBe(readU64LE(raw, 16));
    expect(config.bumpSeed).toBe(readU8(raw, 24));
    expect(config.adminKey.toBase58()).toBe(readPubkey(raw, 32).toBase58());
    expect(config.debtAccountantKey.toBase58()).toBe(readPubkey(raw, 64).toBase58());
    expect(config.rewardsAccountantKey.toBase58()).toBe(readPubkey(raw, 96).toBase58());
    expect(config.contributorManagerKey.toBase58()).toBe(readPubkey(raw, 128).toBase58());
    expect(config.sol2zSwapProgramId.toBase58()).toBe(readPubkey(raw, 192).toBase58());

    // DistributionParameters at offset 224.
    const dp = config.distributionParameters;
    expect(dp.calculationGracePeriodMinutes).toBe(readU16LE(raw, 224));
    expect(dp.initializationGracePeriodMinutes).toBe(readU16LE(raw, 226));
    expect(dp.minimumEpochDurationToFinalizeRewards).toBe(readU8(raw, 228));

    // CommunityBurnRateParameters at offset 232.
    const cb = dp.communityBurnRateParameters;
    expect(cb.limit).toBe(readU32LE(raw, 232));
    expect(cb.dzEpochsToIncreasing).toBe(readU32LE(raw, 236));
    expect(cb.dzEpochsToLimit).toBe(readU32LE(raw, 240));

    // SolanaValidatorFeeParameters at offset 256.
    const vf = dp.solanaValidatorFeeParameters;
    expect(vf.baseBlockRewardsPct).toBe(readU16LE(raw, 256));
    expect(vf.priorityBlockRewardsPct).toBe(readU16LE(raw, 258));
    expect(vf.inflationRewardsPct).toBe(readU16LE(raw, 260));
    expect(vf.jitoTipsPct).toBe(readU16LE(raw, 262));
    expect(vf.fixedSolAmount).toBe(readU32LE(raw, 264));

    // RelayParameters at offset 552.
    const rp = config.relayParameters;
    expect(rp.placeholderLamports).toBe(readU32LE(raw, 552));
    expect(rp.distributeRewardsLamports).toBe(readU32LE(raw, 556));

    // DebtWriteOffFeatureActivationEpoch at offset 600.
    expect(config.debtWriteOffFeatureActivationEpoch).toBe(readU64LE(raw, 600));

    // Sanity.
    expect(config.nextCompletedDzEpoch > 0n).toBe(true);
  });
});

describe("compat: Distribution", () => {
  test("deserialize matches raw bytes", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const client = compatClient();
    const config = await client.fetchConfig();
    const epoch = config.nextCompletedDzEpoch - 1n;

    const dist = await client.fetchDistribution(epoch);

    const programId = new PublicKey(PROGRAM_ID);
    const [addr] = deriveDistributionPda(programId, epoch);
    const raw = await fetchRawAccount(addr);

    expect(dist.dzEpoch).toBe(readU64LE(raw, 8));
    expect(dist.dzEpoch).toBe(epoch);
    expect(dist.flags).toBe(readU64LE(raw, 16));
    expect(dist.communityBurnRate).toBe(readU32LE(raw, 24));

    const vf = dist.solanaValidatorFeeParameters;
    expect(vf.baseBlockRewardsPct).toBe(readU16LE(raw, 32));
    expect(vf.priorityBlockRewardsPct).toBe(readU16LE(raw, 34));
    expect(vf.inflationRewardsPct).toBe(readU16LE(raw, 36));
    expect(vf.jitoTipsPct).toBe(readU16LE(raw, 38));
    expect(vf.fixedSolAmount).toBe(readU32LE(raw, 40));

    expect(dist.totalSolanaValidators).toBe(readU32LE(raw, 104));
    expect(dist.solanaValidatorPaymentsCount).toBe(readU32LE(raw, 108));
    expect(dist.totalSolanaValidatorDebt).toBe(readU64LE(raw, 112));
    expect(dist.collectedSolanaValidatorPayments).toBe(readU64LE(raw, 120));
    expect(dist.totalContributors).toBe(readU32LE(raw, 160));
    expect(dist.distributedRewardsCount).toBe(readU32LE(raw, 164));
    expect(dist.collectedPrepaid2zPayments).toBe(readU64LE(raw, 168));
    expect(dist.collected2zConvertedFromSol).toBe(readU64LE(raw, 176));
    expect(dist.uncollectibleSolDebt).toBe(readU64LE(raw, 184));
    expect(dist.distributed2zAmount).toBe(readU64LE(raw, 216));
    expect(dist.burned2zAmount).toBe(readU64LE(raw, 224));
  });
});

describe("compat: Journal", () => {
  test("deserialize matches raw bytes", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const client = compatClient();
    const journal = await client.fetchJournal();

    const programId = new PublicKey(PROGRAM_ID);
    const [addr] = deriveJournalPda(programId);
    const raw = await fetchRawAccount(addr);

    expect(journal.bumpSeed).toBe(readU8(raw, 8));
    expect(journal.totalSolBalance).toBe(readU64LE(raw, 16));
    expect(journal.total2zBalance).toBe(readU64LE(raw, 24));
    expect(journal.swap2zDestinationBalance).toBe(readU64LE(raw, 32));
    expect(journal.swappedSolAmount).toBe(readU64LE(raw, 40));
    expect(journal.nextDzEpochToSweepTokens).toBe(readU64LE(raw, 48));
  });
});

describe("compat: ValidatorDebts", () => {
  test("fetch and validate", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const client = compatClient();
    const config = await client.fetchConfig();
    const epoch = config.nextCompletedDzEpoch - 5n;

    const debts = await client.fetchValidatorDebts(epoch);

    expect(debts.lastSolanaEpoch > 0n).toBe(true);
    expect(debts.firstSolanaEpoch <= debts.lastSolanaEpoch).toBe(true);
    expect(debts.debts.length).toBeGreaterThan(0);
  });
});

describe("compat: RewardShares", () => {
  test("fetch and validate", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const client = compatClient();
    const config = await client.fetchConfig();
    const epoch = config.nextCompletedDzEpoch - 5n;

    const shares = await client.fetchRewardShares(epoch);

    expect(shares.epoch).toBe(epoch);
    expect(shares.rewards.length).toBeGreaterThan(0);
    expect(shares.totalUnitShares).toBeGreaterThan(0);
  });
});

describe("compat: ValidatorDeposits", () => {
  test("fetch all and spot-check", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const client = compatClient();
    const deposits = await client.fetchAllValidatorDeposits();
    expect(deposits.length).toBeGreaterThan(0);

    // Verify single lookup matches list entry.
    const first = deposits[0];
    const single = await client.fetchValidatorDeposit(first.nodeId);
    expect(single.nodeId.toBase58()).toBe(first.nodeId.toBase58());
    expect(single.writtenOffSolDebt).toBe(first.writtenOffSolDebt);
  });
});

describe("compat: ContributorRewards", () => {
  test("fetch all and spot-check", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const client = compatClient();
    const rewards = await client.fetchAllContributorRewards();
    expect(rewards.length).toBeGreaterThan(0);

    // Verify single lookup matches list entry.
    const first = rewards[0];
    const single = await client.fetchContributorRewards(first.serviceKey);
    expect(single.serviceKey.toBase58()).toBe(first.serviceKey.toBase58());
    expect(single.rewardsManagerKey.toBase58()).toBe(first.rewardsManagerKey.toBase58());
    expect(single.flags).toBe(first.flags);
  });
});
