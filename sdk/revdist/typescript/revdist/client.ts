/** Read-only client for revenue distribution program accounts. */

import { Connection, PublicKey } from "@solana/web3.js";
import { PROGRAM_ID, SOLANA_RPC_URLS } from "./config.js";
import {
  DISCRIMINATOR_PROGRAM_CONFIG,
  DISCRIMINATOR_DISTRIBUTION,
  DISCRIMINATOR_JOURNAL,
  DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT,
  DISCRIMINATOR_CONTRIBUTOR_REWARDS,
} from "./discriminator.js";
import {
  deserializeProgramConfig,
  deserializeDistribution,
  deserializeJournal,
  deserializeSolanaValidatorDeposit,
  deserializeContributorRewards,
} from "./state.js";
import type {
  ProgramConfig,
  Distribution,
  Journal,
  SolanaValidatorDeposit,
  ContributorRewards,
} from "./state.js";
import {
  deriveConfigPda,
  deriveDistributionPda,
  deriveJournalPda,
  deriveValidatorDepositPda,
  deriveContributorRewardsPda,
} from "./pda.js";

export class Client {
  private readonly connection: Connection;
  private readonly programId: PublicKey;

  constructor(connection: Connection, programId: PublicKey) {
    this.connection = connection;
    this.programId = programId;
  }

  static mainnetBeta(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["mainnet-beta"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  static testnet(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["testnet"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  static devnet(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["devnet"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  static localnet(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["localnet"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  async fetchConfig(): Promise<ProgramConfig> {
    const [addr] = deriveConfigPda(this.programId);
    const data = await this.fetchAccountData(addr);
    return deserializeProgramConfig(data, DISCRIMINATOR_PROGRAM_CONFIG);
  }

  async fetchDistribution(epoch: number): Promise<Distribution> {
    const [addr] = deriveDistributionPda(this.programId, epoch);
    const data = await this.fetchAccountData(addr);
    return deserializeDistribution(data, DISCRIMINATOR_DISTRIBUTION);
  }

  async fetchJournal(): Promise<Journal> {
    const [addr] = deriveJournalPda(this.programId);
    const data = await this.fetchAccountData(addr);
    return deserializeJournal(data, DISCRIMINATOR_JOURNAL);
  }

  async fetchValidatorDeposit(
    nodeId: PublicKey,
  ): Promise<SolanaValidatorDeposit> {
    const [addr] = deriveValidatorDepositPda(this.programId, nodeId);
    const data = await this.fetchAccountData(addr);
    return deserializeSolanaValidatorDeposit(
      data,
      DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT,
    );
  }

  async fetchContributorRewards(
    serviceKey: PublicKey,
  ): Promise<ContributorRewards> {
    const [addr] = deriveContributorRewardsPda(this.programId, serviceKey);
    const data = await this.fetchAccountData(addr);
    return deserializeContributorRewards(
      data,
      DISCRIMINATOR_CONTRIBUTOR_REWARDS,
    );
  }

  private async fetchAccountData(addr: PublicKey): Promise<Buffer> {
    const info = await this.connection.getAccountInfo(addr);
    if (info === null) {
      throw new Error(`account not found: ${addr.toBase58()}`);
    }
    return info.data;
  }
}
