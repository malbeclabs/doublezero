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

  /** Create a client configured for the given environment. */
  static forEnv(env: string): Client {
    return new Client(
      newConnection(LEDGER_RPC_URLS[env]),
      new PublicKey(PROGRAM_IDS[env]),
    );
  }

  static mainnetBeta(): Client {
    return Client.forEnv("mainnet-beta");
  }

  static testnet(): Client {
    return Client.forEnv("testnet");
  }

  static devnet(): Client {
    return Client.forEnv("devnet");
  }

  static localnet(): Client {
    return Client.forEnv("localnet");
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
