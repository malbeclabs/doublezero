export { PROGRAM_ID, SOLANA_RPC_URLS, LEDGER_RPC_URLS, ORACLE_URLS } from "./config.js";
export { Client } from "./client.js";
export { newConnection } from "./rpc.js";
export { OracleClient } from "./oracle.js";
export type { SwapRate } from "./oracle.js";

export {
  DISCRIMINATOR_PROGRAM_CONFIG,
  DISCRIMINATOR_DISTRIBUTION,
  DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT,
  DISCRIMINATOR_CONTRIBUTOR_REWARDS,
  DISCRIMINATOR_JOURNAL,
  validateDiscriminator,
} from "./discriminator.js";

export type {
  ProgramConfig,
  Distribution,
  SolanaValidatorDeposit,
  ContributorRewards,
  Journal,
  DistributionParameters,
  SolanaValidatorFeeParameters,
  CommunityBurnRateParameters,
  RelayParameters,
  RecipientShare,
  ComputedSolanaValidatorDebt,
  ComputedSolanaValidatorDebts,
  RewardShare,
  ShapleyOutputStorage,
} from "./state.js";

export {
  deserializeProgramConfig,
  deserializeDistribution,
  deserializeSolanaValidatorDeposit,
  deserializeContributorRewards,
  deserializeJournal,
  deserializeComputedSolanaValidatorDebts,
  deserializeShapleyOutputStorage,
  PROGRAM_CONFIG_STRUCT_SIZE,
  DISTRIBUTION_STRUCT_SIZE,
  SOLANA_VALIDATOR_DEPOSIT_STRUCT_SIZE,
  CONTRIBUTOR_REWARDS_STRUCT_SIZE,
  JOURNAL_STRUCT_SIZE,
} from "./state.js";

export {
  RECORD_HEADER_SIZE,
  RECORD_PROGRAM_ID,
  deriveConfigPda,
  deriveDistributionPda,
  deriveJournalPda,
  deriveValidatorDepositPda,
  deriveContributorRewardsPda,
  deriveRecordKey,
  deriveValidatorDebtRecordKey,
  deriveRewardShareRecordKey,
} from "./pda.js";
