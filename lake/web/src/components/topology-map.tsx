import { useMemo, useEffect, useRef, useState, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import MapGL, { Source, Layer, Marker } from 'react-map-gl/maplibre'
import type { MapRef, MapLayerMouseEvent, LngLatBoundsLike } from 'react-map-gl/maplibre'
import type { StyleSpecification } from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { useQuery } from '@tanstack/react-query'
import { useTheme } from '@/hooks/use-theme'
import type { TopologyMetro, TopologyDevice, TopologyLink, TopologyValidator, MultiPathResponse, SimulateLinkRemovalResponse, SimulateLinkAdditionResponse, MaintenanceImpactResponse } from '@/lib/api'
import { fetchISISPaths, fetchISISTopology, fetchCriticalLinks, fetchSimulateLinkRemoval, fetchSimulateLinkAddition, fetchMaintenanceImpact, fetchLinkHealth, fetchTopologyCompare } from '@/lib/api'
import { useTopology, TopologyControlBar, TopologyPanel, DeviceDetails, LinkDetails, MetroDetails, ValidatorDetails, EntityLink as TopologyEntityLink, PathModePanel, CriticalityPanel, WhatIfRemovalPanel, WhatIfAdditionPanel, ImpactPanel, ComparePanel, StakeOverlayPanel, LinkHealthOverlayPanel, TrafficFlowOverlayPanel, MetroClusteringOverlayPanel, ContributorsOverlayPanel, ValidatorsOverlayPanel, BandwidthOverlayPanel, DeviceTypeOverlayPanel, LinkTypeOverlayPanel, LINK_TYPE_COLORS } from '@/components/topology'

// Path colors for multi-path visualization
const PATH_COLORS = [
  '#22c55e',  // green - primary/shortest
  '#3b82f6',  // blue - alternate 1
  '#a855f7',  // purple - alternate 2
  '#f97316',  // orange - alternate 3
  '#06b6d4',  // cyan - alternate 4
]

// Metro colors for metro clustering visualization (10 distinct colors)
const METRO_COLORS = [
  '#3b82f6',  // blue
  '#a78bfa',  // purple
  '#f472b6',  // pink
  '#f97316',  // orange
  '#22c55e',  // green
  '#22d3ee',  // cyan
  '#818cf8',  // indigo
  '#facc15',  // yellow
  '#2dd4bf',  // teal
  '#f472b6',  // rose
]

// Contributor colors for contributor overlay visualization (12 distinct colors)
const CONTRIBUTOR_COLORS = [
  '#8b5cf6',  // violet
  '#ec4899',  // pink
  '#06b6d4',  // cyan
  '#84cc16',  // lime
  '#f59e0b',  // amber
  '#6366f1',  // indigo
  '#14b8a6',  // teal
  '#f97316',  // orange
  '#a855f7',  // purple
  '#10b981',  // emerald
  '#ef4444',  // red
  '#0ea5e9',  // sky
]

// Device type colors (hybrid, transit, edge)
// Avoid green/red (status colors) and blue/purple (link colors)
const DEVICE_TYPE_COLORS: Record<string, string> = {
  hybrid: '#eab308',    // yellow
  transit: '#f97316',   // orange
  edge: '#22d3ee',      // cyan
  default: '#9ca3af',   // gray
}

interface TopologyMapProps {
  metros: TopologyMetro[]
  devices: TopologyDevice[]
  links: TopologyLink[]
  validators: TopologyValidator[]
}

// Format bandwidth for display
function formatBandwidth(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} Kbps`
  return `${bps.toFixed(0)} bps`
}

// Format traffic rate for display
function formatTrafficRate(bps: number | undefined | null): string {
  if (bps == null || bps <= 0) return 'N/A'
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(2)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(2)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(2)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(2)} Kbps`
  return `${bps.toFixed(0)} bps`
}

// Get link weight based on bandwidth capacity (most links are 10-100 Gbps)
function getBandwidthWeight(bps: number): number {
  const gbps = bps / 1e9
  if (gbps >= 100) return 6
  if (gbps >= 40) return 4
  if (gbps >= 10) return 2
  if (gbps >= 1) return 1.5
  return 1
}

// Calculate validator marker radius based on stake (logarithmic scale)
// Range: 3px (small) to 10px (large)
function calculateValidatorRadius(stakeSol: number): number {
  if (stakeSol <= 0) return 3
  // Log scale: 1k SOL = 3, 10k = 4.5, 100k = 6, 1M = 7.5, 10M = 9
  const minRadius = 3
  const maxRadius = 10
  const minStake = 1000 // 1k SOL
  const radius = minRadius + Math.log10(Math.max(minStake, stakeSol) / minStake) * 1.5
  return Math.min(maxRadius, radius)
}

// Calculate device marker size based on stake (for stake overlay mode)
// Range: 8px (no stake) to 28px (high stake)
function calculateDeviceStakeSize(stakeSol: number): number {
  if (stakeSol <= 0) return 8
  // Log scale: 10k = 10, 100k = 14, 1M = 18, 10M = 22, 100M = 26
  const minSize = 8
  const maxSize = 28
  const minStake = 10000 // 10k SOL
  const size = minSize + Math.log10(Math.max(minStake, stakeSol) / minStake) * 4.5
  return Math.min(maxSize, size)
}

// Get stake-based color intensity (yellow to orange gradient based on stake share)
function getStakeColor(stakeShare: number): string {
  if (stakeShare <= 0) return '#6b7280' // gray for no stake
  // Scale: 0% = yellow, 1%+ = deep orange
  const t = Math.min(stakeShare / 1.0, 1) // cap at 1%
  const r = Math.round(234 + t * (234 - 234))
  const g = Math.round(179 - t * (179 - 88))
  const b = Math.round(8 + t * (8 - 8))
  return `rgb(${r}, ${g}, ${b})`
}

// Hovered link info type
interface HoveredLinkInfo {
  pk: string
  code: string
  linkType: string
  bandwidth: string
  latencyMs: string
  jitterMs: string
  lossPercent: string
  inRate: string
  outRate: string
  deviceAPk: string
  deviceACode: string
  deviceZPk: string
  deviceZCode: string
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

// Hovered device info type
interface HoveredDeviceInfo {
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
}

// Hovered metro info type
interface HoveredMetroInfo {
  pk: string
  code: string
  name: string
  deviceCount: number
}

// Hovered validator info type
interface HoveredValidatorInfo {
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

// Selected item type for drawer
type SelectedItem =
  | { type: 'link'; data: HoveredLinkInfo }
  | { type: 'device'; data: HoveredDeviceInfo }
  | { type: 'metro'; data: HoveredMetroInfo }
  | { type: 'validator'; data: HoveredValidatorInfo }

// Calculate device position with radial offset for multiple devices at same metro
function calculateDevicePosition(
  metroLat: number,
  metroLng: number,
  deviceIndex: number,
  totalDevices: number
): [number, number] {
  if (totalDevices === 1) {
    return [metroLng, metroLat]
  }

  // Distribute devices in a circle around metro center
  const radius = 0.3 // degrees offset
  const angle = (2 * Math.PI * deviceIndex) / totalDevices
  const latOffset = radius * Math.cos(angle)
  // Adjust for latitude distortion
  const lngOffset = radius * Math.sin(angle) / Math.cos(metroLat * Math.PI / 180)

  return [metroLng + lngOffset, metroLat + latOffset]
}

// Calculate curved path between two points (returns GeoJSON coordinates [lng, lat])
function calculateCurvedPath(
  start: [number, number],
  end: [number, number],
  curveOffset: number = 0.15
): [number, number][] {
  let startLng = start[0]
  let endLng = end[0]
  const lngDelta = endLng - startLng
  if (Math.abs(lngDelta) > 180) {
    // Cross the antimeridian in the shorter direction (fixes eastward long routes).
    endLng = lngDelta > 0 ? endLng - 360 : endLng + 360
  }

  const midLng = (startLng + endLng) / 2
  const midLat = (start[1] + end[1]) / 2

  // Calculate perpendicular offset for curve
  const dx = endLng - startLng
  const dy = end[1] - start[1]
  const length = Math.sqrt(dx * dx + dy * dy)

  if (length === 0) return [start, end]

  const controlLng = midLng - (dy / length) * curveOffset * length
  const controlLat = midLat + (dx / length) * curveOffset * length

  // Generate points along quadratic bezier curve
  const points: [number, number][] = []
  const segments = 20
  for (let i = 0; i <= segments; i++) {
    const t = i / segments
    const lng = (1 - t) * (1 - t) * startLng + 2 * (1 - t) * t * controlLng + t * t * endLng
    const lat = (1 - t) * (1 - t) * start[1] + 2 * (1 - t) * t * controlLat + t * t * end[1]
    points.push([lng, lat])
  }
  return points
}

// Create MapLibre style with CARTO basemap
function createMapStyle(isDark: boolean): StyleSpecification {
  const tileUrl = isDark
    ? 'https://a.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}.png'
    : 'https://a.basemaps.cartocdn.com/light_all/{z}/{x}/{y}.png'

  return {
    version: 8,
    sources: {
      carto: {
        type: 'raster',
        tiles: [tileUrl],
        tileSize: 256,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
      },
    },
    layers: [
      {
        id: 'carto-tiles',
        type: 'raster',
        source: 'carto',
        minzoom: 0,
        maxzoom: 22,
      },
    ],
  }
}

export function TopologyMap({ metros, devices, links, validators }: TopologyMapProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'
  const [searchParams, setSearchParams] = useSearchParams()
  const [hoveredLink, setHoveredLink] = useState<HoveredLinkInfo | null>(null)
  const [hoveredDevice, setHoveredDevice] = useState<HoveredDeviceInfo | null>(null)
  const [hoveredMetro, setHoveredMetro] = useState<HoveredMetroInfo | null>(null)
  const [hoveredValidator, setHoveredValidator] = useState<HoveredValidatorInfo | null>(null)
  const [mousePos, setMousePos] = useState<{ x: number; y: number }>({ x: 0, y: 0 })
  const [selectedItem, setSelectedItemState] = useState<SelectedItem | null>(null)
  const mapRef = useRef<MapRef>(null)
  const [mapReady, setMapReady] = useState(false)
  const markerClickedRef = useRef(false)

  // Get unified topology context
  const { mode, setMode, pathMode, setPathMode, overlays, toggleOverlay, panel, openPanel, closePanel, selection, impactDevices, toggleImpactDevice, clearImpactDevices } = useTopology()

  // Derive mode states from context
  const pathModeEnabled = mode === 'path'
  const whatifRemovalMode = mode === 'whatif-removal'
  const whatifAdditionMode = mode === 'whatif-addition'
  const impactMode = mode === 'impact'

  // Derive overlay states from context
  const criticalityOverlayEnabled = overlays.criticality
  const showValidators = overlays.validators
  const deviceTypeMode = overlays.deviceType
  const linkTypeMode = overlays.linkType
  const stakeOverlayMode = overlays.stake
  const linkHealthMode = overlays.linkHealth
  const trafficFlowMode = overlays.trafficFlow
  const metroClusteringMode = overlays.metroClustering
  const contributorDevicesMode = overlays.contributorDevices
  const contributorLinksMode = overlays.contributorLinks
  const bandwidthMode = overlays.bandwidth
  const isisHealthMode = overlays.isisHealth

  // Path finding operational state (local)
  const [pathSource, setPathSource] = useState<string | null>(null)
  const [pathTarget, setPathTarget] = useState<string | null>(null)
  const [pathsResult, setPathsResult] = useState<MultiPathResponse | null>(null)
  const [pathLoading, setPathLoading] = useState(false)
  const [selectedPathIndex, setSelectedPathIndex] = useState(0)

  // What-If Link Removal operational state (local)
  const [removalLink, setRemovalLink] = useState<{ sourcePK: string; targetPK: string; linkPK: string } | null>(null)
  const [removalResult, setRemovalResult] = useState<SimulateLinkRemovalResponse | null>(null)
  const [removalLoading, setRemovalLoading] = useState(false)

  // What-If Link Addition operational state (local)
  const [additionSource, setAdditionSource] = useState<string | null>(null)
  const [additionTarget, setAdditionTarget] = useState<string | null>(null)
  const [additionMetric, setAdditionMetric] = useState<number>(1000)
  const [additionResult, setAdditionResult] = useState<SimulateLinkAdditionResponse | null>(null)
  const [additionLoading, setAdditionLoading] = useState(false)

  // Failure Impact operational state (local) - impactDevices comes from context
  const [impactResult, setImpactResult] = useState<MaintenanceImpactResponse | null>(null)
  const [impactLoading, setImpactLoading] = useState(false)

  // Metro Clustering operational state (local)
  const [collapsedMetros, setCollapsedMetros] = useState<Set<string>>(new Set())

  // Fetch ISIS topology to determine which devices have ISIS data
  const { data: isisTopology } = useQuery({
    queryKey: ['isis-topology'],
    queryFn: fetchISISTopology,
    enabled: pathModeEnabled,
  })

  // Build set of ISIS-enabled device PKs
  const isisDevicePKs = useMemo(() => {
    if (!isisTopology?.nodes) return new Set<string>()
    return new Set(isisTopology.nodes.map(node => node.data.id))
  }, [isisTopology])

  // Fetch critical links when in criticality mode
  const { data: criticalLinksData } = useQuery({
    queryKey: ['critical-links'],
    queryFn: fetchCriticalLinks,
    enabled: criticalityOverlayEnabled,
  })

  // Fetch link health data when link health mode is enabled
  const { data: linkHealthData } = useQuery({
    queryKey: ['link-health'],
    queryFn: fetchLinkHealth,
    enabled: linkHealthMode,
    staleTime: 30000,
  })

  // Fetch topology comparison when ISIS health overlay is enabled
  const { data: compareData, isLoading: compareLoading } = useQuery({
    queryKey: ['topology-compare'],
    queryFn: fetchTopologyCompare,
    enabled: isisHealthMode,
    refetchInterval: 60000,
  })

  // Build link SLA status map (keyed by link PK)
  const linkSlaStatus = useMemo(() => {
    if (!linkHealthData?.links) return new Map<string, { status: string; avgRttUs: number; committedRttNs: number; lossPct: number; slaRatio: number }>()
    const slaMap = new Map<string, { status: string; avgRttUs: number; committedRttNs: number; lossPct: number; slaRatio: number }>()

    for (const link of linkHealthData.links) {
      slaMap.set(link.link_pk, {
        status: link.sla_status,
        avgRttUs: link.avg_rtt_us,
        committedRttNs: link.committed_rtt_ns,
        lossPct: link.loss_pct,
        slaRatio: link.sla_ratio,
      })
    }
    return slaMap
  }, [linkHealthData])

  // Build edge health status from compare data (keyed by device pair)
  // Returns: 'matched' | 'missing' | 'extra' | 'mismatch' | undefined
  const edgeHealthStatus = useMemo(() => {
    if (!compareData?.discrepancies) return new Map<string, string>()
    const status = new Map<string, string>()

    for (const d of compareData.discrepancies) {
      // Create keys for both directions (using | separator to match link lookup)
      const key1 = `${d.deviceAPK}|${d.deviceBPK}`
      const key2 = `${d.deviceBPK}|${d.deviceAPK}`

      if (d.type === 'missing_isis') {
        status.set(key1, 'missing')
        status.set(key2, 'missing')
      } else if (d.type === 'extra_isis') {
        status.set(key1, 'extra')
        status.set(key2, 'extra')
      } else if (d.type === 'metric_mismatch') {
        status.set(key1, 'mismatch')
        status.set(key2, 'mismatch')
      }
    }
    return status
  }, [compareData])

  // Build link criticality map (keyed by link PK)
  const linkCriticalityMap = useMemo(() => {
    const map = new Map<string, 'critical' | 'important' | 'redundant'>()
    if (!criticalLinksData?.links) return map

    // Build a map from device pair to link PK
    const devicePairToLinkPK = new Map<string, string>()
    for (const link of links) {
      const key1 = `${link.side_a_pk}|${link.side_z_pk}`
      const key2 = `${link.side_z_pk}|${link.side_a_pk}`
      devicePairToLinkPK.set(key1, link.pk)
      devicePairToLinkPK.set(key2, link.pk)
    }

    // Map critical links to link PKs
    for (const critLink of criticalLinksData.links) {
      const key = `${critLink.sourcePK}|${critLink.targetPK}`
      const linkPK = devicePairToLinkPK.get(key)
      if (linkPK) {
        map.set(linkPK, critLink.criticality)
      }
    }
    return map
  }, [criticalLinksData, links])

  // Update URL and panel when selected item changes
  const setSelectedItem = useCallback((item: SelectedItem | null) => {
    settingSelectionLocallyRef.current = true
    setSelectedItemState(item)
    if (item === null) {
      setSearchParams({})
      // Close panel when deselecting (only if showing details, not mode)
      if (panel.content === 'details') {
        closePanel()
      }
    } else {
      const params: Record<string, string> = { type: item.type }
      if (item.type === 'validator') {
        params.id = item.data.votePubkey
      } else if (item.type === 'device') {
        params.id = item.data.pk
      } else if (item.type === 'link') {
        params.id = item.data.pk
      } else if (item.type === 'metro') {
        params.id = item.data.pk
      }
      setSearchParams(params)
      // Open panel to show details (only in explore mode)
      if (mode === 'explore') {
        openPanel('details')
      }
    }
  }, [setSearchParams, panel.content, closePanel, mode, openPanel])

  // Track when we're setting selection locally to avoid sync conflicts
  const settingSelectionLocallyRef = useRef(false)

  // Sync context selection to local state (handles external deselection like closing panel)
  useEffect(() => {
    // Skip if we just set the selection locally (avoid race with URL param update)
    if (settingSelectionLocallyRef.current) {
      settingSelectionLocallyRef.current = false
      return
    }
    if (selection === null && selectedItem !== null) {
      setSelectedItemState(null)
    }
  }, [selection, selectedItem])

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't trigger shortcuts when typing in input fields
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return

      const isInMode = mode !== 'explore'

      // Ignore keyboard shortcuts when modifier keys are pressed (e.g., Cmd+R to refresh)
      if (e.metaKey || e.ctrlKey || e.altKey) return

      if (e.key === 'Escape') {
        if (mode !== 'explore') {
          setMode('explore')
        } else if (selectedItem) {
          setSelectedItem(null)
        }
      } else if (e.key === 'p' && !isInMode) {
        setMode('path')
      } else if (e.key === 'c' && !isInMode) {
        toggleOverlay('criticality')
      } else if (e.key === 'r' && !isInMode) {
        setMode('whatif-removal')
      } else if (e.key === 'a' && !isInMode) {
        setMode('whatif-addition')
      } else if (e.key === 's' && !isInMode) {
        toggleOverlay('stake')
      } else if (e.key === 'h' && !isInMode) {
        toggleOverlay('linkHealth')
      } else if (e.key === 't' && !isInMode) {
        toggleOverlay('trafficFlow')
      } else if (e.key === 'm' && !isInMode) {
        toggleOverlay('metroClustering')
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [mode, setMode, toggleOverlay, selectedItem, setSelectedItem])

  // Helper to handle marker clicks - sets flag to prevent map click from clearing selection
  const handleMarkerClick = useCallback((item: SelectedItem) => {
    markerClickedRef.current = true
    setSelectedItem(item)
    setTimeout(() => { markerClickedRef.current = false }, 0)
  }, [setSelectedItem])

  // Hover highlight color: light in dark mode, dark in light mode
  const hoverHighlight = isDark ? '#fff' : '#000'

  // Get metro color based on index
  const getMetroColor = useCallback((_metroPK: string, metroIndex: number) => {
    return METRO_COLORS[metroIndex % METRO_COLORS.length]
  }, [])

  // Toggle metro collapse state
  const toggleMetroCollapse = useCallback((metroPK: string) => {
    setCollapsedMetros(prev => {
      const newSet = new Set(prev)
      if (newSet.has(metroPK)) {
        newSet.delete(metroPK)
      } else {
        newSet.add(metroPK)
      }
      return newSet
    })
  }, [])

  // Get traffic color based on utilization
  const getTrafficColor = useCallback((link: TopologyLink): { color: string; weight: number; opacity: number } => {
    const totalBps = (link.in_bps ?? 0) + (link.out_bps ?? 0)
    const utilization = link.bandwidth_bps > 0 ? (totalBps / link.bandwidth_bps) * 100 : 0

    if (utilization >= 80) {
      return { color: '#ef4444', weight: 5, opacity: 1 } // red - critical
    } else if (utilization >= 50) {
      return { color: '#eab308', weight: 4, opacity: 1 } // yellow - high
    } else if (utilization >= 20) {
      return { color: '#84cc16', weight: 3, opacity: 0.9 } // lime - medium
    } else if (totalBps > 0) {
      return { color: '#22c55e', weight: 2, opacity: 0.8 } // green - low
    } else {
      return { color: isDark ? '#6b7280' : '#9ca3af', weight: 1, opacity: 0.4 } // gray - idle
    }
  }, [isDark])

  // Clear collapsed metros when metro clustering is disabled
  useEffect(() => {
    if (!metroClusteringMode) {
      setCollapsedMetros(new Set())
    }
  }, [metroClusteringMode])

  // Build metro lookup map
  const metroMap = useMemo(() => {
    const map = new Map<string, TopologyMetro>()
    for (const metro of metros) {
      map.set(metro.pk, metro)
    }
    return map
  }, [metros])

  // Build metro index map for consistent colors
  const metroIndexMap = useMemo(() => {
    const map = new Map<string, number>()
    const sortedMetros = [...metros].sort((a, b) => a.code.localeCompare(b.code))
    sortedMetros.forEach((metro, index) => {
      map.set(metro.pk, index)
    })
    return map
  }, [metros])

  // Build device lookup map
  const deviceMap = useMemo(() => {
    const map = new Map<string, TopologyDevice>()
    for (const device of devices) {
      map.set(device.pk, device)
    }
    return map
  }, [devices])

  // Build device code lookup map (pk -> code) for impact panel
  const deviceCodeMap = useMemo(() => {
    const map = new Map<string, string>()
    for (const device of devices) {
      map.set(device.pk, device.code)
    }
    return map
  }, [devices])

  // Build link lookup map
  const linkMap = useMemo(() => {
    const map = new Map<string, TopologyLink>()
    for (const link of links) {
      map.set(link.pk, link)
    }
    return map
  }, [links])

  // Build validator lookup map (by vote_pubkey)
  const validatorMap = useMemo(() => {
    const map = new Map<string, TopologyValidator>()
    for (const validator of validators) {
      map.set(validator.vote_pubkey, validator)
    }
    return map
  }, [validators])

  // Group devices by metro
  const devicesByMetro = useMemo(() => {
    const map = new Map<string, TopologyDevice[]>()
    for (const device of devices) {
      const list = map.get(device.metro_pk) || []
      list.push(device)
      map.set(device.metro_pk, list)
    }
    return map
  }, [devices])

  // Build contributor index map (for consistent colors)
  const contributorIndexMap = useMemo(() => {
    const map = new Map<string, number>()
    // Get unique contributors from devices, sorted by code
    const contributorSet = new Map<string, string>() // pk -> code
    for (const device of devices) {
      if (device.contributor_pk) {
        contributorSet.set(device.contributor_pk, device.contributor_code || device.contributor_pk)
      }
    }
    const sorted = [...contributorSet.entries()].sort((a, b) => a[1].localeCompare(b[1]))
    sorted.forEach(([pk], index) => {
      map.set(pk, index)
    })
    return map
  }, [devices])

  // Get contributor color based on index
  const getContributorColor = useCallback((_contributorPK: string, contributorIndex: number) => {
    return CONTRIBUTOR_COLORS[contributorIndex % CONTRIBUTOR_COLORS.length]
  }, [])

  // Group devices by contributor
  const devicesByContributor = useMemo(() => {
    const map = new Map<string, TopologyDevice[]>()
    for (const device of devices) {
      if (device.contributor_pk) {
        const list = map.get(device.contributor_pk) || []
        list.push(device)
        map.set(device.contributor_pk, list)
      }
    }
    return map
  }, [devices])

  // Group links by contributor
  const linksByContributor = useMemo(() => {
    const map = new Map<string, TopologyLink[]>()
    for (const link of links) {
      if (link.contributor_pk) {
        const list = map.get(link.contributor_pk) || []
        list.push(link)
        map.set(link.contributor_pk, list)
      }
    }
    return map
  }, [links])

  // Build contributor info map for overlay panel
  const contributorInfoMap = useMemo(() => {
    const map = new Map<string, { code: string; name: string }>()
    for (const device of devices) {
      if (device.contributor_pk && !map.has(device.contributor_pk)) {
        map.set(device.contributor_pk, {
          code: device.contributor_code || device.contributor_pk.substring(0, 8),
          name: device.contributor_code || '', // Use code as name if no name available
        })
      }
    }
    return map
  }, [devices])

  // Build device stake map for overlay panel (maps device PK to stake info)
  const deviceStakeMap = useMemo(() => {
    const map = new Map<string, { stakeSol: number; validatorCount: number }>()
    for (const device of devices) {
      map.set(device.pk, {
        stakeSol: device.stake_sol ?? 0,
        validatorCount: device.validator_count ?? 0,
      })
    }
    return map
  }, [devices])

  // Build metro info map for overlay panel (maps metro PK to code/name)
  const metroInfoMap = useMemo(() => {
    const map = new Map<string, { code: string; name: string }>()
    for (const metro of metros) {
      map.set(metro.pk, { code: metro.code, name: metro.name })
    }
    return map
  }, [metros])

  // Build edge traffic map for overlay panel (maps link PK to traffic info)
  const edgeTrafficMap = useMemo(() => {
    const map = new Map<string, { inBps: number; outBps: number; bandwidthBps: number; utilization: number }>()
    for (const link of links) {
      const inBps = link.in_bps ?? 0
      const outBps = link.out_bps ?? 0
      const bandwidthBps = link.bandwidth_bps ?? 0
      const utilization = bandwidthBps > 0 ? Math.max(inBps, outBps) / bandwidthBps : 0
      map.set(link.pk, { inBps, outBps, bandwidthBps, utilization })
    }
    return map
  }, [links])

  // Calculate device positions
  const devicePositions = useMemo(() => {
    const positions = new Map<string, [number, number]>()

    for (const [metroPk, metroDevices] of devicesByMetro) {
      const metro = metroMap.get(metroPk)
      if (!metro) continue

      metroDevices.forEach((device, index) => {
        const pos = calculateDevicePosition(
          metro.latitude,
          metro.longitude,
          index,
          metroDevices.length
        )
        positions.set(device.pk, pos)
      })
    }

    return positions
  }, [devicesByMetro, metroMap])

  // Map style based on theme
  const mapStyle = useMemo(() => createMapStyle(isDark), [isDark])

  // Fit bounds to metros
  const fitBounds = useCallback(() => {
    if (!mapRef.current || metros.length === 0) return

    const lngs = metros.map(m => m.longitude)
    const lats = metros.map(m => m.latitude)
    const bounds: LngLatBoundsLike = [
      [Math.min(...lngs), Math.min(...lats)],
      [Math.max(...lngs), Math.max(...lats)],
    ]
    mapRef.current.fitBounds(bounds, { padding: 50, maxZoom: 5 })
  }, [metros])

  // Fit bounds on initial load
  const initialFitRef = useRef(true)
  useEffect(() => {
    if (initialFitRef.current && metros.length > 0 && mapRef.current) {
      // Wait for map to be ready
      const timer = setTimeout(() => {
        fitBounds()
        initialFitRef.current = false
      }, 100)
      return () => clearTimeout(timer)
    }
  }, [metros, fitBounds])

  // Fetch paths when source and target are set
  useEffect(() => {
    if (!pathModeEnabled || !pathSource || !pathTarget) return

    setPathLoading(true)
    setSelectedPathIndex(0)
    fetchISISPaths(pathSource, pathTarget, 5, pathMode)
      .then(result => {
        setPathsResult(result)
        // Turn off device/link type overlays when path is found to make path visualization clearer
        if (result.paths?.length > 0) {
          if (overlays.deviceType) toggleOverlay('deviceType')
          if (overlays.linkType) toggleOverlay('linkType')
        }
      })
      .catch(err => {
        setPathsResult({ paths: [], from: pathSource, to: pathTarget, error: err.message })
      })
      .finally(() => {
        setPathLoading(false)
      })
  // eslint-disable-next-line react-hooks/exhaustive-deps -- overlays/toggleOverlay are intentionally excluded to avoid re-fetching when overlays change
  }, [pathModeEnabled, pathSource, pathTarget, pathMode])

  // Clear path when exiting path mode
  useEffect(() => {
    if (!pathModeEnabled) {
      setPathSource(null)
      setPathTarget(null)
      setPathsResult(null)
      setSelectedPathIndex(0)
    }
  }, [pathModeEnabled])

  // Clear whatif-removal state when exiting mode
  useEffect(() => {
    if (!whatifRemovalMode) {
      setRemovalLink(null)
      setRemovalResult(null)
    }
  }, [whatifRemovalMode])

  // Clear whatif-addition state when exiting mode
  useEffect(() => {
    if (!whatifAdditionMode) {
      setAdditionSource(null)
      setAdditionTarget(null)
      setAdditionResult(null)
    }
  }, [whatifAdditionMode])

  // Clear impact result when exiting mode (impactDevices is cleared by context)
  useEffect(() => {
    if (!impactMode) {
      setImpactResult(null)
    }
  }, [impactMode])

  // Track whether mode params have been restored from URL (used to prevent sync from clearing params on load)
  // Using state instead of ref ensures sync effect re-runs with correct values after restoration
  const [modeParamsRestored, setModeParamsRestored] = useState(false)

  // Sync mode selections to URL for sharing
  useEffect(() => {
    // Don't sync until restoration is complete, otherwise we clear params before they're read
    if (!modeParamsRestored) return

    const params = new URLSearchParams(searchParams)
    let changed = false

    // Helper to set or delete a param
    const setParam = (key: string, value: string | null) => {
      if (value) {
        if (params.get(key) !== value) {
          params.set(key, value)
          changed = true
        }
      } else if (params.has(key)) {
        params.delete(key)
        changed = true
      }
    }

    // In analysis modes, clear the generic selection params to avoid duplication
    const inAnalysisMode = pathModeEnabled || whatifRemovalMode || whatifAdditionMode || impactMode
    if (inAnalysisMode) {
      setParam('type', null)
      setParam('id', null)
    }

    // Path mode params
    setParam('path_source', pathModeEnabled ? pathSource : null)
    setParam('path_target', pathModeEnabled ? pathTarget : null)

    // What-if removal params
    setParam('removal_link', whatifRemovalMode ? removalLink?.linkPK ?? null : null)

    // What-if addition params
    setParam('addition_source', whatifAdditionMode ? additionSource : null)
    setParam('addition_target', whatifAdditionMode ? additionTarget : null)

    // Impact mode params (comma-separated list of device PKs)
    setParam('impact_devices', impactMode && impactDevices.length > 0 ? impactDevices.join(',') : null)

    if (changed) {
      setSearchParams(params, { replace: true })
    }
  }, [modeParamsRestored, searchParams, setSearchParams, pathModeEnabled, pathSource, pathTarget, whatifRemovalMode, removalLink, whatifAdditionMode, additionSource, additionTarget, impactMode, impactDevices])

  // When entering analysis modes with a device already selected, use it as source
  const prevMapModeRef = useRef<string>(mode)
  useEffect(() => {
    const selectedDevicePK = selectedItem?.type === 'device' ? selectedItem.data.pk : null

    // whatif-addition: use selected device as source
    if (whatifAdditionMode && prevMapModeRef.current !== 'whatif-addition' && selectedDevicePK) {
      setAdditionSource(selectedDevicePK)
    }

    // path mode: use selected device as source
    if (pathModeEnabled && prevMapModeRef.current !== 'path' && selectedDevicePK) {
      setPathSource(selectedDevicePK)
    }

    // impact mode: use selected device for failure analysis (add to impact devices list)
    if (impactMode && prevMapModeRef.current !== 'impact' && selectedDevicePK && !impactDevices.includes(selectedDevicePK)) {
      toggleImpactDevice(selectedDevicePK)
    }

    prevMapModeRef.current = mode
  }, [mode, whatifAdditionMode, pathModeEnabled, impactMode, selectedItem])

  // Fetch link removal simulation when link is selected
  useEffect(() => {
    if (!removalLink) return

    setRemovalLoading(true)
    fetchSimulateLinkRemoval(removalLink.sourcePK, removalLink.targetPK)
      .then(result => {
        setRemovalResult(result)
      })
      .catch(err => {
        setRemovalResult({
          sourcePK: removalLink.sourcePK,
          sourceCode: '',
          targetPK: removalLink.targetPK,
          targetCode: '',
          disconnectedDevices: [],
          disconnectedCount: 0,
          affectedPaths: [],
          affectedPathCount: 0,
          causesPartition: false,
          error: err.message,
        })
      })
      .finally(() => {
        setRemovalLoading(false)
      })
  }, [removalLink])

  // Fetch link addition simulation when both devices are selected
  useEffect(() => {
    if (!additionSource || !additionTarget) return

    setAdditionLoading(true)
    fetchSimulateLinkAddition(additionSource, additionTarget, additionMetric)
      .then(result => {
        setAdditionResult(result)
      })
      .catch(err => {
        setAdditionResult({
          sourcePK: additionSource,
          sourceCode: '',
          targetPK: additionTarget,
          targetCode: '',
          metric: additionMetric,
          improvedPaths: [],
          improvedPathCount: 0,
          redundancyGains: [],
          redundancyCount: 0,
          error: err.message,
        })
      })
      .finally(() => {
        setAdditionLoading(false)
      })
  }, [additionSource, additionTarget, additionMetric])

  // Analyze failure impact when impactDevices changes
  useEffect(() => {
    if (impactDevices.length === 0) {
      setImpactResult(null)
      return
    }

    setImpactLoading(true)
    fetchMaintenanceImpact(impactDevices, [])
      .then(result => {
        setImpactResult(result)
      })
      .catch(err => {
        setImpactResult({
          items: [],
          totalImpact: 0,
          totalDisconnected: 0,
          recommendedOrder: [],
          error: err.message,
        })
      })
      .finally(() => {
        setImpactLoading(false)
      })
  }, [impactDevices])

  // Build map of device PKs to path indices for all paths
  const devicePathMap = useMemo(() => {
    const map = new Map<string, number[]>()
    if (!pathsResult?.paths?.length) return map

    pathsResult.paths.forEach((path, pathIndex) => {
      path.path.forEach(hop => {
        const existing = map.get(hop.devicePK) || []
        if (!existing.includes(pathIndex)) {
          existing.push(pathIndex)
        }
        map.set(hop.devicePK, existing)
      })
    })
    return map
  }, [pathsResult])

  // Build map of link PKs to path indices for all paths
  const linkPathMap = useMemo(() => {
    const map = new Map<string, number[]>()
    if (!pathsResult?.paths?.length) return map

    pathsResult.paths.forEach((singlePath, pathIndex) => {
      // For each consecutive pair in the path, find the link between them
      for (let i = 0; i < singlePath.path.length - 1; i++) {
        const fromPK = singlePath.path[i].devicePK
        const toPK = singlePath.path[i + 1].devicePK

        // Find link that connects these two devices
        for (const link of links) {
          if ((link.side_a_pk === fromPK && link.side_z_pk === toPK) ||
              (link.side_a_pk === toPK && link.side_z_pk === fromPK)) {
            const existing = map.get(link.pk) || []
            if (!existing.includes(pathIndex)) {
              existing.push(pathIndex)
            }
            map.set(link.pk, existing)
            break
          }
        }
      }
    })
    return map
  }, [pathsResult, links])

  // Handle device click for path finding
  const handlePathDeviceClick = useCallback((devicePK: string) => {
    if (!pathSource) {
      setPathSource(devicePK)
    } else if (!pathTarget && devicePK !== pathSource) {
      setPathTarget(devicePK)
    } else {
      // Reset and start new path
      setPathSource(devicePK)
      setPathTarget(null)
      setPathsResult(null)
      setSelectedPathIndex(0)
    }
  }, [pathSource, pathTarget])

  const clearPath = useCallback(() => {
    setPathSource(null)
    setPathTarget(null)
    setPathsResult(null)
    setSelectedPathIndex(0)
  }, [])

  // Criticality colors
  const criticalityColors = {
    critical: '#ef4444',    // red
    important: '#eab308',   // yellow
    redundant: '#22c55e',   // green
  }

  // Build set of disconnected device PKs from removal result
  const disconnectedDevicePKs = useMemo(() => {
    const set = new Set<string>()
    if (removalResult?.disconnectedDevices) {
      removalResult.disconnectedDevices.forEach(d => set.add(d.pk))
    }
    return set
  }, [removalResult])

  // GeoJSON for link lines
  const linkGeoJson = useMemo(() => {
    // When metro clustering mode with collapsed metros, track inter-metro edges
    const interMetroEdges = new Map<string, { count: number; totalLatency: number; latencyCount: number }>()

    const features = links.map(link => {
      const deviceA = deviceMap.get(link.side_a_pk)
      const deviceZ = deviceMap.get(link.side_z_pk)
      const metroAPK = deviceA?.metro_pk
      const metroZPK = deviceZ?.metro_pk

      // Handle collapsed metros
      if (metroClusteringMode && metroAPK && metroZPK) {
        const aCollapsed = collapsedMetros.has(metroAPK)
        const zCollapsed = collapsedMetros.has(metroZPK)

        // Skip intra-metro links when the metro is collapsed
        if (metroAPK === metroZPK && aCollapsed) {
          return null
        }

        // Track inter-metro edges for aggregation
        if (aCollapsed || zCollapsed) {
          if (metroAPK !== metroZPK) {
            // This is an inter-metro link with at least one collapsed end
            const edgeKey = [metroAPK, metroZPK].sort().join('|')
            const existing = interMetroEdges.get(edgeKey) || { count: 0, totalLatency: 0, latencyCount: 0 }
            existing.count++
            if (link.latency_us > 0) {
              existing.totalLatency += link.latency_us
              existing.latencyCount++
            }
            interMetroEdges.set(edgeKey, existing)
          }
          // Skip individual link rendering if either end is collapsed
          if (aCollapsed && zCollapsed) {
            return null
          }
        }
      }

      const startPos = devicePositions.get(link.side_a_pk)
      const endPos = devicePositions.get(link.side_z_pk)

      if (!startPos || !endPos) return null

      const isHovered = hoveredLink?.pk === link.pk
      const isSelected = selectedItem?.type === 'link' && selectedItem.data.pk === link.pk
      const linkPathIndices = linkPathMap.get(link.pk)
      const isInAnyPath = linkPathIndices && linkPathIndices.length > 0
      const isInSelectedPath = linkPathIndices?.includes(selectedPathIndex)
      const criticality = linkCriticalityMap.get(link.pk)
      const isRemovedLink = removalLink?.linkPK === link.pk

      // Determine display color based on mode
      // Default to solid grey when no overlay is active (like graph view)
      let displayColor = isDark ? '#4b5563' : '#9ca3af'
      // Default weight is uniform; bandwidth overlay applies thickness based on capacity
      const defaultWeight = 1.5
      let displayWeight = bandwidthMode ? getBandwidthWeight(link.bandwidth_bps) : defaultWeight
      let displayOpacity = 0.6
      let useDash = false

      if (whatifRemovalMode && isRemovedLink) {
        // Whatif-removal mode: highlight removed link in red dashed
        displayColor = '#ef4444'
        displayWeight = defaultWeight + 3
        displayOpacity = 0.6
        useDash = true
      } else if (linkHealthMode) {
        // Link health mode: color by SLA status (preserves bandwidth weight if active)
        const slaInfo = linkSlaStatus.get(link.pk)
        if (slaInfo) {
          switch (slaInfo.status) {
            case 'healthy':
              displayColor = '#22c55e' // green
              if (!bandwidthMode) displayWeight = defaultWeight + 1
              displayOpacity = 0.9
              break
            case 'warning':
              displayColor = '#eab308' // yellow
              if (!bandwidthMode) displayWeight = defaultWeight + 1
              displayOpacity = 1
              break
            case 'critical':
              displayColor = '#ef4444' // red
              if (!bandwidthMode) displayWeight = defaultWeight + 2
              displayOpacity = 1
              break
            default:
              displayColor = isDark ? '#6b7280' : '#9ca3af' // gray
              displayOpacity = 0.5
          }
        } else {
          displayColor = isDark ? '#6b7280' : '#9ca3af' // gray for no data
          displayOpacity = 0.5
        }
      } else if (trafficFlowMode) {
        // Traffic flow mode: color by utilization (preserves bandwidth weight if active)
        const trafficStyle = getTrafficColor(link)
        displayColor = trafficStyle.color
        if (!bandwidthMode) displayWeight = trafficStyle.weight
        displayOpacity = trafficStyle.opacity
      } else if (contributorLinksMode && link.contributor_pk) {
        // Contributor links mode: color by contributor (preserves bandwidth weight if active)
        const contributorIndex = contributorIndexMap.get(link.contributor_pk) ?? 0
        displayColor = CONTRIBUTOR_COLORS[contributorIndex % CONTRIBUTOR_COLORS.length]
        if (!bandwidthMode) displayWeight = defaultWeight + 1
        displayOpacity = 0.9
      } else if (contributorLinksMode && !link.contributor_pk) {
        // Contributor links mode but no contributor - dim the link
        displayColor = isDark ? '#6b7280' : '#9ca3af'
        displayOpacity = 0.3
      } else if (linkTypeMode) {
        // Link type mode: color by link type (default overlay)
        const linkType = link.link_type || 'default'
        const colors = LINK_TYPE_COLORS[linkType] || LINK_TYPE_COLORS.default
        displayColor = isDark ? colors.dark : colors.light
        if (!bandwidthMode) displayWeight = linkType === 'WAN' ? defaultWeight + 1 : defaultWeight
        displayOpacity = 0.8
      } else if (criticalityOverlayEnabled && criticality) {
        // Criticality mode: color by criticality level (preserves bandwidth weight if active)
        displayColor = criticalityColors[criticality]
        if (!bandwidthMode) displayWeight = criticality === 'critical' ? defaultWeight + 3 : criticality === 'important' ? defaultWeight + 2 : defaultWeight + 1
        displayOpacity = 1
        useDash = true
      } else if (isisHealthMode) {
        // ISIS overlay: color by health status, thickness by metric
        const healthKey = `${link.side_a_pk}|${link.side_z_pk}`
        const healthStatus = edgeHealthStatus.get(healthKey)

        // Get metric-based width
        const metric = link.latency_us ?? 0
        if (metric <= 0) {
          displayWeight = 1
        } else if (metric <= 1000) {
          displayWeight = 4
        } else if (metric <= 5000) {
          displayWeight = 3
        } else {
          displayWeight = 2
        }

        // Color by health status
        if (healthStatus === 'missing') {
          displayColor = '#ef4444' // red
          useDash = true
          displayOpacity = 1
        } else if (healthStatus === 'extra') {
          displayColor = '#f59e0b' // amber
          displayOpacity = 1
        } else if (healthStatus === 'mismatch') {
          displayColor = '#eab308' // yellow
          displayOpacity = 1
        } else {
          // Default to matched (green)
          displayColor = '#22c55e'
          displayOpacity = 0.8
        }
      } else if (isInSelectedPath && linkPathIndices) {
        // Use the selected path's color
        displayColor = PATH_COLORS[selectedPathIndex % PATH_COLORS.length]
        displayWeight = defaultWeight + 3
        displayOpacity = 1
      } else if (isInAnyPath && linkPathIndices) {
        // In another path but not selected - use first path's color but dimmed
        const firstPathIndex = linkPathIndices[0]
        displayColor = PATH_COLORS[firstPathIndex % PATH_COLORS.length]
        displayWeight = defaultWeight + 1
        displayOpacity = 0.4
      } else if (isSelected) {
        displayColor = '#3b82f6' // blue - selection color
        displayWeight = defaultWeight + 3
        displayOpacity = 1
      } else if (isHovered) {
        displayColor = hoverHighlight
        displayWeight = defaultWeight + 2
        displayOpacity = 1
      }

      return {
        type: 'Feature' as const,
        properties: {
          pk: link.pk,
          code: link.code,
          color: displayColor,
          weight: displayWeight,
          opacity: displayOpacity,
          useDash,
          isSelected: isSelected ? 1 : 0,
        },
        geometry: {
          type: 'LineString' as const,
          coordinates: calculateCurvedPath(startPos, endPos),
        },
      }
    }).filter((f): f is NonNullable<typeof f> => f !== null)

    // Add inter-metro edges for collapsed metros
    if (metroClusteringMode && collapsedMetros.size > 0) {
      for (const [edgeKey, data] of interMetroEdges) {
        const [metroAPK, metroZPK] = edgeKey.split('|')
        const metroA = metroMap.get(metroAPK)
        const metroZ = metroMap.get(metroZPK)
        if (!metroA || !metroZ) continue

        // Only show inter-metro edge if at least one end is collapsed
        if (!collapsedMetros.has(metroAPK) && !collapsedMetros.has(metroZPK)) continue

        const avgLatencyMs: string = data.latencyCount > 0
          ? ((data.totalLatency / data.latencyCount) / 1000).toFixed(2)
          : 'N/A';

        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (features as any[]).push({
          type: 'Feature' as const,
          properties: {
            pk: `inter-metro-${edgeKey}`,
            code: `${metroA.code} ↔ ${metroZ.code}`,
            color: isDark ? '#94a3b8' : '#64748b',
            weight: 4,
            opacity: 0.8,
            useDash: true,
            isInterMetro: true,
            linkCount: data.count,
            avgLatencyMs,
          },
          geometry: {
            type: 'LineString' as const,
            coordinates: calculateCurvedPath(
              [metroA.longitude, metroA.latitude],
              [metroZ.longitude, metroZ.latitude]
            ),
          },
        })
      }
    }

    return {
      type: 'FeatureCollection' as const,
      features,
    }
  }, [links, devicePositions, isDark, hoveredLink, selectedItem, hoverHighlight, linkPathMap, selectedPathIndex, criticalityOverlayEnabled, linkCriticalityMap, whatifRemovalMode, removalLink, linkHealthMode, linkSlaStatus, trafficFlowMode, getTrafficColor, metroClusteringMode, collapsedMetros, deviceMap, metroMap, contributorLinksMode, contributorIndexMap, bandwidthMode, isisHealthMode, edgeHealthStatus, linkTypeMode])

  // GeoJSON for validator links (connecting lines)
  const validatorLinksGeoJson = useMemo(() => {
    if (!showValidators) return { type: 'FeatureCollection' as const, features: [] }

    const validatorLinkColor = isDark ? '#7c3aed' : '#6d28d9'
    const features = validators.map(validator => {
      const devicePos = devicePositions.get(validator.device_pk)
      if (!devicePos) return null

      const isHovered = hoveredValidator?.votePubkey === validator.vote_pubkey

      return {
        type: 'Feature' as const,
        properties: {
          votePubkey: validator.vote_pubkey,
          color: isHovered ? hoverHighlight : validatorLinkColor,
          weight: isHovered ? 2 : 1,
          opacity: isHovered ? 0.9 : 0.4,
        },
        geometry: {
          type: 'LineString' as const,
          coordinates: [
            [validator.longitude, validator.latitude],
            devicePos,
          ],
        },
      }
    }).filter((f): f is NonNullable<typeof f> => f !== null)

    return {
      type: 'FeatureCollection' as const,
      features,
    }
  }, [validators, devicePositions, showValidators, isDark, hoveredValidator, hoverHighlight])

  // Colors
  const deviceColor = isDark ? '#6b7280' : '#1f2937' // neutral grey/dark - overlays will override
  const metroColor = isDark ? '#4b5563' : '#9ca3af' // gray
  const validatorColor = isDark ? '#a855f7' : '#9333ea' // purple
  const selectionColor = '#3b82f6' // blue - consistent with graph view

  // Build hover info for links
  const buildLinkInfo = useCallback((link: TopologyLink): HoveredLinkInfo => {
    const deviceA = deviceMap.get(link.side_a_pk)
    const deviceZ = deviceMap.get(link.side_z_pk)
    const hasLatencyData = (link.sample_count ?? 0) > 0
    const healthInfo = linkSlaStatus.get(link.pk)
    return {
      pk: link.pk,
      code: link.code,
      linkType: link.link_type,
      bandwidth: formatBandwidth(link.bandwidth_bps),
      latencyMs: hasLatencyData ? (link.latency_us > 0 ? `${(link.latency_us / 1000).toFixed(2)}ms` : '0.00ms') : 'N/A',
      jitterMs: hasLatencyData ? ((link.jitter_us ?? 0) > 0 ? `${(link.jitter_us / 1000).toFixed(3)}ms` : '0.000ms') : 'N/A',
      lossPercent: hasLatencyData ? `${(link.loss_percent ?? 0).toFixed(2)}%` : 'N/A',
      inRate: formatTrafficRate(link.in_bps),
      outRate: formatTrafficRate(link.out_bps),
      deviceAPk: link.side_a_pk,
      deviceACode: deviceA?.code || 'Unknown',
      deviceZPk: link.side_z_pk,
      deviceZCode: deviceZ?.code || 'Unknown',
      contributorPk: link.contributor_pk,
      contributorCode: link.contributor_code,
      health: healthInfo ? {
        status: healthInfo.status,
        committedRttNs: healthInfo.committedRttNs,
        slaRatio: healthInfo.slaRatio,
        lossPct: healthInfo.lossPct,
      } : undefined,
    }
  }, [deviceMap, linkSlaStatus])

  // Handle map click to deselect or select links
  const handleMapClick = useCallback((e: MapLayerMouseEvent) => {
    // If a marker was clicked, don't process map click (marker handler takes precedence)
    if (markerClickedRef.current) {
      return
    }
    // Don't handle link clicks in path mode or addition mode
    if (pathModeEnabled || whatifAdditionMode) {
      return
    }
    // Check if a link was clicked
    if (e.features && e.features.length > 0) {
      const feature = e.features[0]
      if (feature.properties?.pk && feature.layer?.id?.includes('link')) {
        const pk = feature.properties.pk
        const link = linkMap.get(pk)
        if (link) {
          // Handle whatif-removal mode link click
          if (whatifRemovalMode) {
            setRemovalLink({
              sourcePK: link.side_a_pk,
              targetPK: link.side_z_pk,
              linkPK: link.pk,
            })
            return
          }
          handleMarkerClick({ type: 'link', data: buildLinkInfo(link) })
          return
        }
      }
    }
    // Close drawer when clicking empty area
    setSelectedItem(null)
  }, [setSelectedItem, linkMap, buildLinkInfo, handleMarkerClick, pathModeEnabled, whatifRemovalMode, whatifAdditionMode])

  // Map control handlers
  const handleZoomIn = useCallback(() => {
    mapRef.current?.zoomIn()
  }, [])

  const handleZoomOut = useCallback(() => {
    mapRef.current?.zoomOut()
  }, [])

  // Restore mode selections from URL params on initial load
  useEffect(() => {
    // Only run once when data and map are available
    if (modeParamsRestored) return
    if (deviceMap.size === 0 || linkMap.size === 0) return
    if (!mapReady) return

    const pathSourceParam = searchParams.get('path_source')
    const pathTargetParam = searchParams.get('path_target')
    const removalLinkParam = searchParams.get('removal_link')
    const additionSourceParam = searchParams.get('addition_source')
    const additionTargetParam = searchParams.get('addition_target')
    const impactDevicesParam = searchParams.get('impact_devices')

    // Restore path mode
    if (pathSourceParam || pathTargetParam) {
      // Check if all referenced devices are available
      const sourceFound = !pathSourceParam || deviceMap.has(pathSourceParam)
      const targetFound = !pathTargetParam || deviceMap.has(pathTargetParam)
      if (!sourceFound || !targetFound) {
        // Devices not found yet, wait for more data
        return
      }
      if (pathSourceParam) {
        setPathSource(pathSourceParam)
      }
      if (pathTargetParam) {
        setPathTarget(pathTargetParam)
      }
      if (mode !== 'path') {
        setMode('path')
        openPanel('mode')
      }
      // Fly to show selected device(s)
      const sourceDevice = pathSourceParam ? deviceMap.get(pathSourceParam) : undefined
      const targetDevice = pathTargetParam ? deviceMap.get(pathTargetParam) : undefined
      const sourceMetro = sourceDevice ? metroMap.get(sourceDevice.metro_pk) : undefined
      const targetMetro = targetDevice ? metroMap.get(targetDevice.metro_pk) : undefined
      if (sourceMetro && targetMetro && mapRef.current) {
        // Two devices - fly to midpoint
        let aLng = sourceMetro.longitude
        let zLng = targetMetro.longitude
        const lngDelta = zLng - aLng
        if (Math.abs(lngDelta) > 180) {
          zLng = lngDelta > 0 ? zLng - 360 : zLng + 360
        }
        let midLng = (aLng + zLng) / 2
        if (midLng > 180) midLng -= 360
        if (midLng < -180) midLng += 360
        const midLat = (sourceMetro.latitude + targetMetro.latitude) / 2
        mapRef.current.flyTo({ center: [midLng, midLat], zoom: 3, duration: 1000 })
      } else if (sourceMetro && mapRef.current) {
        mapRef.current.flyTo({ center: [sourceMetro.longitude, sourceMetro.latitude], zoom: 4, duration: 1000 })
      } else if (targetMetro && mapRef.current) {
        mapRef.current.flyTo({ center: [targetMetro.longitude, targetMetro.latitude], zoom: 4, duration: 1000 })
      }
      setModeParamsRestored(true)
      return
    }

    // Restore what-if removal mode
    if (removalLinkParam) {
      const link = linkMap.get(removalLinkParam)
      if (link) {
        setRemovalLink({ sourcePK: link.side_a_pk, targetPK: link.side_z_pk, linkPK: link.pk })
        if (mode !== 'whatif-removal') {
          setMode('whatif-removal')
          openPanel('mode')
        }
        // Fly to the link's midpoint
        const deviceA = deviceMap.get(link.side_a_pk)
        const deviceZ = deviceMap.get(link.side_z_pk)
        const metroA = deviceA ? metroMap.get(deviceA.metro_pk) : undefined
        const metroZ = deviceZ ? metroMap.get(deviceZ.metro_pk) : undefined
        if (metroA && metroZ && mapRef.current) {
          let aLng = metroA.longitude
          let zLng = metroZ.longitude
          const lngDelta = zLng - aLng
          if (Math.abs(lngDelta) > 180) {
            zLng = lngDelta > 0 ? zLng - 360 : zLng + 360
          }
          let midLng = (aLng + zLng) / 2
          if (midLng > 180) midLng -= 360
          if (midLng < -180) midLng += 360
          const midLat = (metroA.latitude + metroZ.latitude) / 2
          mapRef.current.flyTo({ center: [midLng, midLat], zoom: 3, duration: 1000 })
        }
        setModeParamsRestored(true)
        return
      }
      // Link not found yet, wait for more data
      return
    }

    // Restore what-if addition mode
    if (additionSourceParam || additionTargetParam) {
      // Check if all referenced devices are available
      const sourceFound = !additionSourceParam || deviceMap.has(additionSourceParam)
      const targetFound = !additionTargetParam || deviceMap.has(additionTargetParam)
      if (!sourceFound || !targetFound) {
        // Devices not found yet, wait for more data
        return
      }
      if (additionSourceParam) {
        setAdditionSource(additionSourceParam)
      }
      if (additionTargetParam) {
        setAdditionTarget(additionTargetParam)
      }
      if (mode !== 'whatif-addition') {
        setMode('whatif-addition')
        openPanel('mode')
      }
      // Fly to show selected device(s)
      const sourceDevice = additionSourceParam ? deviceMap.get(additionSourceParam) : undefined
      const targetDevice = additionTargetParam ? deviceMap.get(additionTargetParam) : undefined
      const sourceMetro = sourceDevice ? metroMap.get(sourceDevice.metro_pk) : undefined
      const targetMetro = targetDevice ? metroMap.get(targetDevice.metro_pk) : undefined
      if (sourceMetro && targetMetro && mapRef.current) {
        // Two devices - fly to midpoint
        let aLng = sourceMetro.longitude
        let zLng = targetMetro.longitude
        const lngDelta = zLng - aLng
        if (Math.abs(lngDelta) > 180) {
          zLng = lngDelta > 0 ? zLng - 360 : zLng + 360
        }
        let midLng = (aLng + zLng) / 2
        if (midLng > 180) midLng -= 360
        if (midLng < -180) midLng += 360
        const midLat = (sourceMetro.latitude + targetMetro.latitude) / 2
        mapRef.current.flyTo({ center: [midLng, midLat], zoom: 3, duration: 1000 })
      } else if (sourceMetro && mapRef.current) {
        mapRef.current.flyTo({ center: [sourceMetro.longitude, sourceMetro.latitude], zoom: 4, duration: 1000 })
      } else if (targetMetro && mapRef.current) {
        mapRef.current.flyTo({ center: [targetMetro.longitude, targetMetro.latitude], zoom: 4, duration: 1000 })
      }
      setModeParamsRestored(true)
      return
    }

    // Restore impact mode (comma-separated device PKs)
    if (impactDevicesParam) {
      const devicePKs = impactDevicesParam.split(',').filter(Boolean)
      // Check if all devices are available
      const allFound = devicePKs.every(pk => deviceMap.has(pk))
      if (!allFound) {
        // Some devices not found yet, wait for more data
        return
      }
      // Add each device to the impact list
      for (const pk of devicePKs) {
        if (!impactDevices.includes(pk)) {
          toggleImpactDevice(pk)
        }
      }
      if (mode !== 'impact') {
        setMode('impact')
        openPanel('mode')
      }
      // Fly to fit all impact devices
      if (devicePKs.length > 0 && mapRef.current) {
        // Collect all metro locations for the selected devices
        const locations: { lng: number; lat: number }[] = []
        for (const pk of devicePKs) {
          const device = deviceMap.get(pk)
          const metro = device ? metroMap.get(device.metro_pk) : undefined
          if (metro) {
            locations.push({ lng: metro.longitude, lat: metro.latitude })
          }
        }
        if (locations.length === 1) {
          // Single device - fly to it
          mapRef.current.flyTo({ center: [locations[0].lng, locations[0].lat], zoom: 4, duration: 1000 })
        } else if (locations.length > 1) {
          // Multiple devices - fit bounds to show all
          const lngs = locations.map(l => l.lng)
          const lats = locations.map(l => l.lat)
          const bounds: [[number, number], [number, number]] = [
            [Math.min(...lngs), Math.min(...lats)],
            [Math.max(...lngs), Math.max(...lats)],
          ]
          mapRef.current.fitBounds(bounds, { padding: 100, duration: 1000 })
        }
      }
      setModeParamsRestored(true)
      return
    }

    // No mode params to restore
    setModeParamsRestored(true)
  }, [modeParamsRestored, searchParams, deviceMap, linkMap, metroMap, mapReady, mode, setMode, openPanel, impactDevices, toggleImpactDevice])

  // Restore selected item from URL params when they change
  // Track last processed params to avoid re-processing the same selection
  const lastProcessedParamsRef = useRef<string | null>(null)
  useEffect(() => {
    const type = searchParams.get('type')
    const id = searchParams.get('id')

    // No params to restore
    if (!type || !id) {
      return
    }

    // Wait for map to be ready before restoring selection (need map for flyTo)
    if (!mapReady) {
      return
    }

    // Skip if we already processed these exact params
    const paramsKey = `${type}:${id}`
    if (lastProcessedParamsRef.current === paramsKey) {
      return
    }

    // Helper to fly to location (gentle zoom - just enough to see the item)
    const getPanelOffsetX = () => {
      if (!panel.isOpen || panel.content !== 'details') return 0
      const sidebarCollapsed = typeof window === 'undefined'
        ? true
        : window.localStorage.getItem('sidebar-collapsed') !== 'false'
      const sidebarWidth = sidebarCollapsed ? 48 : 256
      const leftPad = sidebarWidth + panel.width + 16
      const rightPad = 176 + 16 // Controls panel max width + margin
      return Math.round((leftPad - rightPad) / 2)
    }

    const flyToLocation = (lng: number, lat: number, zoom = 3, usePanelOffset = false) => {
      if (mapRef.current) {
        mapRef.current.flyTo({
          center: [lng, lat],
          zoom,
          duration: 1000,
          offset: usePanelOffset ? [getPanelOffsetX(), 0] : [0, 0],
        })
      }
    }

    const linkFitsInView = (aLng: number, aLat: number, zLng: number, zLat: number) => {
      if (!mapRef.current) return false
      const bounds = mapRef.current.getMap().getBounds()
      return bounds.contains([aLng, aLat]) && bounds.contains([zLng, zLat])
    }

    let itemFound = false

    if (type === 'validator') {
      const validator = validatorMap.get(id)
      if (validator) {
        const device = deviceMap.get(validator.device_pk)
        const metro = device ? metroMap.get(device.metro_pk) : undefined
        setSelectedItemState({
          type: 'validator',
          data: {
            votePubkey: validator.vote_pubkey,
            nodePubkey: validator.node_pubkey,
            tunnelId: validator.tunnel_id,
            city: validator.city || 'Unknown',
            country: validator.country || 'Unknown',
            stakeSol: (validator.stake_sol ?? 0) >= 1e6 ? `${(validator.stake_sol / 1e6).toFixed(2)}M` : (validator.stake_sol ?? 0) >= 1e3 ? `${(validator.stake_sol / 1e3).toFixed(0)}k` : `${(validator.stake_sol ?? 0).toFixed(0)}`,
            stakeShare: (validator.stake_share ?? 0) > 0 ? `${validator.stake_share.toFixed(2)}%` : '0%',
            commission: validator.commission ?? 0,
            version: validator.version || '',
            gossipIp: validator.gossip_ip || '',
            gossipPort: validator.gossip_port ?? 0,
            tpuQuicIp: validator.tpu_quic_ip || '',
            tpuQuicPort: validator.tpu_quic_port ?? 0,
            deviceCode: device?.code || 'Unknown',
            devicePk: validator.device_pk,
            metroPk: device?.metro_pk || '',
            metroName: metro?.name || 'Unknown',
            inRate: formatTrafficRate(validator.in_bps),
            outRate: formatTrafficRate(validator.out_bps),
          },
        })
        if (!showValidators) toggleOverlay('validators') // Show validators layer when loading a validator from URL
        // Fly to validator's metro location (skip in analysis modes)
        if (metro && !pathModeEnabled && !impactMode) {
          flyToLocation(metro.longitude, metro.latitude, 4)
        }
        itemFound = true
      }
    } else if (type === 'device') {
      const device = deviceMap.get(id)
      if (device) {
        const metro = metroMap.get(device.metro_pk)
        setSelectedItemState({
          type: 'device',
          data: {
            pk: device.pk,
            code: device.code,
            deviceType: device.device_type,
            status: device.status,
            metroPk: device.metro_pk,
            metroName: metro?.name || 'Unknown',
            contributorPk: device.contributor_pk,
            contributorCode: device.contributor_code,
            userCount: device.user_count ?? 0,
            validatorCount: device.validator_count ?? 0,
            stakeSol: (device.stake_sol ?? 0) >= 1e6 ? `${(device.stake_sol / 1e6).toFixed(2)}M` : (device.stake_sol ?? 0) >= 1e3 ? `${(device.stake_sol / 1e3).toFixed(0)}k` : `${(device.stake_sol ?? 0).toFixed(0)}`,
            stakeShare: (device.stake_share ?? 0) > 0 ? `${device.stake_share.toFixed(2)}%` : '0%',
          },
        })
        // Fly to device's metro location (skip in analysis modes)
        if (metro && !pathModeEnabled && !impactMode) {
          flyToLocation(metro.longitude, metro.latitude, 4)
        }
        itemFound = true

        // Handle mode-specific actions for device selection via search
        if (impactMode) {
          if (!impactDevices.includes(id)) {
            toggleImpactDevice(id)
          }
        } else if (pathModeEnabled) {
          if (!pathSource) {
            setPathSource(id)
          } else if (!pathTarget && id !== pathSource) {
            setPathTarget(id)
          }
        } else if (whatifAdditionMode) {
          if (!additionSource) {
            setAdditionSource(id)
          } else if (!additionTarget && id !== additionSource) {
            setAdditionTarget(id)
          }
        }
      }
    } else if (type === 'link') {
      const link = linkMap.get(id)
      if (link) {
        const deviceA = deviceMap.get(link.side_a_pk)
        const deviceZ = deviceMap.get(link.side_z_pk)
        const hasLatencyData = (link.sample_count ?? 0) > 0
        setSelectedItemState({
          type: 'link',
          data: {
            pk: link.pk,
            code: link.code,
            linkType: link.link_type,
            bandwidth: formatBandwidth(link.bandwidth_bps),
            latencyMs: hasLatencyData ? (link.latency_us > 0 ? `${(link.latency_us / 1000).toFixed(2)}ms` : '0.00ms') : 'N/A',
            jitterMs: hasLatencyData ? ((link.jitter_us ?? 0) > 0 ? `${(link.jitter_us / 1000).toFixed(3)}ms` : '0.000ms') : 'N/A',
            lossPercent: hasLatencyData ? `${(link.loss_percent ?? 0).toFixed(2)}%` : 'N/A',
            inRate: formatTrafficRate(link.in_bps),
            outRate: formatTrafficRate(link.out_bps),
            deviceAPk: link.side_a_pk,
            deviceACode: deviceA?.code || 'Unknown',
            deviceZPk: link.side_z_pk,
            deviceZCode: deviceZ?.code || 'Unknown',
            contributorPk: link.contributor_pk,
            contributorCode: link.contributor_code,
          },
        })
        // Fly to the midpoint of the link (skip in analysis modes)
        if (!pathModeEnabled && !impactMode && !whatifRemovalMode) {
          const metroA = deviceA ? metroMap.get(deviceA.metro_pk) : undefined
          const metroZ = deviceZ ? metroMap.get(deviceZ.metro_pk) : undefined
          if (metroA && metroZ) {
            if (linkFitsInView(metroA.longitude, metroA.latitude, metroZ.longitude, metroZ.latitude)) {
              itemFound = true
            } else {
            let aLng = metroA.longitude
            let zLng = metroZ.longitude
            const lngDelta = zLng - aLng
            if (Math.abs(lngDelta) > 180) {
              zLng = lngDelta > 0 ? zLng - 360 : zLng + 360
            }
            let midLng = (aLng + zLng) / 2
            if (midLng > 180) midLng -= 360
            if (midLng < -180) midLng += 360
            const midLat = (metroA.latitude + metroZ.latitude) / 2
            flyToLocation(midLng, midLat, 3, true)
            }
          } else if (metroA) {
            flyToLocation(metroA.longitude, metroA.latitude, 3, true)
          }
        }
        itemFound = true

        // Handle mode-specific actions for link selection via search
        if (whatifRemovalMode) {
          setRemovalLink({ sourcePK: link.side_a_pk, targetPK: link.side_z_pk, linkPK: link.pk })
        }
      }
    } else if (type === 'metro') {
      const metro = metroMap.get(id)
      if (metro) {
        const metroDeviceCount = devicesByMetro.get(metro.pk)?.length || 0
        setSelectedItemState({
          type: 'metro',
          data: {
            pk: metro.pk,
            code: metro.code,
            name: metro.name,
            deviceCount: metroDeviceCount,
          },
        })
        // Fly to metro location
        flyToLocation(metro.longitude, metro.latitude, 4)
        itemFound = true
      }
    }

    // Open the details panel if item was found (in explore mode)
    if (itemFound && mode === 'explore') {
      openPanel('details')
    }

    // Mark as processed once we've successfully found and selected the item
    if (itemFound) {
      lastProcessedParamsRef.current = paramsKey
    }
  }, [searchParams, mapReady, validatorMap, deviceMap, linkMap, metroMap, devicesByMetro, showValidators, toggleOverlay, mode, openPanel, impactMode, pathModeEnabled, whatifAdditionMode, whatifRemovalMode, pathSource, pathTarget, additionSource, additionTarget])

  // Handle link layer hover
  const handleLinkMouseEnter = useCallback((e: MapLayerMouseEvent) => {
    if (e.features && e.features[0]) {
      const pk = e.features[0].properties?.pk
      const props = e.features[0].properties

      // Handle inter-metro links
      if (pk?.startsWith('inter-metro-') && props?.isInterMetro) {
        setHoveredLink({
          pk,
          code: props.code || '',
          linkType: 'Inter-Metro',
          bandwidth: '',
          latencyMs: props.avgLatencyMs || 'N/A',
          jitterMs: '',
          lossPercent: '',
          inRate: '',
          outRate: '',
          deviceAPk: '',
          deviceACode: '',
          deviceZPk: '',
          deviceZCode: '',
          contributorPk: '',
          contributorCode: '',
          isInterMetro: true,
          linkCount: props.linkCount || 0,
          avgLatencyMs: props.avgLatencyMs || 'N/A',
        })
        return
      }

      const link = linkMap.get(pk)
      if (link) {
        setHoveredLink(buildLinkInfo(link))
      }
    }
  }, [linkMap, buildLinkInfo])

  const handleLinkMouseLeave = useCallback(() => {
    setHoveredLink(null)
  }, [])

  // Track mouse position for cursor-following popover
  const handleMouseMove = useCallback((e: maplibregl.MapMouseEvent) => {
    setMousePos({ x: e.point.x, y: e.point.y })
  }, [])

  return (
    <>
      <MapGL
        ref={mapRef}
        initialViewState={{
          longitude: 0,
          latitude: 30,
          zoom: 2,
        }}
        minZoom={2}
        maxZoom={18}
        mapStyle={mapStyle}
        style={{ width: '100%', height: '100%' }}
        attributionControl={false}
        onClick={handleMapClick}
        interactiveLayerIds={['link-lines', 'link-hit-area']}
        onMouseEnter={handleLinkMouseEnter}
        onMouseLeave={handleLinkMouseLeave}
        onMouseMove={handleMouseMove}
        onLoad={() => setMapReady(true)}
        cursor={hoveredLink ? 'pointer' : undefined}
      >
        <TopologyControlBar
          onZoomIn={handleZoomIn}
          onZoomOut={handleZoomOut}
          onReset={fitBounds}
        />

        {/* Link lines source and layers */}
        <Source id="links" type="geojson" data={linkGeoJson}>
          {/* Invisible hit area for easier clicking */}
          <Layer
            id="link-hit-area"
            type="line"
            paint={{
              'line-color': 'transparent',
              'line-width': 12,
            }}
            layout={{
              'line-cap': 'round',
              'line-join': 'round',
            }}
          />
          {/* Selection halo - renders before main lines */}
          <Layer
            id="link-selection-halo"
            type="line"
            filter={['==', ['get', 'isSelected'], 1]}
            paint={{
              'line-color': '#3b82f6',
              'line-width': ['+', ['get', 'weight'], 8],
              'line-opacity': 0.3,
            }}
            layout={{
              'line-cap': 'round',
              'line-join': 'round',
            }}
          />
          {/* Render each link with its own color - using data-driven styling */}
          <Layer
            id="link-lines"
            type="line"
            paint={{
              'line-color': ['get', 'color'],
              'line-width': ['get', 'weight'],
              'line-opacity': ['get', 'opacity'],
            }}
            layout={{
              'line-cap': 'round',
              'line-join': 'round',
            }}
          />
        </Source>

        {/* Validator links (when toggled, hidden in path mode) */}
        {showValidators && !pathModeEnabled && (
          <Source id="validator-links" type="geojson" data={validatorLinksGeoJson}>
            <Layer
              id="validator-link-lines"
              type="line"
              paint={{
                'line-color': ['get', 'color'],
                'line-width': ['get', 'weight'],
                'line-opacity': ['get', 'opacity'],
                'line-dasharray': [4, 4],
              }}
            />
          </Source>
        )}

        {/* Metro markers - only show when metros overlay is active */}
        {metroClusteringMode && metros.map(metro => {
          const isThisHovered = hoveredMetro?.code === metro.code
          const isThisSelected = selectedItem?.type === 'metro' && selectedItem.data.pk === metro.pk
          const metroDeviceCount = devicesByMetro.get(metro.pk)?.length || 0
          const metroInfo: HoveredMetroInfo = {
            pk: metro.pk,
            code: metro.code,
            name: metro.name,
            deviceCount: metroDeviceCount,
          }

          return (
            <Marker
              key={`metro-${metro.pk}`}
              longitude={metro.longitude}
              latitude={metro.latitude}
              anchor="center"
            >
              <div
                className="rounded-full cursor-pointer transition-all"
                style={{
                  width: isThisSelected ? 28 : isThisHovered ? 26 : 24,
                  height: isThisSelected ? 28 : isThisHovered ? 26 : 24,
                  backgroundColor: metroColor,
                  opacity: isThisHovered || isThisSelected ? 0.5 : 0.3,
                  border: `${isThisSelected ? 3 : isThisHovered ? 2 : 1}px solid ${isThisSelected ? selectionColor : isThisHovered ? hoverHighlight : metroColor}`,
                  boxShadow: isThisSelected
                    ? `0 0 0 4px ${selectionColor}40, 0 0 12px ${selectionColor}60`
                    : isThisHovered
                      ? `0 0 0 2px ${hoverHighlight}40`
                      : undefined,
                }}
                onMouseEnter={() => {
                  setHoveredMetro(metroInfo)
                  setHoveredLink(null)
                }}
                onMouseLeave={() => setHoveredMetro(null)}
                onClick={() => handleMarkerClick({ type: 'metro', data: metroInfo })}
              />
            </Marker>
          )
        })}

        {/* Device markers - show disabled state for non-ISIS devices in path mode */}
        {devices.map(device => {
          const pos = devicePositions.get(device.pk)
          if (!pos) return null

          // Hide device if its metro is collapsed
          if (metroClusteringMode && collapsedMetros.has(device.metro_pk)) {
            return null
          }

          const metro = metroMap.get(device.metro_pk)
          const isThisHovered = hoveredDevice?.code === device.code
          const isThisSelected = selectedItem?.type === 'device' && selectedItem.data.pk === device.pk
          const isPathSource = pathSource === device.pk
          const isPathTarget = pathTarget === device.pk
          const devicePathIndices = devicePathMap.get(device.pk)
          const isInSelectedPath = devicePathIndices?.includes(selectedPathIndex)
          const isInAnyPath = devicePathIndices && devicePathIndices.length > 0
          // Check if device has ISIS data (can participate in path finding)
          const isISISEnabled = isisDevicePKs.size === 0 || isisDevicePKs.has(device.pk)
          const isDisabledInPathMode = pathModeEnabled && !isISISEnabled
          // What-If mode states
          const isAdditionSource = additionSource === device.pk
          const isAdditionTarget = additionTarget === device.pk
          const isDisconnected = disconnectedDevicePKs.has(device.pk)
          // Impact mode state
          const isImpactDevice = impactMode && impactDevices.includes(device.pk)
          const deviceInfo: HoveredDeviceInfo = {
            pk: device.pk,
            code: device.code,
            deviceType: device.device_type,
            status: device.status,
            metroPk: device.metro_pk,
            metroName: metro?.name || 'Unknown',
            contributorPk: device.contributor_pk,
            contributorCode: device.contributor_code,
            userCount: device.user_count ?? 0,
            validatorCount: device.validator_count ?? 0,
            stakeSol: (device.stake_sol ?? 0) >= 1e6 ? `${(device.stake_sol / 1e6).toFixed(2)}M` : (device.stake_sol ?? 0) >= 1e3 ? `${(device.stake_sol / 1e3).toFixed(0)}k` : `${(device.stake_sol ?? 0).toFixed(0)}`,
            stakeShare: (device.stake_share ?? 0) > 0 ? `${device.stake_share.toFixed(2)}%` : '0%',
          }

          // Determine marker styling based on path state
          let markerColor = deviceColor
          let markerSize = 12
          let borderWidth = 1
          let opacity = 0.9
          let borderColor = hoverHighlight

          // Device type mode - color based on device type (default overlay)
          if (deviceTypeMode && !stakeOverlayMode && !metroClusteringMode && !contributorDevicesMode) {
            const deviceType = device.device_type?.toLowerCase() || 'default'
            markerColor = DEVICE_TYPE_COLORS[deviceType] || DEVICE_TYPE_COLORS.default
            borderColor = markerColor
            borderWidth = 2
            opacity = 1
          }

          // Stake overlay mode - size and color based on stake
          if (stakeOverlayMode) {
            const stakeSol = device.stake_sol ?? 0
            const stakeShare = device.stake_share ?? 0
            markerSize = calculateDeviceStakeSize(stakeSol)
            markerColor = getStakeColor(stakeShare)
            borderColor = markerColor
            borderWidth = stakeSol > 0 ? 2 : 1
            opacity = stakeSol > 0 ? 1 : 0.4
          }

          // Metro clustering mode - color based on metro
          if (metroClusteringMode && !stakeOverlayMode && !contributorDevicesMode) {
            const metroIndex = metroIndexMap.get(device.metro_pk) ?? 0
            markerColor = getMetroColor(device.metro_pk, metroIndex)
            borderColor = markerColor
            borderWidth = 2
            opacity = 1
          }

          // Contributor devices mode - color based on contributor
          if (contributorDevicesMode && !stakeOverlayMode && !metroClusteringMode) {
            const contributorIndex = contributorIndexMap.get(device.contributor_pk) ?? 0
            markerColor = getContributorColor(device.contributor_pk, contributorIndex)
            borderColor = markerColor
            borderWidth = 2
            opacity = device.contributor_pk ? 1 : 0.4
          }

          if (isDisabledInPathMode) {
            // Grey out non-ISIS devices in path mode
            markerColor = isDark ? '#4b5563' : '#9ca3af' // gray
            borderColor = markerColor
            markerSize = 10
            opacity = 0.4
          } else if (whatifRemovalMode && isDisconnected) {
            // Disconnected device in whatif-removal mode
            markerColor = '#ef4444' // red
            borderColor = '#ef4444'
            markerSize = 16
            borderWidth = 3
            opacity = 1
          } else if (isAdditionSource) {
            markerColor = '#22c55e' // green
            borderColor = '#22c55e'
            markerSize = 18
            borderWidth = 3
            opacity = 1
          } else if (isAdditionTarget) {
            markerColor = '#ef4444' // red
            borderColor = '#ef4444'
            markerSize = 18
            borderWidth = 3
            opacity = 1
          } else if (isPathSource) {
            markerColor = '#22c55e' // green
            borderColor = '#22c55e'
            markerSize = 18
            borderWidth = 3
            opacity = 1
          } else if (isPathTarget) {
            markerColor = '#ef4444' // red
            borderColor = '#ef4444'
            markerSize = 18
            borderWidth = 3
            opacity = 1
          } else if (isInSelectedPath && devicePathIndices) {
            // Use selected path's color
            markerColor = PATH_COLORS[selectedPathIndex % PATH_COLORS.length]
            borderColor = markerColor
            markerSize = 16
            borderWidth = 2
            opacity = 1
          } else if (isInAnyPath && devicePathIndices) {
            // In another path but not selected
            const firstPathIndex = devicePathIndices[0]
            markerColor = PATH_COLORS[firstPathIndex % PATH_COLORS.length]
            borderColor = markerColor
            markerSize = 14
            borderWidth = 1
            opacity = 0.5
          } else if (isImpactDevice) {
            // Impact device being analyzed for failure
            markerColor = '#ef4444' // red
            borderColor = '#ef4444'
            markerSize = 18
            borderWidth = 3
            opacity = 1
          } else if (isThisSelected) {
            // Selected: blue border with halo
            borderColor = selectionColor
            markerSize = 14
            borderWidth = 3
            opacity = 1
          } else if (isThisHovered) {
            // Hovered: amber highlight
            borderColor = hoverHighlight
            markerSize = 14
            borderWidth = 2
            opacity = 1
          }

          // Selection/impact halo effect
          const boxShadow = isImpactDevice
            ? `0 0 0 4px #ef444440, 0 0 12px #ef444460`
            : isThisSelected
              ? `0 0 0 4px ${selectionColor}40, 0 0 12px ${selectionColor}60`
              : isThisHovered
                ? `0 0 0 2px ${hoverHighlight}40`
                : undefined

          return (
            <Marker
              key={`device-${device.pk}`}
              longitude={pos[0]}
              latitude={pos[1]}
              anchor="center"
            >
              <div
                className={`rounded-full transition-all ${isDisabledInPathMode ? 'cursor-not-allowed' : 'cursor-pointer'}`}
                style={{
                  width: markerSize,
                  height: markerSize,
                  backgroundColor: markerColor,
                  border: `${borderWidth}px solid ${borderColor}`,
                  opacity,
                  boxShadow,
                }}
                onMouseEnter={() => {
                  setHoveredDevice(deviceInfo)
                  setHoveredLink(null) // Clear link hover so device takes priority
                }}
                onMouseLeave={() => setHoveredDevice(null)}
                title={isDisabledInPathMode ? 'No ISIS data - cannot use for path finding' : undefined}
                onClick={() => {
                  markerClickedRef.current = true
                  setTimeout(() => { markerClickedRef.current = false }, 0)
                  if (pathModeEnabled) {
                    // Only allow clicking ISIS-enabled devices in path mode
                    if (!isDisabledInPathMode) {
                      handlePathDeviceClick(device.pk)
                    }
                  } else if (whatifAdditionMode) {
                    // Handle whatif-addition mode device clicks
                    if (!additionSource) {
                      setAdditionSource(device.pk)
                    } else if (!additionTarget && device.pk !== additionSource) {
                      setAdditionTarget(device.pk)
                    } else {
                      // Reset and start new addition
                      setAdditionSource(device.pk)
                      setAdditionTarget(null)
                      setAdditionResult(null)
                    }
                  } else if (impactMode) {
                    // Handle impact mode device clicks - toggle device in/out of selection
                    toggleImpactDevice(device.pk)
                  } else {
                    handleMarkerClick({ type: 'device', data: deviceInfo })
                  }
                }}
              />
            </Marker>
          )
        })}

        {/* Super markers for collapsed metros */}
        {metroClusteringMode && metros.map(metro => {
          if (!collapsedMetros.has(metro.pk)) return null

          const metroIndex = metroIndexMap.get(metro.pk) ?? 0
          const metroColor = getMetroColor(metro.pk, metroIndex)
          const metroDevices = devicesByMetro.get(metro.pk) || []
          const deviceCount = metroDevices.length

          return (
            <Marker
              key={`super-metro-${metro.pk}`}
              longitude={metro.longitude}
              latitude={metro.latitude}
              anchor="center"
            >
              <div
                className="rounded-full cursor-pointer transition-all flex items-center justify-center text-white font-bold text-xs shadow-lg"
                style={{
                  width: 32,
                  height: 32,
                  backgroundColor: metroColor,
                  border: `3px solid ${metroColor}`,
                  opacity: 1,
                }}
                onClick={() => toggleMetroCollapse(metro.pk)}
                title={`${metro.name} (${deviceCount} devices) - Click to expand`}
              >
                {deviceCount}
              </div>
            </Marker>
          )
        })}

        {/* Validator markers (when toggled, hidden in path mode) */}
        {showValidators && !pathModeEnabled && validators.map(validator => {
          const devicePos = devicePositions.get(validator.device_pk)
          if (!devicePos) return null

          const device = deviceMap.get(validator.device_pk)
          const metro = device ? metroMap.get(device.metro_pk) : undefined
          const isThisHovered = hoveredValidator?.votePubkey === validator.vote_pubkey
          const isThisSelected = selectedItem?.type === 'validator' && selectedItem.data.votePubkey === validator.vote_pubkey
          const baseRadius = calculateValidatorRadius(validator.stake_sol)
          const validatorInfo: HoveredValidatorInfo = {
            votePubkey: validator.vote_pubkey,
            nodePubkey: validator.node_pubkey,
            tunnelId: validator.tunnel_id,
            city: validator.city || 'Unknown',
            country: validator.country || 'Unknown',
            stakeSol: (validator.stake_sol ?? 0) >= 1e6 ? `${(validator.stake_sol / 1e6).toFixed(2)}M` : (validator.stake_sol ?? 0) >= 1e3 ? `${(validator.stake_sol / 1e3).toFixed(0)}k` : `${(validator.stake_sol ?? 0).toFixed(0)}`,
            stakeShare: (validator.stake_share ?? 0) > 0 ? `${validator.stake_share.toFixed(2)}%` : '0%',
            commission: validator.commission ?? 0,
            version: validator.version || '',
            gossipIp: validator.gossip_ip || '',
            gossipPort: validator.gossip_port ?? 0,
            tpuQuicIp: validator.tpu_quic_ip || '',
            tpuQuicPort: validator.tpu_quic_port ?? 0,
            deviceCode: device?.code || 'Unknown',
            devicePk: validator.device_pk,
            metroPk: device?.metro_pk || '',
            metroName: metro?.name || 'Unknown',
            inRate: formatTrafficRate(validator.in_bps),
            outRate: formatTrafficRate(validator.out_bps),
          }

          const size = (isThisSelected ? baseRadius + 3 : isThisHovered ? baseRadius + 2 : baseRadius) * 2

          return (
            <Marker
              key={`validator-${validator.vote_pubkey}`}
              longitude={validator.longitude}
              latitude={validator.latitude}
              anchor="center"
            >
              <div
                className="rounded-full cursor-pointer transition-all"
                style={{
                  width: size,
                  height: size,
                  backgroundColor: validatorColor,
                  border: `${isThisSelected ? 3 : isThisHovered ? 2 : 1}px solid ${isThisSelected ? selectionColor : hoverHighlight}`,
                  opacity: isThisHovered || isThisSelected ? 1 : 0.9,
                  boxShadow: isThisSelected
                    ? `0 0 0 4px ${selectionColor}40, 0 0 12px ${selectionColor}60`
                    : isThisHovered
                      ? `0 0 0 2px ${hoverHighlight}40`
                      : undefined,
                }}
                onMouseEnter={() => {
                  setHoveredValidator(validatorInfo)
                  setHoveredLink(null)
                }}
                onMouseLeave={() => setHoveredValidator(null)}
                onClick={() => handleMarkerClick({ type: 'validator', data: validatorInfo })}
              />
            </Marker>
          )
        })}
      </MapGL>

      {/* Hover tooltip - cursor-following */}
      {(hoveredLink || hoveredDevice || hoveredMetro || hoveredValidator) && (
        <div
          className="absolute z-[1000] bg-[var(--card)]/95 backdrop-blur border border-[var(--border)] rounded-md shadow-lg px-3 py-2 pointer-events-none"
          style={{
            left: mousePos.x + 16,
            top: mousePos.y + 16,
          }}
        >
          {hoveredLink && (
            <div className="space-y-1">
              <div className="text-sm font-medium">{hoveredLink.code}</div>
              <div className="text-xs text-muted-foreground space-y-0.5">
                <div>Type: <span className="text-foreground">{hoveredLink.isInterMetro ? 'Inter-Metro' : hoveredLink.linkType}</span></div>
                {hoveredLink.contributorCode && (
                  <div>Contributor: <span className="text-foreground">{hoveredLink.contributorCode}</span></div>
                )}
                {hoveredLink.isInterMetro ? (
                  <>
                    <div>Links: <span className="text-foreground">{hoveredLink.linkCount}</span></div>
                    <div>Avg Latency: <span className="text-foreground">{hoveredLink.avgLatencyMs}</span></div>
                  </>
                ) : (
                  <>
                    <div>Latency: <span className="text-foreground">{hoveredLink.latencyMs}</span></div>
                    <div>Bandwidth: <span className="text-foreground">{hoveredLink.bandwidth}</span></div>
                  </>
                )}
              </div>
            </div>
          )}
          {hoveredDevice && !hoveredLink && (
            <div className="space-y-1">
              <div className="text-sm font-medium">{hoveredDevice.code}</div>
              <div className="text-xs text-muted-foreground space-y-0.5">
                <div>Type: <span className="text-foreground capitalize">{hoveredDevice.deviceType}</span></div>
                {hoveredDevice.contributorCode && (
                  <div>Contributor: <span className="text-foreground">{hoveredDevice.contributorCode}</span></div>
                )}
              </div>
            </div>
          )}
          {hoveredMetro && !hoveredLink && !hoveredDevice && !hoveredValidator && (
            <div>
              <div className="text-xs text-muted-foreground">Metro</div>
              <div className="text-sm font-medium">{hoveredMetro.name}</div>
            </div>
          )}
          {hoveredValidator && !hoveredLink && !hoveredDevice && (
            <div>
              <div className="text-xs text-muted-foreground">Validator</div>
              <div className="text-sm font-medium font-mono">{hoveredValidator.votePubkey.slice(0, 12)}...</div>
            </div>
          )}
        </div>
      )}

      {/* Detail panel (right side) - shown when entity selected in explore mode */}
      {selectedItem && panel.isOpen && panel.content === 'details' && (
        <TopologyPanel
          title={
            selectedItem.type === 'device' ? selectedItem.data.code :
            selectedItem.type === 'link' ? selectedItem.data.code :
            selectedItem.type === 'metro' ? selectedItem.data.name :
            selectedItem.data.votePubkey.substring(0, 12) + '...'
          }
          subtitle={
            selectedItem.type === 'link' ? (
              <span>
                <TopologyEntityLink to={`/dz/devices/${selectedItem.data.deviceAPk}`}>{selectedItem.data.deviceACode}</TopologyEntityLink>
                {' ↔ '}
                <TopologyEntityLink to={`/dz/devices/${selectedItem.data.deviceZPk}`}>{selectedItem.data.deviceZCode}</TopologyEntityLink>
              </span>
            ) : undefined
          }
        >
          {selectedItem.type === 'device' && (
            <DeviceDetails device={selectedItem.data as any} />
          )}
          {selectedItem.type === 'link' && (
            <LinkDetails link={selectedItem.data as any} />
          )}
          {selectedItem.type === 'metro' && (
            <MetroDetails metro={selectedItem.data as any} />
          )}
          {selectedItem.type === 'validator' && (
            <ValidatorDetails validator={selectedItem.data as any} />
          )}
        </TopologyPanel>
      )}

      {/* Mode panel (right side) - shown when in analysis mode */}
      {panel.isOpen && panel.content === 'mode' && (
        <TopologyPanel
          title={
            mode === 'path' ? 'Path Finding' :
            mode === 'whatif-removal' ? 'Simulate Link Removal' :
            mode === 'whatif-addition' ? 'Simulate Link Addition' :
            mode === 'impact' ? 'Device Failure' :
            'Analysis Mode'
          }
          subtitle={
            mode === 'path' ? 'Find shortest paths between two devices by hop count or latency.' :
            mode === 'whatif-removal' ? 'Analyze what happens to network paths if a link is removed.' :
            mode === 'whatif-addition' ? 'See how adding a new link would improve connectivity.' :
            mode === 'impact' ? 'Analyze the impact of a device failure on network paths.' :
            undefined
          }
        >
          {mode === 'path' && (
            <PathModePanel
              pathSource={pathSource}
              pathTarget={pathTarget}
              pathsResult={pathsResult}
              pathLoading={pathLoading}
              pathMode={pathMode}
              selectedPathIndex={selectedPathIndex}
              onPathModeChange={setPathMode}
              onSelectPath={setSelectedPathIndex}
              onClearPath={clearPath}
            />
          )}
          {mode === 'whatif-removal' && (
            <WhatIfRemovalPanel
              removalLink={removalLink}
              result={removalResult}
              isLoading={removalLoading}
              onClear={() => {
                setRemovalLink(null)
                setRemovalResult(null)
              }}
            />
          )}
          {mode === 'whatif-addition' && (
            <WhatIfAdditionPanel
              additionSource={additionSource}
              additionTarget={additionTarget}
              additionMetric={additionMetric}
              result={additionResult}
              isLoading={additionLoading}
              onMetricChange={setAdditionMetric}
              onClear={() => {
                setAdditionSource(null)
                setAdditionTarget(null)
                setAdditionResult(null)
              }}
            />
          )}
          {mode === 'impact' && (
            <ImpactPanel
              devicePKs={impactDevices}
              deviceCodes={deviceCodeMap}
              result={impactResult}
              isLoading={impactLoading}
              onRemoveDevice={toggleImpactDevice}
              onClear={() => {
                clearImpactDevices()
                setImpactResult(null)
              }}
            />
          )}
        </TopologyPanel>
      )}

      {/* Overlay panel (right side) - shown when overlay is active */}
      {panel.isOpen && panel.content === 'overlay' && (
        <TopologyPanel
          title={
            deviceTypeMode ? 'Device Types' :
            linkTypeMode ? 'Link Types' :
            stakeOverlayMode ? 'Stake' :
            isisHealthMode ? 'ISIS' :
            bandwidthMode ? 'Bandwidth' :
            linkHealthMode ? 'Health' :
            trafficFlowMode ? 'Traffic' :
            criticalityOverlayEnabled ? 'Link Criticality' :
            metroClusteringMode ? 'Metros' :
            showValidators ? 'Validators' :
            (contributorDevicesMode || contributorLinksMode) ? 'Contributors' :
            'Overlay'
          }
          subtitle={
            deviceTypeMode ? 'Devices colored by type (router, switch, etc.).' :
            linkTypeMode ? 'Links colored by type (fiber, wavelength, etc.).' :
            stakeOverlayMode ? 'Devices sized by validator stake.' :
            isisHealthMode ? 'Compare current topology to baseline.' :
            bandwidthMode ? 'Links sized by bandwidth capacity.' :
            linkHealthMode ? 'Links colored by latency, jitter, and loss.' :
            trafficFlowMode ? 'Links sized by traffic volume.' :
            criticalityOverlayEnabled ? 'Links ranked by impact if removed.' :
            metroClusteringMode ? 'Devices grouped by metro location.' :
            showValidators ? 'Solana validators on the network.' :
            (contributorDevicesMode || contributorLinksMode) ? 'Devices and links by network contributor.' :
            undefined
          }
        >
          {deviceTypeMode && (
            <DeviceTypeOverlayPanel
              isDark={isDark}
              deviceCounts={devices.reduce((acc, d) => {
                const type = d.device_type?.toLowerCase() || 'unknown'
                acc[type] = (acc[type] || 0) + 1
                return acc
              }, {} as Record<string, number>)}
            />
          )}
          {linkTypeMode && (
            <LinkTypeOverlayPanel
              isDark={isDark}
              linkCounts={links.reduce((acc, l) => {
                const type = l.link_type || 'unknown'
                acc[type] = (acc[type] || 0) + 1
                return acc
              }, {} as Record<string, number>)}
            />
          )}
          {stakeOverlayMode && (
            <StakeOverlayPanel
              deviceStakeMap={deviceStakeMap}
              getStakeColor={getStakeColor}
              getDeviceLabel={(pk) => deviceMap.get(pk)?.code || pk.substring(0, 8)}
              isLoading={devices.length === 0}
            />
          )}
          {bandwidthMode && (
            <BandwidthOverlayPanel />
          )}
          {isisHealthMode && (
            <ComparePanel
              data={compareData ?? null}
              isLoading={compareLoading}
            />
          )}
          {linkHealthMode && (
            <LinkHealthOverlayPanel
              linkHealthData={linkHealthData}
              isLoading={!linkHealthData}
            />
          )}
          {trafficFlowMode && (
            <TrafficFlowOverlayPanel
              edgeTrafficMap={edgeTrafficMap}
              links={links}
              isLoading={links.length === 0}
            />
          )}
          {criticalityOverlayEnabled && (
            <CriticalityPanel
              data={criticalLinksData ?? null}
              isLoading={!criticalLinksData}
            />
          )}
          {metroClusteringMode && (
            <MetroClusteringOverlayPanel
              metroInfoMap={metroInfoMap}
              collapsedMetros={collapsedMetros}
              getMetroColor={(pk) => getMetroColor(pk, metroIndexMap.get(pk) ?? 0)}
              getDeviceCountForMetro={(pk) => devicesByMetro.get(pk)?.length ?? 0}
              totalDeviceCount={devices.length}
              onToggleMetroCollapse={toggleMetroCollapse}
              onCollapseAll={() => setCollapsedMetros(new Set(metroInfoMap.keys()))}
              onExpandAll={() => setCollapsedMetros(new Set())}
              isLoading={metros.length === 0}
            />
          )}
          {showValidators && (
            <ValidatorsOverlayPanel
              validators={validators}
              isLoading={validators.length === 0}
            />
          )}
          {(contributorDevicesMode || contributorLinksMode) && (
            <ContributorsOverlayPanel
              contributorInfoMap={contributorInfoMap}
              getContributorColor={(pk) => getContributorColor(pk, contributorIndexMap.get(pk) ?? 0)}
              getDeviceCountForContributor={(pk) => devicesByContributor.get(pk)?.length ?? 0}
              getLinkCountForContributor={(pk) => linksByContributor.get(pk)?.length ?? 0}
              totalDeviceCount={devices.length}
              totalLinkCount={links.length}
              isLoading={devices.length === 0}
            />
          )}
        </TopologyPanel>
      )}
    </>
  )
}
