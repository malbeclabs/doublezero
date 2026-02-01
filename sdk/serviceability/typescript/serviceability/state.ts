/** Account state types and Borsh deserialization for the serviceability program. */

import { PublicKey } from "@solana/web3.js";
import { IncrementalReader } from "@doublezero/borsh-incremental";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function readPubkey(r: IncrementalReader): PublicKey {
  return new PublicKey(r.readPubkeyRaw());
}

function readPubkeyVec(r: IncrementalReader): PublicKey[] {
  return r.readPubkeyRawVec().map((b) => new PublicKey(b));
}

function tryReadPubkeyVec(r: IncrementalReader): PublicKey[] {
  return r.tryReadPubkeyRawVec([]).map((b) => new PublicKey(b));
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
// Enum string mappings
// ---------------------------------------------------------------------------

const LOCATION_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  2: "suspended",
};
export function locationStatusString(v: number): string {
  return LOCATION_STATUS_NAMES[v] ?? "unknown";
}

const EXCHANGE_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  2: "suspended",
};
export function exchangeStatusString(v: number): string {
  return EXCHANGE_STATUS_NAMES[v] ?? "unknown";
}

const DEVICE_DEVICE_TYPE_NAMES: Record<number, string> = {
  0: "hybrid",
  1: "transit",
  2: "edge",
};
export function deviceDeviceTypeString(v: number): string {
  return DEVICE_DEVICE_TYPE_NAMES[v] ?? "unknown";
}

const DEVICE_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  2: "deleting",
  3: "rejected",
  4: "drained",
  5: "device-provisioning",
  6: "link-provisioning",
};
export function deviceStatusString(v: number): string {
  return DEVICE_STATUS_NAMES[v] ?? "unknown";
}

const DEVICE_HEALTH_NAMES: Record<number, string> = {
  0: "unknown",
  1: "pending",
  2: "ready_for_links",
  3: "ready_for_users",
  4: "impaired",
};
export function deviceHealthString(v: number): string {
  return DEVICE_HEALTH_NAMES[v] ?? "unknown";
}

const DEVICE_DESIRED_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  6: "drained",
};
export function deviceDesiredStatusString(v: number): string {
  return DEVICE_DESIRED_STATUS_NAMES[v] ?? "unknown";
}

const INTERFACE_STATUS_NAMES: Record<number, string> = {
  0: "invalid",
  1: "unmanaged",
  2: "pending",
  3: "activated",
  4: "deleting",
  5: "rejecting",
  6: "unlinked",
};
export function interfaceStatusString(v: number): string {
  return INTERFACE_STATUS_NAMES[v] ?? "unknown";
}

const INTERFACE_TYPE_NAMES: Record<number, string> = {
  0: "invalid",
  1: "loopback",
  2: "physical",
};
export function interfaceTypeString(v: number): string {
  return INTERFACE_TYPE_NAMES[v] ?? "unknown";
}

const LOOPBACK_TYPE_NAMES: Record<number, string> = {
  0: "none",
  1: "vpnv4",
  2: "ipv4",
  3: "pim_rp_addr",
  4: "reserved",
};
export function loopbackTypeString(v: number): string {
  return LOOPBACK_TYPE_NAMES[v] ?? "unknown";
}

const INTERFACE_CYOA_NAMES: Record<number, string> = {
  0: "none",
  1: "gre_over_dia",
  2: "gre_over_fabric",
  3: "gre_over_private_peering",
  4: "gre_over_public_peering",
  5: "gre_over_cable",
};
export function interfaceCYOAString(v: number): string {
  return INTERFACE_CYOA_NAMES[v] ?? "unknown";
}

const INTERFACE_DIA_NAMES: Record<number, string> = {
  0: "none",
  1: "dia",
};
export function interfaceDIAString(v: number): string {
  return INTERFACE_DIA_NAMES[v] ?? "unknown";
}

const ROUTING_MODE_NAMES: Record<number, string> = {
  0: "static",
  1: "bgp",
};
export function routingModeString(v: number): string {
  return ROUTING_MODE_NAMES[v] ?? "unknown";
}

const LINK_LINK_TYPE_NAMES: Record<number, string> = {
  1: "WAN",
  127: "DZX",
};
export function linkLinkTypeString(v: number): string {
  return LINK_LINK_TYPE_NAMES[v] ?? "";
}

const LINK_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  2: "deleting",
  3: "rejected",
  4: "requested",
  5: "hard-drained",
  6: "soft-drained",
  7: "provisioning",
};
export function linkStatusString(v: number): string {
  return LINK_STATUS_NAMES[v] ?? "unknown";
}

const LINK_HEALTH_NAMES: Record<number, string> = {
  0: "unknown",
  1: "pending",
  2: "ready_for_service",
  3: "impaired",
};
export function linkHealthString(v: number): string {
  return LINK_HEALTH_NAMES[v] ?? "unknown";
}

const LINK_DESIRED_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  2: "hard-drained",
  3: "soft-drained",
};
export function linkDesiredStatusString(v: number): string {
  return LINK_DESIRED_STATUS_NAMES[v] ?? "unknown";
}

const CONTRIBUTOR_STATUS_NAMES: Record<number, string> = {
  0: "none",
  1: "activated",
  2: "suspended",
  3: "deleting",
};
export function contributorStatusString(v: number): string {
  return CONTRIBUTOR_STATUS_NAMES[v] ?? "unknown";
}

const USER_USER_TYPE_NAMES: Record<number, string> = {
  0: "ibrl",
  1: "ibrl_with_allocated_ip",
  2: "edge_filtering",
  3: "multicast",
};
export function userUserTypeString(v: number): string {
  return USER_USER_TYPE_NAMES[v] ?? "unknown";
}

const CYOA_TYPE_NAMES: Record<number, string> = {
  0: "none",
  1: "gre_over_dia",
  2: "gre_over_fabric",
  3: "gre_over_private_peering",
  4: "gre_over_public_peering",
  5: "gre_over_cable",
};
export function cyoaTypeString(v: number): string {
  return CYOA_TYPE_NAMES[v] ?? "unknown";
}

const USER_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  3: "deleting",
  4: "rejected",
  5: "pending_ban",
  6: "banned",
  7: "updating",
  8: "out_of_credits",
};
export function userStatusString(v: number): string {
  return USER_STATUS_NAMES[v] ?? "unknown";
}

const MULTICAST_GROUP_STATUS_NAMES: Record<number, string> = {
  0: "pending",
  1: "activated",
  2: "suspended",
  3: "deleting",
  4: "rejected",
};
export function multicastGroupStatusString(v: number): string {
  return MULTICAST_GROUP_STATUS_NAMES[v] ?? "unknown";
}

const ACCESS_PASS_TYPE_TAG_NAMES: Record<number, string> = {
  0: "prepaid",
  1: "solana_validator",
};
export function accessPassTypeTagString(v: number): string {
  return ACCESS_PASS_TYPE_TAG_NAMES[v] ?? "unknown";
}

const ACCESS_PASS_STATUS_NAMES: Record<number, string> = {
  0: "requested",
  1: "connected",
  2: "disconnected",
  3: "expired",
};
export function accessPassStatusString(v: number): string {
  return ACCESS_PASS_STATUS_NAMES[v] ?? "unknown";
}

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
  const r = new IncrementalReader(data);
  const accountType = r.readU8();
  const bumpSeed = r.readU8();
  const accountIndex = r.readU128();
  const foundationAllowlist = readPubkeyVec(r);
  readPubkeyVec(r); // deprecated device_allowlist
  readPubkeyVec(r); // deprecated user_allowlist
  const activatorAuthorityPk = readPubkey(r);
  const sentinelAuthorityPk = readPubkey(r);
  const contributorAirdropLamports = r.readU64();
  const userAirdropLamports = r.readU64();
  const healthOraclePk = readPubkey(r);
  const qaAllowlist = tryReadPubkeyVec(r);
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
  const r = new IncrementalReader(data);
  return {
    accountType: r.readU8(),
    owner: readPubkey(r),
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
  const r = new IncrementalReader(data);
  return {
    accountType: r.readU8(),
    owner: readPubkey(r),
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
  const r = new IncrementalReader(data);
  const accountType = r.readU8();
  const owner = readPubkey(r);
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
  const device1Pk = readPubkey(r);
  const device2Pk = readPubkey(r);
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

function deserializeInterface(r: IncrementalReader): DeviceInterface {
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
  const r = new IncrementalReader(data);
  const accountType = r.readU8();
  const owner = readPubkey(r);
  const index = r.readU128();
  const bumpSeed = r.readU8();
  const locationPubKey = readPubkey(r);
  const exchangePubKey = readPubkey(r);
  const deviceType = r.readU8();
  const publicIp = r.readIPv4();
  const status = r.readU8();
  const code = r.readString();
  const dzPrefixes = r.readNetworkV4Vec();
  const metricsPublisherPubKey = readPubkey(r);
  const contributorPubKey = readPubkey(r);
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
  const r = new IncrementalReader(data);
  return {
    accountType: r.readU8(),
    owner: readPubkey(r),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    sideAPubKey: readPubkey(r),
    sideZPubKey: readPubkey(r),
    linkType: r.readU8(),
    bandwidth: r.readU64(),
    mtu: r.readU32(),
    delayNs: r.readU64(),
    jitterNs: r.readU64(),
    tunnelId: r.readU16(),
    tunnelNet: r.readNetworkV4(),
    status: r.readU8(),
    code: r.readString(),
    contributorPubKey: readPubkey(r),
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
  const r = new IncrementalReader(data);
  return {
    accountType: r.readU8(),
    owner: readPubkey(r),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    userType: r.readU8(),
    tenantPubKey: readPubkey(r),
    devicePubKey: readPubkey(r),
    cyoaType: r.readU8(),
    clientIp: r.readIPv4(),
    dzIp: r.readIPv4(),
    tunnelId: r.readU16(),
    tunnelNet: r.readNetworkV4(),
    status: r.readU8(),
    publishers: readPubkeyVec(r),
    subscribers: readPubkeyVec(r),
    validatorPubKey: readPubkey(r),
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
  const r = new IncrementalReader(data);
  return {
    accountType: r.readU8(),
    owner: readPubkey(r),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    tenantPubKey: readPubkey(r),
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

function deserializeProgramVersion(r: IncrementalReader): ProgramVersion {
  return {
    major: r.readU32(),
    minor: r.readU32(),
    patch: r.readU32(),
  };
}

export function deserializeProgramConfig(data: Uint8Array): ProgramConfig {
  const r = new IncrementalReader(data);
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
  const r = new IncrementalReader(data);
  return {
    accountType: r.readU8(),
    owner: readPubkey(r),
    index: r.readU128(),
    bumpSeed: r.readU8(),
    status: r.readU8(),
    code: r.readString(),
    referenceCount: r.readU32(),
    opsManagerPk: readPubkey(r),
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
  const r = new IncrementalReader(data);
  const accountType = r.readU8();
  const owner = readPubkey(r);
  const bumpSeed = r.readU8();
  const accessPassType = r.readU8();
  let validatorPubKey: PublicKey | null = null;
  if (accessPassType === 1) {
    // SolanaValidator
    validatorPubKey = readPubkey(r);
  }
  const clientIp = r.readIPv4();
  const userPayer = readPubkey(r);
  const lastAccessEpoch = r.readU64();
  const connectionCount = r.readU16();
  const status = r.readU8();
  const mGroupPubAllowlist = readPubkeyVec(r);
  const mGroupSubAllowlist = readPubkeyVec(r);
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
