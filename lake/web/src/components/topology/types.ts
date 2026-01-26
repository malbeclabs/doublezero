// Shared types for topology components

// Link info (used in drawer/panel)
export interface LinkInfo {
  pk: string
  code: string
  linkType: string
  bandwidth: string
  latencyMs: string
  jitterMs: string
  latencyAtoZMs: string
  jitterAtoZMs: string
  latencyZtoAMs: string
  jitterZtoAMs: string
  lossPercent: string
  inRate: string
  outRate: string
  deviceAPk: string
  deviceACode: string
  interfaceAName: string
  interfaceAIP: string
  deviceZPk: string
  deviceZCode: string
  interfaceZName: string
  interfaceZIP: string
  contributorPk: string
  contributorCode: string
  health?: {
    status: string
    committedRttNs: number
    slaRatio: number
    lossPct: number
  }
  // Inter-metro link properties
  isInterMetro?: boolean
  linkCount?: number
  avgLatencyMs?: string
}

// Interface info
export interface InterfaceInfo {
  name: string
  ip: string
  status: string
}

// Device info (used in drawer/panel)
export interface DeviceInfo {
  pk: string
  code: string
  deviceType: string
  status: string
  metroPk: string
  metroName: string
  contributorPk: string
  contributorCode: string
  userCount: number
  validatorCount: number
  stakeSol: string
  stakeShare: string
  interfaces: InterfaceInfo[]
}

// Metro info (used in drawer/panel)
export interface MetroInfo {
  pk: string
  code: string
  name: string
  deviceCount: number
}

// Validator info (used in drawer/panel)
export interface ValidatorInfo {
  votePubkey: string
  nodePubkey: string
  tunnelId: number
  city: string
  country: string
  stakeSol: string
  stakeShare: string
  commission: number
  version: string
  gossipIp: string
  gossipPort: number
  tpuQuicIp: string
  tpuQuicPort: number
  deviceCode: string
  devicePk: string
  metroPk: string
  metroName: string
  inRate: string
  outRate: string
}

// Union type for selected items
export type SelectedItemData =
  | { type: 'link'; data: LinkInfo }
  | { type: 'device'; data: DeviceInfo }
  | { type: 'metro'; data: MetroInfo }
  | { type: 'validator'; data: ValidatorInfo }

// Traffic data point for charts
export interface TrafficDataPoint {
  time: string
  avgIn: number
  avgOut: number
  peakIn: number
  peakOut: number
}
