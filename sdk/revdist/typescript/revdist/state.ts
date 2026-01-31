/**
 * On-chain account data structures for the revenue distribution program.
 *
 * Binary layout matches Rust #[repr(C)] structs. Deserialization uses
 * DataView with little-endian byte order and tolerates extra trailing
 * bytes for forward compatibility.
 */

import { PublicKey } from "@solana/web3.js";
import { DISCRIMINATOR_SIZE, validateDiscriminator } from "./discriminator.js";

function readPubkey(dv: DataView, offset: number): PublicKey {
  const bytes = new Uint8Array(dv.buffer, dv.byteOffset + offset, 32);
  return new PublicKey(bytes);
}

function deserializeBody(
  data: Uint8Array,
  discriminator: Uint8Array,
  minSize: number,
): DataView {
  validateDiscriminator(data, discriminator);
  const body = data.slice(DISCRIMINATOR_SIZE);
  if (body.length < minSize) {
    throw new Error(
      `account data too short: have ${body.length} bytes, need at least ${minSize}`,
    );
  }
  return new DataView(body.buffer, body.byteOffset, body.byteLength);
}

// ---------------------------------------------------------------------------
// Nested types
// ---------------------------------------------------------------------------

export interface CommunityBurnRateParameters {
  limit: number;
  dzEpochsToIncreasing: number;
  dzEpochsToLimit: number;
  cachedSlopeNumerator: number;
  cachedSlopeDenominator: number;
  cachedNextBurnRate: number;
}

function deserializeCommunityBurnRateParameters(
  dv: DataView,
  offset: number,
): CommunityBurnRateParameters {
  return {
    limit: dv.getUint32(offset, true),
    dzEpochsToIncreasing: dv.getUint32(offset + 4, true),
    dzEpochsToLimit: dv.getUint32(offset + 8, true),
    cachedSlopeNumerator: dv.getUint32(offset + 12, true),
    cachedSlopeDenominator: dv.getUint32(offset + 16, true),
    cachedNextBurnRate: dv.getUint32(offset + 20, true),
  };
}

export interface SolanaValidatorFeeParameters {
  baseBlockRewardsPct: number;
  priorityBlockRewardsPct: number;
  inflationRewardsPct: number;
  jitoTipsPct: number;
  fixedSolAmount: number;
}

const SOLANA_VALIDATOR_FEE_PARAMETERS_SIZE = 40;

function deserializeSolanaValidatorFeeParameters(
  dv: DataView,
  offset: number,
): SolanaValidatorFeeParameters {
  return {
    baseBlockRewardsPct: dv.getUint16(offset, true),
    priorityBlockRewardsPct: dv.getUint16(offset + 2, true),
    inflationRewardsPct: dv.getUint16(offset + 4, true),
    jitoTipsPct: dv.getUint16(offset + 6, true),
    fixedSolAmount: dv.getUint32(offset + 8, true),
  };
}

export interface DistributionParameters {
  calculationGracePeriodMinutes: number;
  initializationGracePeriodMinutes: number;
  minimumEpochDurationToFinalizeRewards: number;
  communityBurnRateParameters: CommunityBurnRateParameters;
  solanaValidatorFeeParameters: SolanaValidatorFeeParameters;
}

const DISTRIBUTION_PARAMETERS_SIZE = 328;

function deserializeDistributionParameters(
  dv: DataView,
  offset: number,
): DistributionParameters {
  return {
    calculationGracePeriodMinutes: dv.getUint16(offset, true),
    initializationGracePeriodMinutes: dv.getUint16(offset + 2, true),
    minimumEpochDurationToFinalizeRewards: dv.getUint8(offset + 4),
    // 3 bytes padding
    communityBurnRateParameters: deserializeCommunityBurnRateParameters(
      dv,
      offset + 8,
    ),
    solanaValidatorFeeParameters: deserializeSolanaValidatorFeeParameters(
      dv,
      offset + 8 + 24,
    ),
  };
}

export interface RelayParameters {
  placeholderLamports: number;
  distributeRewardsLamports: number;
}

const RELAY_PARAMETERS_SIZE = 40;

function deserializeRelayParameters(
  dv: DataView,
  offset: number,
): RelayParameters {
  return {
    placeholderLamports: dv.getUint32(offset, true),
    distributeRewardsLamports: dv.getUint32(offset + 4, true),
  };
}

export interface RecipientShare {
  recipientKey: PublicKey;
  share: number;
}

const RECIPIENT_SHARE_SIZE = 34;

// ---------------------------------------------------------------------------
// Top-level account types
// ---------------------------------------------------------------------------

export interface ProgramConfig {
  flags: bigint;
  nextCompletedDzEpoch: bigint;
  bumpSeed: number;
  reserve2zBumpSeed: number;
  swapAuthorityBumpSeed: number;
  swapDestination2zBumpSeed: number;
  withdrawSolAuthorityBumpSeed: number;
  adminKey: PublicKey;
  debtAccountantKey: PublicKey;
  rewardsAccountantKey: PublicKey;
  contributorManagerKey: PublicKey;
  placeholderKey: PublicKey;
  sol2zSwapProgramId: PublicKey;
  distributionParameters: DistributionParameters;
  relayParameters: RelayParameters;
  lastInitializedDistributionTimestamp: number;
  debtWriteOffFeatureActivationEpoch: bigint;
}

export const PROGRAM_CONFIG_STRUCT_SIZE = 600;

export function deserializeProgramConfig(
  data: Uint8Array,
  discriminator: Uint8Array,
): ProgramConfig {
  const dv = deserializeBody(data, discriminator, PROGRAM_CONFIG_STRUCT_SIZE);
  let off = 0;
  const flags = dv.getBigUint64(off, true);
  off += 8;
  const nextCompletedDzEpoch = dv.getBigUint64(off, true);
  off += 8;
  const bumpSeed = dv.getUint8(off);
  const reserve2zBumpSeed = dv.getUint8(off + 1);
  const swapAuthorityBumpSeed = dv.getUint8(off + 2);
  const swapDestination2zBumpSeed = dv.getUint8(off + 3);
  const withdrawSolAuthorityBumpSeed = dv.getUint8(off + 4);
  off += 8; // 5 bytes + 3 padding
  const adminKey = readPubkey(dv, off);
  off += 32;
  const debtAccountantKey = readPubkey(dv, off);
  off += 32;
  const rewardsAccountantKey = readPubkey(dv, off);
  off += 32;
  const contributorManagerKey = readPubkey(dv, off);
  off += 32;
  const placeholderKey = readPubkey(dv, off);
  off += 32;
  const sol2zSwapProgramId = readPubkey(dv, off);
  off += 32;
  const distributionParameters = deserializeDistributionParameters(dv, off);
  off += DISTRIBUTION_PARAMETERS_SIZE;
  const relayParameters = deserializeRelayParameters(dv, off);
  off += RELAY_PARAMETERS_SIZE;
  const lastInitializedDistributionTimestamp = dv.getUint32(off, true);
  off += 4;
  off += 4; // 4 bytes padding
  const debtWriteOffFeatureActivationEpoch = dv.getBigUint64(off, true);

  return {
    flags,
    nextCompletedDzEpoch,
    bumpSeed,
    reserve2zBumpSeed,
    swapAuthorityBumpSeed,
    swapDestination2zBumpSeed,
    withdrawSolAuthorityBumpSeed,
    adminKey,
    debtAccountantKey,
    rewardsAccountantKey,
    contributorManagerKey,
    placeholderKey,
    sol2zSwapProgramId,
    distributionParameters,
    relayParameters,
    lastInitializedDistributionTimestamp,
    debtWriteOffFeatureActivationEpoch,
  };
}

export interface Distribution {
  dzEpoch: bigint;
  flags: bigint;
  communityBurnRate: number;
  bumpSeed: number;
  token2zPdaBumpSeed: number;
  solanaValidatorFeeParameters: SolanaValidatorFeeParameters;
  solanaValidatorDebtMerkleRoot: Uint8Array;
  totalSolanaValidators: number;
  solanaValidatorPaymentsCount: number;
  totalSolanaValidatorDebt: bigint;
  collectedSolanaValidatorPayments: bigint;
  rewardsMerkleRoot: Uint8Array;
  totalContributors: number;
  distributedRewardsCount: number;
  collectedPrepaid2zPayments: bigint;
  collected2zConvertedFromSol: bigint;
  uncollectibleSolDebt: bigint;
  processedSvDebtStartIndex: number;
  processedSvDebtEndIndex: number;
  processedRewardsStartIndex: number;
  processedRewardsEndIndex: number;
  distributeRewardsRelayLamports: number;
  calculationAllowedTimestamp: number;
  distributed2zAmount: bigint;
  burned2zAmount: bigint;
  processedSvDebtWoStartIndex: number;
  processedSvDebtWoEndIndex: number;
  solanaValidatorWriteOffCount: number;
}

export const DISTRIBUTION_STRUCT_SIZE = 448;

export function deserializeDistribution(
  data: Uint8Array,
  discriminator: Uint8Array,
): Distribution {
  const dv = deserializeBody(data, discriminator, DISTRIBUTION_STRUCT_SIZE);
  let off = 0;
  const dzEpoch = dv.getBigUint64(off, true);
  off += 8;
  const flags = dv.getBigUint64(off, true);
  off += 8;
  const communityBurnRate = dv.getUint32(off, true);
  off += 4;
  const bumpSeed = dv.getUint8(off);
  const token2zPdaBumpSeed = dv.getUint8(off + 1);
  off += 4; // 2 bytes + 2 padding
  const solanaValidatorFeeParameters =
    deserializeSolanaValidatorFeeParameters(dv, off);
  off += SOLANA_VALIDATOR_FEE_PARAMETERS_SIZE;
  const solanaValidatorDebtMerkleRoot = new Uint8Array(
    dv.buffer,
    dv.byteOffset + off,
    32,
  );
  off += 32;
  const totalSolanaValidators = dv.getUint32(off, true);
  off += 4;
  const solanaValidatorPaymentsCount = dv.getUint32(off, true);
  off += 4;
  const totalSolanaValidatorDebt = dv.getBigUint64(off, true);
  off += 8;
  const collectedSolanaValidatorPayments = dv.getBigUint64(off, true);
  off += 8;
  const rewardsMerkleRoot = new Uint8Array(
    dv.buffer,
    dv.byteOffset + off,
    32,
  );
  off += 32;
  const totalContributors = dv.getUint32(off, true);
  off += 4;
  const distributedRewardsCount = dv.getUint32(off, true);
  off += 4;
  const collectedPrepaid2zPayments = dv.getBigUint64(off, true);
  off += 8;
  const collected2zConvertedFromSol = dv.getBigUint64(off, true);
  off += 8;
  const uncollectibleSolDebt = dv.getBigUint64(off, true);
  off += 8;
  const processedSvDebtStartIndex = dv.getUint32(off, true);
  off += 4;
  const processedSvDebtEndIndex = dv.getUint32(off, true);
  off += 4;
  const processedRewardsStartIndex = dv.getUint32(off, true);
  off += 4;
  const processedRewardsEndIndex = dv.getUint32(off, true);
  off += 4;
  const distributeRewardsRelayLamports = dv.getUint32(off, true);
  off += 4;
  const calculationAllowedTimestamp = dv.getUint32(off, true);
  off += 4;
  const distributed2zAmount = dv.getBigUint64(off, true);
  off += 8;
  const burned2zAmount = dv.getBigUint64(off, true);
  off += 8;
  const processedSvDebtWoStartIndex = dv.getUint32(off, true);
  off += 4;
  const processedSvDebtWoEndIndex = dv.getUint32(off, true);
  off += 4;
  const solanaValidatorWriteOffCount = dv.getUint32(off, true);

  return {
    dzEpoch,
    flags,
    communityBurnRate,
    bumpSeed,
    token2zPdaBumpSeed,
    solanaValidatorFeeParameters,
    solanaValidatorDebtMerkleRoot,
    totalSolanaValidators,
    solanaValidatorPaymentsCount,
    totalSolanaValidatorDebt,
    collectedSolanaValidatorPayments,
    rewardsMerkleRoot,
    totalContributors,
    distributedRewardsCount,
    collectedPrepaid2zPayments,
    collected2zConvertedFromSol,
    uncollectibleSolDebt,
    processedSvDebtStartIndex,
    processedSvDebtEndIndex,
    processedRewardsStartIndex,
    processedRewardsEndIndex,
    distributeRewardsRelayLamports,
    calculationAllowedTimestamp,
    distributed2zAmount,
    burned2zAmount,
    processedSvDebtWoStartIndex,
    processedSvDebtWoEndIndex,
    solanaValidatorWriteOffCount,
  };
}

export interface SolanaValidatorDeposit {
  nodeId: PublicKey;
  writtenOffSolDebt: bigint;
}

export const SOLANA_VALIDATOR_DEPOSIT_STRUCT_SIZE = 96;

export function deserializeSolanaValidatorDeposit(
  data: Uint8Array,
  discriminator: Uint8Array,
): SolanaValidatorDeposit {
  const dv = deserializeBody(
    data,
    discriminator,
    SOLANA_VALIDATOR_DEPOSIT_STRUCT_SIZE,
  );
  return {
    nodeId: readPubkey(dv, 0),
    writtenOffSolDebt: dv.getBigUint64(32, true),
  };
}

export interface ContributorRewards {
  rewardsManagerKey: PublicKey;
  serviceKey: PublicKey;
  flags: bigint;
  recipientShares: RecipientShare[];
}

export const CONTRIBUTOR_REWARDS_STRUCT_SIZE = 600;

export function deserializeContributorRewards(
  data: Uint8Array,
  discriminator: Uint8Array,
): ContributorRewards {
  const dv = deserializeBody(
    data,
    discriminator,
    CONTRIBUTOR_REWARDS_STRUCT_SIZE,
  );
  const rewardsManagerKey = readPubkey(dv, 0);
  const serviceKey = readPubkey(dv, 32);
  const flags = dv.getBigUint64(64, true);
  const recipientShares: RecipientShare[] = [];
  let off = 72;
  for (let i = 0; i < 8; i++) {
    recipientShares.push({
      recipientKey: readPubkey(dv, off),
      share: dv.getUint16(off + 32, true),
    });
    off += RECIPIENT_SHARE_SIZE;
  }
  return { rewardsManagerKey, serviceKey, flags, recipientShares };
}

export interface Journal {
  bumpSeed: number;
  token2zPdaBumpSeed: number;
  totalSolBalance: bigint;
  total2zBalance: bigint;
  swap2zDestinationBalance: bigint;
  swappedSolAmount: bigint;
  nextDzEpochToSweepTokens: bigint;
  lifetimeSwapped2zAmount: Uint8Array;
}

export const JOURNAL_STRUCT_SIZE = 64;

export function deserializeJournal(
  data: Uint8Array,
  discriminator: Uint8Array,
): Journal {
  const dv = deserializeBody(data, discriminator, JOURNAL_STRUCT_SIZE);
  const bumpSeed = dv.getUint8(0);
  const token2zPdaBumpSeed = dv.getUint8(1);
  // 6 bytes padding
  const totalSolBalance = dv.getBigUint64(8, true);
  const total2zBalance = dv.getBigUint64(16, true);
  const swap2zDestinationBalance = dv.getBigUint64(24, true);
  const swappedSolAmount = dv.getBigUint64(32, true);
  const nextDzEpochToSweepTokens = dv.getBigUint64(40, true);
  const lifetimeSwapped2zAmount = new Uint8Array(
    dv.buffer,
    dv.byteOffset + 48,
    16,
  );
  return {
    bumpSeed,
    token2zPdaBumpSeed,
    totalSolBalance,
    total2zBalance,
    swap2zDestinationBalance,
    swappedSolAmount,
    nextDzEpochToSweepTokens,
    lifetimeSwapped2zAmount,
  };
}
