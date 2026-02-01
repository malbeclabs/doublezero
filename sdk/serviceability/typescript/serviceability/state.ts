/** Account state types and Borsh deserialization for the serviceability program. */

import { PublicKey } from "@solana/web3.js";

// ---------------------------------------------------------------------------
// BorshReader — cursor-based binary reader (little-endian)
// ---------------------------------------------------------------------------

class BorshReader {
  private data: DataView;
  private offset: number;
  private raw: Uint8Array;

  constructor(data: Uint8Array) {
    this.raw = data;
    this.data = new DataView(data.buffer, data.byteOffset, data.byteLength);
    this.offset = 0;
  }

  get remaining(): number {
    return this.raw.byteLength - this.offset;
  }

  readU8(): number {
    const v = this.data.getUint8(this.offset);
    this.offset += 1;
    return v;
  }

  readU16(): number {
    const v = this.data.getUint16(this.offset, true);
    this.offset += 2;
    return v;
  }

  readU32(): number {
    const v = this.data.getUint32(this.offset, true);
    this.offset += 4;
    return v;
  }

  readU64(): bigint {
    const v = this.data.getBigUint64(this.offset, true);
    this.offset += 8;
    return v;
  }

  readU128(): bigint {
    const low = this.readU64();
    const high = this.readU64();
    return low | (high << 64n);
  }

  readF64(): number {
    const v = this.data.getFloat64(this.offset, true);
    this.offset += 8;
    return v;
  }

  readBool(): boolean {
    return this.readU8() !== 0;
  }

  readPubkey(): PublicKey {
    const bytes = this.raw.slice(this.offset, this.offset + 32);
    this.offset += 32;
    return new PublicKey(bytes);
  }

  readString(): string {
    const len = this.readU32();
    if (len === 0) return "";
    const bytes = this.raw.slice(this.offset, this.offset + len);
    this.offset += len;
    return new TextDecoder().decode(bytes);
  }

  readIPv4(): Uint8Array {
    const bytes = this.raw.slice(this.offset, this.offset + 4);
    this.offset += 4;
    return bytes;
  }

  readNetworkV4(): Uint8Array {
    const bytes = this.raw.slice(this.offset, this.offset + 5);
    this.offset += 5;
    return bytes;
  }

  readPubkeyVec(): PublicKey[] {
    const len = this.readU32();
    const result: PublicKey[] = [];
    for (let i = 0; i < len; i++) result.push(this.readPubkey());
    return result;
  }

  readNetworkV4Vec(): Uint8Array[] {
    const len = this.readU32();
    const result: Uint8Array[] = [];
    for (let i = 0; i < len; i++) result.push(this.readNetworkV4());
    return result;
  }
}

// ---------------------------------------------------------------------------
// Account type discriminants
// ---------------------------------------------------------------------------

export const ACCOUNT_TYPE_GLOBAL_STATE = 1;
export const ACCOUNT_TYPE_GLOBAL_CONFIG = 2;
export const ACCOUNT_TYPE_LOCATION = 3;
export const ACCOUNT_TYPE_EXCHANGE = 4;
export const ACCOUNT_TYPE_DEVICE = 5;
export const ACCOUNT_TYPE_LINK = 6;
export const ACCOUNT_TYPE_USER = 7;
export const ACCOUNT_TYPE_MULTICAST_GROUP = 8;
export const ACCOUNT_TYPE_PROGRAM_CONFIG = 9;
export const ACCOUNT_TYPE_CONTRIBUTOR = 10;
export const ACCOUNT_TYPE_ACCESS_PASS = 11;

// ---------------------------------------------------------------------------
// GlobalState
// ---------------------------------------------------------------------------

export interface GlobalState {
  accountType: number;
  bumpSeed: number;
  accountIndex: bigint;
  foundationAllowlist: PublicKey[];
  activatorAuthorityPk: PublicKey;
  sentinelAuthorityPk: PublicKey;
  contributorAirdropLamports: bigint;
  userAirdropLamports: bigint;
  healthOraclePk: PublicKey;
  qaAllowlist: PublicKey[];
}

export function deserializeGlobalState(data: Uint8Array): GlobalState {
  const r = new BorshReader(data);
  const accountType = r.readU8();
  const bumpSeed = r.readU8();
  const accountIndex = r.readU128();
  const foundationAllowlist = r.readPubkeyVec();
  r.readPubkeyVec(); // deprecated device_allowlist
  r.readPubkeyVec(); // deprecated user_allowlist
  const activatorAuthorityPk = r.readPubkey();
  const sentinelAuthorityPk = r.readPubkey();
  const contributorAirdropLamports = r.readU64();
  const userAirdropLamports = r.readU64();
  const healthOraclePk = r.readPubkey();
  const qaAllowlist = r.remaining >= 4 ? r.readPubkeyVec() : [];
  return {
    accountType,
    bumpSeed,
    accountIndex,
    foundationAllowlist,
    activatorAuthorityPk,
    sentinelAuthorityPk,
    contributorAirdropLamports,
    userAirdropLamports,
    healthOraclePk,
    qaAllowlist,
  };
}

// ---------------------------------------------------------------------------
// GlobalConfig
// ---------------------------------------------------------------------------

export interface GlobalConfig {
  accountType: number;
  owner: PublicKey;
  bumpSeed: number;
  localAsn: number;
  remoteAsn: number;
  deviceTunnelBlock: Uint8Array;
  userTunnelBlock: Uint8Array;
  multicastGroupBlock: Uint8Array;
  nextBgpCommunity: number;
}

export function deserializeGlobalConfig(data: Uint8Array): GlobalConfig {
  const r = new BorshReader(data);
  return {
    accountType: r.readU8(),
    owner: r.readPubkey(),
    bumpSeed: r.readU8(),
    localAsn: r.readU32(),
    remoteAsn: r.readU32(),
    deviceTunnelBlock: r.readNetworkV4(),
    userTunnelBlock: r.readNetworkV4(),
    multicastGroupBlock: r.readNetworkV4(),
    nextBgpCommunity: r.readU16(),
  };
}

// ---------------------------------------------------------------------------
// Location
// ---------------------------------------------------------------------------

export interface Location {
  accountType: number;
  owner: PublicKey;
  index: bigint;
  bumpSeed: number;
  lat: number;
  lng: number;
  locId: number;
  status: number;
  code: string;
  name: string;
  country: string;
  referenceCount: number;
}

export function deserializeLocation(data: Uint8Array): Location {
  const r = new BorshReader(data);
  return {
    accountType: r.readU8(),
    owner: r.readPubkey(),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    lat: r.readF64(),
    lng: r.readF64(),
    locId: r.readU32(),
    status: r.readU8(),
    code: r.readString(),
    name: r.readString(),
    country: r.readString(),
    referenceCount: r.readU32(),
  };
}

// ---------------------------------------------------------------------------
// Exchange
// ---------------------------------------------------------------------------

export interface Exchange {
  accountType: number;
  owner: PublicKey;
  index: bigint;
  bumpSeed: number;
  lat: number;
  lng: number;
  bgpCommunity: number;
  status: number;
  code: string;
  name: string;
  referenceCount: number;
  device1Pk: PublicKey;
  device2Pk: PublicKey;
}

export function deserializeExchange(data: Uint8Array): Exchange {
  const r = new BorshReader(data);
  const accountType = r.readU8();
  const owner = r.readPubkey();
  const index = r.readU128();
  const bumpSeed = r.readU8();
  const lat = r.readF64();
  const lng = r.readF64();
  const bgpCommunity = r.readU16();
  r.readU16(); // unused padding
  const status = r.readU8();
  const code = r.readString();
  const name = r.readString();
  const referenceCount = r.readU32();
  const device1Pk = r.readPubkey();
  const device2Pk = r.readPubkey();
  return {
    accountType,
    owner,
    index,
    bumpSeed,
    lat,
    lng,
    bgpCommunity,
    status,
    code,
    name,
    referenceCount,
    device1Pk,
    device2Pk,
  };
}

// ---------------------------------------------------------------------------
// Interface (versioned, embedded in Device)
// ---------------------------------------------------------------------------

export interface DeviceInterface {
  version: number;
  status: number;
  name: string;
  interfaceType: number;
  interfaceCyoa: number;
  interfaceDia: number;
  loopbackType: number;
  bandwidth: bigint;
  cir: bigint;
  mtu: number;
  routingMode: number;
  vlanId: number;
  ipNet: Uint8Array;
  nodeSegmentIdx: number;
  userTunnelEndpoint: boolean;
}

const CURRENT_INTERFACE_VERSION = 2;

function deserializeInterface(r: BorshReader): DeviceInterface {
  const iface: DeviceInterface = {
    version: 0,
    status: 0,
    name: "",
    interfaceType: 0,
    interfaceCyoa: 0,
    interfaceDia: 0,
    loopbackType: 0,
    bandwidth: 0n,
    cir: 0n,
    mtu: 0,
    routingMode: 0,
    vlanId: 0,
    ipNet: new Uint8Array(5),
    nodeSegmentIdx: 0,
    userTunnelEndpoint: false,
  };

  iface.version = r.readU8();
  if (iface.version > CURRENT_INTERFACE_VERSION - 1) {
    return iface;
  }

  if (iface.version === 0) {
    // V1
    iface.status = r.readU8();
    iface.name = r.readString();
    iface.interfaceType = r.readU8();
    iface.loopbackType = r.readU8();
    iface.vlanId = r.readU16();
    iface.ipNet = r.readNetworkV4();
    iface.nodeSegmentIdx = r.readU16();
    iface.userTunnelEndpoint = r.readBool();
  } else if (iface.version === 1) {
    // V2
    iface.status = r.readU8();
    iface.name = r.readString();
    iface.interfaceType = r.readU8();
    iface.interfaceCyoa = r.readU8();
    iface.interfaceDia = r.readU8();
    iface.loopbackType = r.readU8();
    iface.bandwidth = r.readU64();
    iface.cir = r.readU64();
    iface.mtu = r.readU16();
    iface.routingMode = r.readU8();
    iface.vlanId = r.readU16();
    iface.ipNet = r.readNetworkV4();
    iface.nodeSegmentIdx = r.readU16();
    iface.userTunnelEndpoint = r.readBool();
  }

  return iface;
}

// ---------------------------------------------------------------------------
// Device
// ---------------------------------------------------------------------------

export interface Device {
  accountType: number;
  owner: PublicKey;
  index: bigint;
  bumpSeed: number;
  locationPubKey: PublicKey;
  exchangePubKey: PublicKey;
  deviceType: number;
  publicIp: Uint8Array;
  status: number;
  code: string;
  dzPrefixes: Uint8Array[];
  metricsPublisherPubKey: PublicKey;
  contributorPubKey: PublicKey;
  mgmtVrf: string;
  interfaces: DeviceInterface[];
  referenceCount: number;
  usersCount: number;
  maxUsers: number;
  deviceHealth: number;
  deviceDesiredStatus: number;
}

export function deserializeDevice(data: Uint8Array): Device {
  const r = new BorshReader(data);
  const accountType = r.readU8();
  const owner = r.readPubkey();
  const index = r.readU128();
  const bumpSeed = r.readU8();
  const locationPubKey = r.readPubkey();
  const exchangePubKey = r.readPubkey();
  const deviceType = r.readU8();
  const publicIp = r.readIPv4();
  const status = r.readU8();
  const code = r.readString();
  const dzPrefixes = r.readNetworkV4Vec();
  const metricsPublisherPubKey = r.readPubkey();
  const contributorPubKey = r.readPubkey();
  const mgmtVrf = r.readString();

  const ifaceLen = r.readU32();
  const interfaces: DeviceInterface[] = [];
  for (let i = 0; i < ifaceLen; i++) {
    interfaces.push(deserializeInterface(r));
  }

  const referenceCount = r.readU32();
  const usersCount = r.readU16();
  const maxUsers = r.readU16();
  const deviceHealth = r.readU8();
  const deviceDesiredStatus = r.readU8();

  return {
    accountType,
    owner,
    index,
    bumpSeed,
    locationPubKey,
    exchangePubKey,
    deviceType,
    publicIp,
    status,
    code,
    dzPrefixes,
    metricsPublisherPubKey,
    contributorPubKey,
    mgmtVrf,
    interfaces,
    referenceCount,
    usersCount,
    maxUsers,
    deviceHealth,
    deviceDesiredStatus,
  };
}

// ---------------------------------------------------------------------------
// Link
// ---------------------------------------------------------------------------

export interface Link {
  accountType: number;
  owner: PublicKey;
  index: bigint;
  bumpSeed: number;
  sideAPubKey: PublicKey;
  sideZPubKey: PublicKey;
  linkType: number;
  bandwidth: bigint;
  mtu: number;
  delayNs: bigint;
  jitterNs: bigint;
  tunnelId: number;
  tunnelNet: Uint8Array;
  status: number;
  code: string;
  contributorPubKey: PublicKey;
  sideAIfaceName: string;
  sideZIfaceName: string;
  delayOverrideNs: bigint;
  linkHealth: number;
  linkDesiredStatus: number;
}

export function deserializeLink(data: Uint8Array): Link {
  const r = new BorshReader(data);
  return {
    accountType: r.readU8(),
    owner: r.readPubkey(),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    sideAPubKey: r.readPubkey(),
    sideZPubKey: r.readPubkey(),
    linkType: r.readU8(),
    bandwidth: r.readU64(),
    mtu: r.readU32(),
    delayNs: r.readU64(),
    jitterNs: r.readU64(),
    tunnelId: r.readU16(),
    tunnelNet: r.readNetworkV4(),
    status: r.readU8(),
    code: r.readString(),
    contributorPubKey: r.readPubkey(),
    sideAIfaceName: r.readString(),
    sideZIfaceName: r.readString(),
    delayOverrideNs: r.readU64(),
    linkHealth: r.readU8(),
    linkDesiredStatus: r.readU8(),
  };
}

// ---------------------------------------------------------------------------
// User
// ---------------------------------------------------------------------------

export interface User {
  accountType: number;
  owner: PublicKey;
  index: bigint;
  bumpSeed: number;
  userType: number;
  tenantPubKey: PublicKey;
  devicePubKey: PublicKey;
  cyoaType: number;
  clientIp: Uint8Array;
  dzIp: Uint8Array;
  tunnelId: number;
  tunnelNet: Uint8Array;
  status: number;
  publishers: PublicKey[];
  subscribers: PublicKey[];
  validatorPubKey: PublicKey;
}

export function deserializeUser(data: Uint8Array): User {
  const r = new BorshReader(data);
  return {
    accountType: r.readU8(),
    owner: r.readPubkey(),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    userType: r.readU8(),
    tenantPubKey: r.readPubkey(),
    devicePubKey: r.readPubkey(),
    cyoaType: r.readU8(),
    clientIp: r.readIPv4(),
    dzIp: r.readIPv4(),
    tunnelId: r.readU16(),
    tunnelNet: r.readNetworkV4(),
    status: r.readU8(),
    publishers: r.readPubkeyVec(),
    subscribers: r.readPubkeyVec(),
    validatorPubKey: r.readPubkey(),
  };
}

// ---------------------------------------------------------------------------
// MulticastGroup
// ---------------------------------------------------------------------------

export interface MulticastGroup {
  accountType: number;
  owner: PublicKey;
  index: bigint;
  bumpSeed: number;
  tenantPubKey: PublicKey;
  multicastIp: Uint8Array;
  maxBandwidth: bigint;
  status: number;
  code: string;
  publisherCount: number;
  subscriberCount: number;
}

export function deserializeMulticastGroup(data: Uint8Array): MulticastGroup {
  const r = new BorshReader(data);
  return {
    accountType: r.readU8(),
    owner: r.readPubkey(),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    tenantPubKey: r.readPubkey(),
    multicastIp: r.readIPv4(),
    maxBandwidth: r.readU64(),
    status: r.readU8(),
    code: r.readString(),
    publisherCount: r.readU32(),
    subscriberCount: r.readU32(),
  };
}

// ---------------------------------------------------------------------------
// ProgramConfig
// ---------------------------------------------------------------------------

export interface ProgramVersion {
  major: number;
  minor: number;
  patch: number;
}

export interface ProgramConfig {
  accountType: number;
  bumpSeed: number;
  version: ProgramVersion;
  minCompatVersion: ProgramVersion;
}

function deserializeProgramVersion(r: BorshReader): ProgramVersion {
  return {
    major: r.readU32(),
    minor: r.readU32(),
    patch: r.readU32(),
  };
}

export function deserializeProgramConfig(data: Uint8Array): ProgramConfig {
  const r = new BorshReader(data);
  return {
    accountType: r.readU8(),
    bumpSeed: r.readU8(),
    version: deserializeProgramVersion(r),
    minCompatVersion: deserializeProgramVersion(r),
  };
}

// ---------------------------------------------------------------------------
// Contributor
// ---------------------------------------------------------------------------

export interface Contributor {
  accountType: number;
  owner: PublicKey;
  index: bigint;
  bumpSeed: number;
  status: number;
  code: string;
  referenceCount: number;
  opsManagerPk: PublicKey;
}

export function deserializeContributor(data: Uint8Array): Contributor {
  const r = new BorshReader(data);
  return {
    accountType: r.readU8(),
    owner: r.readPubkey(),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    status: r.readU8(),
    code: r.readString(),
    referenceCount: r.readU32(),
    opsManagerPk: r.readPubkey(),
  };
}

// ---------------------------------------------------------------------------
// AccessPass
// ---------------------------------------------------------------------------

export interface AccessPass {
  accountType: number;
  owner: PublicKey;
  bumpSeed: number;
  accessPassType: number;
  validatorPubKey: PublicKey | null;
  clientIp: Uint8Array;
  userPayer: PublicKey;
  lastAccessEpoch: bigint;
  connectionCount: number;
  status: number;
  mGroupPubAllowlist: PublicKey[];
  mGroupSubAllowlist: PublicKey[];
  flags: number;
}

export function deserializeAccessPass(data: Uint8Array): AccessPass {
  const r = new BorshReader(data);
  const accountType = r.readU8();
  const owner = r.readPubkey();
  const bumpSeed = r.readU8();
  const accessPassType = r.readU8();
  let validatorPubKey: PublicKey | null = null;
  if (accessPassType === 1) {
    // SolanaValidator
    validatorPubKey = r.readPubkey();
  }
  const clientIp = r.readIPv4();
  const userPayer = r.readPubkey();
  const lastAccessEpoch = r.readU64();
  const connectionCount = r.readU16();
  const status = r.readU8();
  const mGroupPubAllowlist = r.readPubkeyVec();
  const mGroupSubAllowlist = r.readPubkeyVec();
  const flags = r.readU8();
  return {
    accountType,
    owner,
    bumpSeed,
    accessPassType,
    validatorPubKey,
    clientIp,
    userPayer,
    lastAccessEpoch,
    connectionCount,
    status,
    mGroupPubAllowlist,
    mGroupSubAllowlist,
    flags,
  };
}
