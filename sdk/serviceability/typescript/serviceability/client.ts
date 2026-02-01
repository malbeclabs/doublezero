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
}
