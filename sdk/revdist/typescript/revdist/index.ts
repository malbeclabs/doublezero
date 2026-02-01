export { PROGRAM_ID, SOLANA_RPC_URLS } from "./config.js";

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
} from "./state.js";

export {
  deserializeProgramConfig,
  deserializeDistribution,
  deserializeSolanaValidatorDeposit,
  deserializeContributorRewards,
  deserializeJournal,
  PROGRAM_CONFIG_STRUCT_SIZE,
  DISTRIBUTION_STRUCT_SIZE,
  SOLANA_VALIDATOR_DEPOSIT_STRUCT_SIZE,
  CONTRIBUTOR_REWARDS_STRUCT_SIZE,
  JOURNAL_STRUCT_SIZE,
} from "./state.js";

export {
  deriveConfigPda,
  deriveDistributionPda,
  deriveJournalPda,
  deriveValidatorDepositPda,
  deriveContributorRewardsPda,
} from "./pda.js";
