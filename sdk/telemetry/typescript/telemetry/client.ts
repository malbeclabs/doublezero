/** RPC client for fetching telemetry program accounts. */

import { Connection, PublicKey } from "@solana/web3.js";
import { PROGRAM_IDS, LEDGER_RPC_URLS } from "./config.js";
import { newConnection } from "./rpc.js";
import {
  deriveDeviceLatencySamplesPda,
  deriveInternetLatencySamplesPda,
} from "./pda.js";
import {
  deserializeDeviceLatencySamples,
  deserializeInternetLatencySamples,
  type DeviceLatencySamples,
  type InternetLatencySamples,
} from "./state.js";

export class Client {
  constructor(
    private connection: Connection,
    private programId: PublicKey,
  ) {}

  static mainnetBeta(): Client {
    return new Client(
      newConnection(LEDGER_RPC_URLS["mainnet-beta"]),
      new PublicKey(PROGRAM_IDS["mainnet-beta"]),
    );
  }

  static testnet(): Client {
    return new Client(
      newConnection(LEDGER_RPC_URLS["testnet"]),
      new PublicKey(PROGRAM_IDS["testnet"]),
    );
  }

  static devnet(): Client {
    return new Client(
      newConnection(LEDGER_RPC_URLS["devnet"]),
      new PublicKey(PROGRAM_IDS["devnet"]),
    );
  }

  static localnet(): Client {
    return new Client(
      newConnection(LEDGER_RPC_URLS["localnet"]),
      new PublicKey(PROGRAM_IDS["localnet"]),
    );
  }

  async getDeviceLatencySamples(
    originDevicePK: PublicKey,
    targetDevicePK: PublicKey,
    linkPK: PublicKey,
    epoch: number | bigint,
  ): Promise<DeviceLatencySamples> {
    const [addr] = deriveDeviceLatencySamplesPda(
      this.programId,
      originDevicePK,
      targetDevicePK,
      linkPK,
      epoch,
    );
    const info = await this.connection.getAccountInfo(addr);
    if (!info) throw new Error("Account not found");
    return deserializeDeviceLatencySamples(new Uint8Array(info.data));
  }

  async getInternetLatencySamples(
    collectorOraclePK: PublicKey,
    dataProviderName: string,
    originLocationPK: PublicKey,
    targetLocationPK: PublicKey,
    epoch: number | bigint,
  ): Promise<InternetLatencySamples> {
    const [addr] = deriveInternetLatencySamplesPda(
      this.programId,
      collectorOraclePK,
      dataProviderName,
      originLocationPK,
      targetLocationPK,
      epoch,
    );
    const info = await this.connection.getAccountInfo(addr);
    if (!info) throw new Error("Account not found");
    return deserializeInternetLatencySamples(new Uint8Array(info.data));
  }
}
