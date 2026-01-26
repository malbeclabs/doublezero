// Shared utility functions for topology components

// Format bandwidth for display
export function formatBandwidth(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} Kbps`
  return `${bps.toFixed(0)} bps`
}

// Format traffic rate for display
export function formatTrafficRate(bps: number | undefined | null): string {
  if (bps == null || bps <= 0) return 'N/A'
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(2)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(2)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(2)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(2)} Kbps`
  return `${bps.toFixed(0)} bps`
}

// Format rate for chart axis (compact)
export function formatChartAxisRate(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)}T`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)}G`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)}M`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)}K`
  return `${bps.toFixed(0)}`
}

// Format rate for chart tooltip (full)
export function formatChartTooltipRate(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(2)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(2)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(2)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(2)} Kbps`
  return `${bps.toFixed(0)} bps`
}

// Format bits per second to human readable (compact, no space)
export function formatBps(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)}Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)}Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)}Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(0)}Kbps`
  return `${bps.toFixed(0)}bps`
}

// Format stake in SOL (compact display)
export function formatStake(stakeSol: number): string {
  if (stakeSol >= 1_000_000) return `${(stakeSol / 1_000_000).toFixed(1)}M SOL`
  if (stakeSol >= 1_000) return `${(stakeSol / 1_000).toFixed(0)}K SOL`
  return `${stakeSol.toFixed(0)} SOL`
}

// Fetch traffic history for a link, device, or validator
export async function fetchTrafficHistory(
  type: 'link' | 'device' | 'validator',
  pk: string
): Promise<{ time: string; avgIn: number; avgOut: number; peakIn: number; peakOut: number }[]> {
  const res = await fetch(`/api/topology/traffic?type=${type}&pk=${encodeURIComponent(pk)}`)
  if (!res.ok) return []
  const data = await res.json()
  return data.points || []
}

// Latency data point for charts
export interface LatencyDataPoint {
  time: string
  avgRttMs: number
  p95RttMs: number
  avgJitter: number
  lossPct: number
  avgRttAtoZMs?: number
  p95RttAtoZMs?: number
  avgRttZtoAMs?: number
  p95RttZtoAMs?: number
  jitterAtoZMs?: number
  jitterZtoAMs?: number
}

// Time range options for latency charts
export type TimeRangePreset = '15m' | '30m' | '1h' | '3h' | '6h' | '12h' | '24h' | '2d' | '7d' | 'custom'

export interface TimeRange {
  preset: TimeRangePreset
  from?: string // yyyy-mm-dd-hh:mm:ss
  to?: string   // yyyy-mm-dd-hh:mm:ss
}

// Fetch latency history for a link with optional time range
export async function fetchLatencyHistory(
  pk: string,
  timeRange?: TimeRange
): Promise<LatencyDataPoint[]> {
  const params = new URLSearchParams({ pk })

  if (timeRange) {
    if (timeRange.preset === 'custom' && timeRange.from && timeRange.to) {
      params.set('from', timeRange.from)
      params.set('to', timeRange.to)
    } else if (timeRange.preset !== 'custom') {
      params.set('range', timeRange.preset)
    }
  }

  const res = await fetch(`/api/topology/link-latency?${params.toString()}`)
  if (!res.ok) return []
  const data = await res.json()
  return data.points || []
}
