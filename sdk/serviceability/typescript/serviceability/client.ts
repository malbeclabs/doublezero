/** Read-only client for serviceability program accounts. */

import { Connection, PublicKey } from "@solana/web3.js";
import { PROGRAM_IDS, LEDGER_RPC_URLS } from "./config.js";
import { newConnection } from "./rpc.js";
import {
  ACCOUNT_TYPE_GLOBAL_STATE,
  ACCOUNT_TYPE_GLOBAL_CONFIG,
  ACCOUNT_TYPE_LOCATION,
  ACCOUNT_TYPE_EXCHANGE,
  ACCOUNT_TYPE_DEVICE,
  ACCOUNT_TYPE_LINK,
  ACCOUNT_TYPE_USER,
  ACCOUNT_TYPE_MULTICAST_GROUP,
  ACCOUNT_TYPE_PROGRAM_CONFIG,
  ACCOUNT_TYPE_CONTRIBUTOR,
  ACCOUNT_TYPE_ACCESS_PASS,
  deserializeGlobalState,
  deserializeGlobalConfig,
  deserializeLocation,
  deserializeExchange,
  deserializeDevice,
  deserializeLink,
  deserializeUser,
  deserializeMulticastGroup,
  deserializeProgramConfig,
  deserializeContributor,
  deserializeAccessPass,
  type GlobalState,
  type GlobalConfig,
  type Location,
  type Exchange,
  type Device,
  type Link,
  type User,
  type MulticastGroup,
  type ProgramConfig,
  type Contributor,
  type AccessPass,
} from "./state.js";

export interface ProgramData {
  globalState: GlobalState | null;
  globalConfig: GlobalConfig | null;
  programConfig: ProgramConfig | null;
  locations: Location[];
  exchanges: Exchange[];
  devices: Device[];
  links: Link[];
  users: User[];
  multicastGroups: MulticastGroup[];
  contributors: Contributor[];
  accessPasses: AccessPass[];
}

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

  /** Fetch all program accounts and deserialize them by type. */
  async getProgramData(): Promise<ProgramData> {
    const accounts = await this.connection.getProgramAccounts(this.programId);

    const pd: ProgramData = {
      globalState: null,
      globalConfig: null,
      programConfig: null,
      locations: [],
      exchanges: [],
      devices: [],
      links: [],
      users: [],
      multicastGroups: [],
      contributors: [],
      accessPasses: [],
    };

    for (const { account } of accounts) {
      const data = account.data;
      if (data.length === 0) continue;

      const accountType = data[0];

      switch (accountType) {
        case ACCOUNT_TYPE_GLOBAL_STATE:
          pd.globalState = deserializeGlobalState(data);
          break;
        case ACCOUNT_TYPE_GLOBAL_CONFIG:
          pd.globalConfig = deserializeGlobalConfig(data);
          break;
        case ACCOUNT_TYPE_LOCATION:
          pd.locations.push(deserializeLocation(data));
          break;
        case ACCOUNT_TYPE_EXCHANGE:
          pd.exchanges.push(deserializeExchange(data));
          break;
        case ACCOUNT_TYPE_DEVICE:
          pd.devices.push(deserializeDevice(data));
          break;
        case ACCOUNT_TYPE_LINK:
          pd.links.push(deserializeLink(data));
          break;
        case ACCOUNT_TYPE_USER:
          pd.users.push(deserializeUser(data));
          break;
        case ACCOUNT_TYPE_MULTICAST_GROUP:
          pd.multicastGroups.push(deserializeMulticastGroup(data));
          break;
        case ACCOUNT_TYPE_PROGRAM_CONFIG:
          pd.programConfig = deserializeProgramConfig(data);
          break;
        case ACCOUNT_TYPE_CONTRIBUTOR:
          pd.contributors.push(deserializeContributor(data));
          break;
        case ACCOUNT_TYPE_ACCESS_PASS:
          pd.accessPasses.push(deserializeAccessPass(data));
          break;
      }
    }

    return pd;
  }
}
