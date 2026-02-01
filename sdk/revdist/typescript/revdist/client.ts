/** Read-only client for revenue distribution program accounts. */

import { Connection, PublicKey } from "@solana/web3.js";
import { PROGRAM_ID, SOLANA_RPC_URLS, LEDGER_RPC_URLS } from "./config.js";
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
  deserializeComputedSolanaValidatorDebts,
  deserializeShapleyOutputStorage,
} from "./state.js";
import type {
  ProgramConfig,
  Distribution,
  Journal,
  SolanaValidatorDeposit,
  ContributorRewards,
  ComputedSolanaValidatorDebts,
  ShapleyOutputStorage,
} from "./state.js";
import {
  deriveConfigPda,
  deriveDistributionPda,
  deriveJournalPda,
  deriveValidatorDepositPda,
  deriveContributorRewardsPda,
  deriveValidatorDebtPda,
  deriveRewardSharePda,
} from "./pda.js";

export class Client {
  private readonly solanaConnection: Connection;
  private readonly ledgerConnection: Connection;
  private readonly programId: PublicKey;

  constructor(
    solanaConnection: Connection,
    ledgerConnection: Connection,
    programId: PublicKey,
  ) {
    this.solanaConnection = solanaConnection;
    this.ledgerConnection = ledgerConnection;
    this.programId = programId;
  }

  static mainnetBeta(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["mainnet-beta"]),
      new Connection(LEDGER_RPC_URLS["mainnet-beta"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  static testnet(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["testnet"]),
      new Connection(LEDGER_RPC_URLS["testnet"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  static devnet(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["devnet"]),
      new Connection(LEDGER_RPC_URLS["devnet"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  static localnet(): Client {
    return new Client(
      new Connection(SOLANA_RPC_URLS["localnet"]),
      new Connection(LEDGER_RPC_URLS["localnet"]),
      new PublicKey(PROGRAM_ID),
    );
  }

  // -- Solana RPC (on-chain accounts) --

  async fetchConfig(): Promise<ProgramConfig> {
    const [addr] = deriveConfigPda(this.programId);
    const data = await this.fetchSolanaAccountData(addr);
    return deserializeProgramConfig(data, DISCRIMINATOR_PROGRAM_CONFIG);
  }

  async fetchDistribution(epoch: bigint): Promise<Distribution> {
    const [addr] = deriveDistributionPda(this.programId, epoch);
    const data = await this.fetchSolanaAccountData(addr);
    return deserializeDistribution(data, DISCRIMINATOR_DISTRIBUTION);
  }

  async fetchJournal(): Promise<Journal> {
    const [addr] = deriveJournalPda(this.programId);
    const data = await this.fetchSolanaAccountData(addr);
    return deserializeJournal(data, DISCRIMINATOR_JOURNAL);
  }

  async fetchValidatorDeposit(
    nodeId: PublicKey,
  ): Promise<SolanaValidatorDeposit> {
    const [addr] = deriveValidatorDepositPda(this.programId, nodeId);
    const data = await this.fetchSolanaAccountData(addr);
    return deserializeSolanaValidatorDeposit(
      data,
      DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT,
    );
  }

  async fetchContributorRewards(
    serviceKey: PublicKey,
  ): Promise<ContributorRewards> {
    const [addr] = deriveContributorRewardsPda(this.programId, serviceKey);
    const data = await this.fetchSolanaAccountData(addr);
    return deserializeContributorRewards(
      data,
      DISCRIMINATOR_CONTRIBUTOR_REWARDS,
    );
  }

  // -- DZ Ledger RPC (ledger records) --

  async fetchValidatorDebts(
    epoch: bigint,
  ): Promise<ComputedSolanaValidatorDebts> {
    const [addr] = deriveValidatorDebtPda(this.programId, epoch);
    const data = await this.fetchLedgerRecordData(addr);
    return deserializeComputedSolanaValidatorDebts(data);
  }

  async fetchRewardShares(epoch: bigint): Promise<ShapleyOutputStorage> {
    const [addr] = deriveRewardSharePda(this.programId, epoch);
    const data = await this.fetchLedgerRecordData(addr);
    return deserializeShapleyOutputStorage(data);
  }

  // -- Internal helpers --

  private async fetchSolanaAccountData(addr: PublicKey): Promise<Buffer> {
    const info = await this.solanaConnection.getAccountInfo(addr);
    if (info === null) {
      throw new Error(`account not found: ${addr.toBase58()}`);
    }
    return info.data;
  }

  private async fetchLedgerRecordData(addr: PublicKey): Promise<Buffer> {
    const info = await this.ledgerConnection.getAccountInfo(addr);
    if (info === null) {
      throw new Error(`ledger record not found: ${addr.toBase58()}`);
    }
    return info.data;
  }
}
