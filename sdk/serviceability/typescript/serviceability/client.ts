/** Read-only client for serviceability program accounts. */

import { Connection, PublicKey } from "@solana/web3.js";
import { PROGRAM_IDS, LEDGER_RPC_URLS } from "./config.js";
import { newConnection } from "./rpc.js";

export class Client {
  private readonly connection: Connection;
  private readonly programId: PublicKey;

  constructor(connection: Connection, programId: PublicKey) {
    this.connection = connection;
    this.programId = programId;
  }

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
}
