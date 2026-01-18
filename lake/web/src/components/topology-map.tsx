import { useMemo, useEffect, useRef, useState, useCallback } from 'react'
import { useSearchParams, Link, useNavigate } from 'react-router-dom'
import MapGL, { Source, Layer, Marker } from 'react-map-gl/maplibre'
import type { MapRef, MapLayerMouseEvent, LngLatBoundsLike } from 'react-map-gl/maplibre'
import type { StyleSpecification } from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { ZoomIn, ZoomOut, Maximize, Users, X, Search, Route, Shield, MinusCircle, PlusCircle, AlertTriangle, Zap, Coins, Activity, MapPin } from 'lucide-react'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { useQuery } from '@tanstack/react-query'
import { useTheme } from '@/hooks/use-theme'
import type { TopologyMetro, TopologyDevice, TopologyLink, TopologyValidator, MultiPathResponse, SimulateLinkRemovalResponse, SimulateLinkAdditionResponse, FailureImpactResponse } from '@/lib/api'
import { fetchISISPaths, fetchISISTopology, fetchCriticalLinks, fetchSimulateLinkRemoval, fetchSimulateLinkAddition, fetchFailureImpact, fetchLinkHealth } from '@/lib/api'

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

// Calculate link weight based on bandwidth (logarithmic scale)
function calculateLinkWeight(bps: number): number {
  if (bps <= 0) return 1
  // Log scale: 1 Gbps = 2, 10 Gbps = 3, 100 Gbps = 4, 400 Gbps = 5
  const gbps = bps / 1e9
  const weight = Math.max(1, Math.min(8, 1 + Math.log10(Math.max(1, gbps)) * 1.5))
  return weight
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

// Calculate link color based on loss percentage
// 0% = blue, 1% = yellow, 5%+ = red, no data = gray
function getLossColor(lossPercent: number | undefined, hasData: boolean, isDark: boolean): string {
  if (!hasData) {
    return isDark ? '#6b7280' : '#9ca3af' // gray for no data
  }

  const loss = lossPercent ?? 0

  // Clamp loss between 0 and 5 for color interpolation
  const t = Math.min(loss / 5, 1)

  // Blue (0%) -> Yellow (1%) -> Red (5%+)
  if (t <= 0.2) {
    // Blue to yellow (0-1% loss)
    const ratio = t / 0.2
    const r = Math.round(59 + ratio * (234 - 59))
    const g = Math.round(130 + ratio * (179 - 130))
    const b = Math.round(246 + ratio * (8 - 246))
    return `rgb(${r}, ${g}, ${b})`
  } else {
    // Yellow to red (1-5% loss)
    const ratio = (t - 0.2) / 0.8
    const r = Math.round(234 + ratio * (239 - 234))
    const g = Math.round(179 - ratio * 179)
    const b = Math.round(8 - ratio * 8)
    return `rgb(${r}, ${g}, ${b})`
  }
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

// Map controls component
interface MapControlsProps {
  onZoomIn: () => void
  onZoomOut: () => void
  onReset: () => void
  showValidators: boolean
  onToggleValidators: () => void
  validatorCount: number
  pathMode: boolean
  onTogglePathMode: () => void
  criticalityMode: boolean
  onToggleCriticalityMode: () => void
  whatifRemovalMode: boolean
  onToggleWhatifRemovalMode: () => void
  whatifAdditionMode: boolean
  onToggleWhatifAdditionMode: () => void
  impactMode: boolean
  onToggleImpactMode: () => void
  selectedDevicePK: string | null
  stakeOverlayMode: boolean
  onToggleStakeOverlayMode: () => void
  linkHealthMode: boolean
  onToggleLinkHealthMode: () => void
  metroClusteringMode: boolean
  onToggleMetroClusteringMode: () => void
}

function MapControls({
  onZoomIn, onZoomOut, onReset, showValidators, onToggleValidators, validatorCount,
  pathMode, onTogglePathMode, criticalityMode, onToggleCriticalityMode,
  whatifRemovalMode, onToggleWhatifRemovalMode, whatifAdditionMode, onToggleWhatifAdditionMode,
  impactMode, onToggleImpactMode, selectedDevicePK,
  stakeOverlayMode, onToggleStakeOverlayMode,
  linkHealthMode, onToggleLinkHealthMode,
  metroClusteringMode, onToggleMetroClusteringMode
}: MapControlsProps) {
  const anyModeActive = pathMode || criticalityMode || whatifRemovalMode || whatifAdditionMode || impactMode
  return (
    <div className="absolute top-4 right-4 z-[1000] flex flex-col gap-1">
      <button
        onClick={() => window.dispatchEvent(new CustomEvent('open-search'))}
        className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors"
        title="Search (Cmd+K)"
      >
        <Search className="h-4 w-4" />
      </button>
      <div className="my-1 border-t border-[var(--border)]" />
      <button
        onClick={onZoomIn}
        className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors"
        title="Zoom in"
      >
        <ZoomIn className="h-4 w-4" />
      </button>
      <button
        onClick={onZoomOut}
        className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors"
        title="Zoom out"
      >
        <ZoomOut className="h-4 w-4" />
      </button>
      <button
        onClick={onReset}
        className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors"
        title="Reset view"
      >
        <Maximize className="h-4 w-4" />
      </button>
      <div className="my-1 border-t border-[var(--border)]" />
      <button
        onClick={onToggleValidators}
        disabled={anyModeActive}
        className={`p-2 border rounded shadow-sm transition-colors ${
          showValidators
            ? 'bg-purple-500/20 border-purple-500/50 text-purple-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${anyModeActive ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={anyModeActive ? 'Disabled in current mode' : `${showValidators ? 'Hide' : 'Show'} validators (${validatorCount})`}
      >
        <Users className="h-4 w-4" />
      </button>
      <button
        onClick={onTogglePathMode}
        disabled={criticalityMode || whatifRemovalMode || whatifAdditionMode}
        className={`p-2 border rounded shadow-sm transition-colors ${
          pathMode
            ? 'bg-amber-500/20 border-amber-500/50 text-amber-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${criticalityMode || whatifRemovalMode || whatifAdditionMode ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={criticalityMode || whatifRemovalMode || whatifAdditionMode ? 'Disabled in current mode' : (pathMode ? 'Exit path finding mode' : 'Enter path finding mode')}
      >
        <Route className="h-4 w-4" />
      </button>
      <button
        onClick={onToggleCriticalityMode}
        disabled={pathMode || whatifRemovalMode || whatifAdditionMode}
        className={`p-2 border rounded shadow-sm transition-colors ${
          criticalityMode
            ? 'bg-red-500/20 border-red-500/50 text-red-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${pathMode || whatifRemovalMode || whatifAdditionMode ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={pathMode || whatifRemovalMode || whatifAdditionMode ? 'Disabled in current mode' : (criticalityMode ? 'Exit criticality mode' : 'Show link criticality (single points of failure)')}
      >
        <Shield className="h-4 w-4" />
      </button>
      <div className="my-1 border-t border-[var(--border)]" />
      <button
        onClick={onToggleWhatifRemovalMode}
        disabled={pathMode || criticalityMode || whatifAdditionMode}
        className={`p-2 border rounded shadow-sm transition-colors ${
          whatifRemovalMode
            ? 'bg-red-500/20 border-red-500/50 text-red-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${pathMode || criticalityMode || whatifAdditionMode ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={pathMode || criticalityMode || whatifAdditionMode ? 'Disabled in current mode' : (whatifRemovalMode ? 'Exit link removal simulation' : 'Simulate removing a link (r)')}
      >
        <MinusCircle className="h-4 w-4" />
      </button>
      <button
        onClick={onToggleWhatifAdditionMode}
        disabled={pathMode || criticalityMode || whatifRemovalMode || impactMode}
        className={`p-2 border rounded shadow-sm transition-colors ${
          whatifAdditionMode
            ? 'bg-green-500/20 border-green-500/50 text-green-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${pathMode || criticalityMode || whatifRemovalMode || impactMode ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={pathMode || criticalityMode || whatifRemovalMode || impactMode ? 'Disabled in current mode' : (whatifAdditionMode ? 'Exit link addition simulation' : 'Simulate adding a new link (a)')}
      >
        <PlusCircle className="h-4 w-4" />
      </button>
      <button
        onClick={onToggleImpactMode}
        disabled={pathMode || criticalityMode || whatifRemovalMode || whatifAdditionMode || (!selectedDevicePK && !impactMode)}
        className={`p-2 border rounded shadow-sm transition-colors ${
          impactMode
            ? 'bg-purple-500/20 border-purple-500/50 text-purple-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${pathMode || criticalityMode || whatifRemovalMode || whatifAdditionMode ? 'opacity-40 cursor-not-allowed' : ''} ${!selectedDevicePK && !impactMode ? 'opacity-50 cursor-not-allowed' : ''}`}
        title={impactMode ? 'Close impact analysis' : selectedDevicePK ? 'Analyze failure impact of selected device' : 'Select a device first'}
      >
        <Zap className="h-4 w-4" />
      </button>
      <div className="my-1 border-t border-[var(--border)]" />
      <button
        onClick={onToggleStakeOverlayMode}
        disabled={anyModeActive}
        className={`p-2 border rounded shadow-sm transition-colors ${
          stakeOverlayMode
            ? 'bg-yellow-500/20 border-yellow-500/50 text-yellow-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${anyModeActive ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={anyModeActive ? 'Disabled in current mode' : (stakeOverlayMode ? 'Hide stake overlay' : 'Show stake distribution (s)')}
      >
        <Coins className="h-4 w-4" />
      </button>
      <button
        onClick={onToggleLinkHealthMode}
        disabled={anyModeActive}
        className={`p-2 border rounded shadow-sm transition-colors ${
          linkHealthMode
            ? 'bg-green-500/20 border-green-500/50 text-green-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${anyModeActive ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={anyModeActive ? 'Disabled in current mode' : (linkHealthMode ? 'Hide link health overlay' : 'Show link health (h)')}
      >
        <Activity className="h-4 w-4" />
      </button>
      <button
        onClick={onToggleMetroClusteringMode}
        disabled={anyModeActive}
        className={`p-2 border rounded shadow-sm transition-colors ${
          metroClusteringMode
            ? 'bg-blue-500/20 border-blue-500/50 text-blue-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        } ${anyModeActive ? 'opacity-40 cursor-not-allowed' : ''}`}
        title={anyModeActive ? 'Disabled in current mode' : (metroClusteringMode ? 'Hide metro colors' : 'Show metro colors (m)')}
      >
        <MapPin className="h-4 w-4" />
      </button>
    </div>
  )
}

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
  const midLng = (start[0] + end[0]) / 2
  const midLat = (start[1] + end[1]) / 2

  // Calculate perpendicular offset for curve
  const dx = end[0] - start[0]
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
    const lng = (1 - t) * (1 - t) * start[0] + 2 * (1 - t) * t * controlLng + t * t * end[0]
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
  const [showValidators, setShowValidators] = useState(false)
  const [selectedItem, setSelectedItemState] = useState<SelectedItem | null>(null)
  const mapRef = useRef<MapRef>(null)
  const markerClickedRef = useRef(false)

  // Path finding state
  const [pathModeEnabled, setPathModeEnabled] = useState(false)
  const [pathSource, setPathSource] = useState<string | null>(null)
  const [pathTarget, setPathTarget] = useState<string | null>(null)
  const [pathsResult, setPathsResult] = useState<MultiPathResponse | null>(null)
  const [pathLoading, setPathLoading] = useState(false)
  const [selectedPathIndex, setSelectedPathIndex] = useState(0)

  // Criticality mode state
  const [criticalityModeEnabled, setCriticalityModeEnabled] = useState(false)

  // What-If Link Removal state
  const [whatifRemovalMode, setWhatifRemovalMode] = useState(false)
  const [removalLink, setRemovalLink] = useState<{ sourcePK: string; targetPK: string; linkPK: string } | null>(null)
  const [removalResult, setRemovalResult] = useState<SimulateLinkRemovalResponse | null>(null)
  const [removalLoading, setRemovalLoading] = useState(false)

  // What-If Link Addition state
  const [whatifAdditionMode, setWhatifAdditionMode] = useState(false)
  const [additionSource, setAdditionSource] = useState<string | null>(null)
  const [additionTarget, setAdditionTarget] = useState<string | null>(null)
  const [additionMetric, setAdditionMetric] = useState<number>(1000)
  const [additionResult, setAdditionResult] = useState<SimulateLinkAdditionResponse | null>(null)
  const [additionLoading, setAdditionLoading] = useState(false)

  // Failure Impact state
  const [impactDevice, setImpactDevice] = useState<string | null>(null)
  const [impactResult, setImpactResult] = useState<FailureImpactResponse | null>(null)
  const [impactLoading, setImpactLoading] = useState(false)

  // Stake Overlay state
  const [stakeOverlayMode, setStakeOverlayMode] = useState(false)

  // Link Health Overlay state
  const [linkHealthMode, setLinkHealthMode] = useState(false)

  // Metro Clustering state
  const [metroClusteringMode, setMetroClusteringMode] = useState(false)
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
    enabled: criticalityModeEnabled,
  })

  // Fetch link health data when link health mode is enabled
  const { data: linkHealthData } = useQuery({
    queryKey: ['link-health'],
    queryFn: fetchLinkHealth,
    enabled: linkHealthMode,
    staleTime: 30000,
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

  // Update URL when selected item changes (push to history for back button support)
  const setSelectedItem = useCallback((item: SelectedItem | null) => {
    setSelectedItemState(item)
    if (item === null) {
      setSearchParams({})
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
    }
  }, [setSearchParams])

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't trigger shortcuts when typing in input fields
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return

      if (e.key === 'Escape' && selectedItem) {
        setSelectedItem(null)
      } else if (e.key === 's' && !pathModeEnabled && !criticalityModeEnabled && !whatifRemovalMode && !whatifAdditionMode && !impactDevice) {
        setStakeOverlayMode(prev => !prev)
      } else if (e.key === 'h' && !pathModeEnabled && !criticalityModeEnabled && !whatifRemovalMode && !whatifAdditionMode && !impactDevice) {
        setLinkHealthMode(prev => !prev)
      } else if (e.key === 'm' && !pathModeEnabled && !criticalityModeEnabled && !whatifRemovalMode && !whatifAdditionMode && !impactDevice) {
        setMetroClusteringMode(prev => !prev)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [selectedItem, setSelectedItem, pathModeEnabled, criticalityModeEnabled, whatifRemovalMode, whatifAdditionMode, impactDevice])

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
    fetchISISPaths(pathSource, pathTarget, 5)
      .then(result => {
        setPathsResult(result)
      })
      .catch(err => {
        setPathsResult({ paths: [], from: pathSource, to: pathTarget, error: err.message })
      })
      .finally(() => {
        setPathLoading(false)
      })
  }, [pathModeEnabled, pathSource, pathTarget])

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

  // Analyze failure impact when impactDevice changes
  useEffect(() => {
    if (!impactDevice) {
      setImpactResult(null)
      return
    }

    setImpactLoading(true)
    fetchFailureImpact(impactDevice)
      .then(result => {
        setImpactResult(result)
      })
      .catch(err => {
        setImpactResult({
          devicePK: impactDevice,
          deviceCode: '',
          unreachableDevices: [],
          unreachableCount: 0,
          error: err.message,
        })
      })
      .finally(() => {
        setImpactLoading(false)
      })
  }, [impactDevice])

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

      const hasLatencyData = (link.sample_count ?? 0) > 0
      const color = getLossColor(link.loss_percent, hasLatencyData, isDark)
      const weight = calculateLinkWeight(link.bandwidth_bps)
      const isHovered = hoveredLink?.pk === link.pk
      const isSelected = selectedItem?.type === 'link' && selectedItem.data.pk === link.pk
      const linkPathIndices = linkPathMap.get(link.pk)
      const isInAnyPath = linkPathIndices && linkPathIndices.length > 0
      const isInSelectedPath = linkPathIndices?.includes(selectedPathIndex)
      const criticality = linkCriticalityMap.get(link.pk)
      const isRemovedLink = removalLink?.linkPK === link.pk

      // Determine display color based on mode
      let displayColor = color
      let displayWeight = weight
      let displayOpacity = 0.8
      let dashArray: number[] = link.link_type === 'WAN' ? [8, 4] : [4, 4]

      if (whatifRemovalMode && isRemovedLink) {
        // Whatif-removal mode: highlight removed link in red dashed
        displayColor = '#ef4444'
        displayWeight = weight + 3
        displayOpacity = 0.6
        dashArray = [6, 4]
      } else if (linkHealthMode) {
        // Link health mode: color by SLA status
        const slaInfo = linkSlaStatus.get(link.pk)
        if (slaInfo) {
          switch (slaInfo.status) {
            case 'healthy':
              displayColor = '#22c55e' // green
              displayWeight = weight + 1
              displayOpacity = 0.9
              break
            case 'warning':
              displayColor = '#eab308' // yellow
              displayWeight = weight + 1
              displayOpacity = 1
              break
            case 'critical':
              displayColor = '#ef4444' // red
              displayWeight = weight + 2
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
      } else if (criticalityModeEnabled && criticality) {
        // Criticality mode: color by criticality level
        displayColor = criticalityColors[criticality]
        displayWeight = criticality === 'critical' ? weight + 3 : criticality === 'important' ? weight + 2 : weight + 1
        displayOpacity = 1
      } else if (isInSelectedPath && linkPathIndices) {
        // Use the selected path's color
        displayColor = PATH_COLORS[selectedPathIndex % PATH_COLORS.length]
        displayWeight = weight + 3
        displayOpacity = 1
      } else if (isInAnyPath && linkPathIndices) {
        // In another path but not selected - use first path's color but dimmed
        const firstPathIndex = linkPathIndices[0]
        displayColor = PATH_COLORS[firstPathIndex % PATH_COLORS.length]
        displayWeight = weight + 1
        displayOpacity = 0.4
      } else if (isHovered || isSelected) {
        displayColor = hoverHighlight
        displayWeight = weight + 2
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
          dashArray,
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
            dashArray: [8, 4],
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
  }, [links, devicePositions, isDark, hoveredLink, selectedItem, hoverHighlight, linkPathMap, selectedPathIndex, criticalityModeEnabled, linkCriticalityMap, whatifRemovalMode, removalLink, linkHealthMode, linkSlaStatus, metroClusteringMode, collapsedMetros, deviceMap, metroMap])

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
  const deviceColor = isDark ? '#f97316' : '#ea580c' // orange
  const metroColor = isDark ? '#4b5563' : '#9ca3af' // gray
  const validatorColor = isDark ? '#a855f7' : '#9333ea' // purple

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

  // Restore selected item from URL params (on initial load and when params change, e.g. back button)
  const lastParamsRef = useRef<string | null>(null)
  useEffect(() => {
    const type = searchParams.get('type')
    const id = searchParams.get('id')
    const paramsKey = type && id ? `${type}:${id}` : null

    // Skip if params haven't changed (avoids loops when we set params ourselves)
    if (paramsKey === lastParamsRef.current) return
    lastParamsRef.current = paramsKey

    // If no params, close the drawer (handles back button to no-drawer state)
    if (!type || !id) {
      setSelectedItemState(null)
      return
    }

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
        setShowValidators(true) // Show validators layer when loading a validator from URL
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
      }
    }
  }, [searchParams, validatorMap, deviceMap, linkMap, metroMap, devicesByMetro])

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
        cursor={hoveredLink ? 'pointer' : undefined}
      >
        <MapControls
          onZoomIn={handleZoomIn}
          onZoomOut={handleZoomOut}
          onReset={fitBounds}
          showValidators={showValidators}
          onToggleValidators={() => setShowValidators(!showValidators)}
          validatorCount={validators.length}
          pathMode={pathModeEnabled}
          onTogglePathMode={() => setPathModeEnabled(!pathModeEnabled)}
          criticalityMode={criticalityModeEnabled}
          onToggleCriticalityMode={() => setCriticalityModeEnabled(!criticalityModeEnabled)}
          whatifRemovalMode={whatifRemovalMode}
          onToggleWhatifRemovalMode={() => setWhatifRemovalMode(!whatifRemovalMode)}
          whatifAdditionMode={whatifAdditionMode}
          onToggleWhatifAdditionMode={() => setWhatifAdditionMode(!whatifAdditionMode)}
          impactMode={!!impactDevice}
          onToggleImpactMode={() => {
            if (impactDevice) {
              setImpactDevice(null)
            } else if (selectedItem?.type === 'device') {
              setImpactDevice(selectedItem.data.pk)
            }
          }}
          selectedDevicePK={selectedItem?.type === 'device' ? selectedItem.data.pk : null}
          stakeOverlayMode={stakeOverlayMode}
          onToggleStakeOverlayMode={() => setStakeOverlayMode(!stakeOverlayMode)}
          linkHealthMode={linkHealthMode}
          onToggleLinkHealthMode={() => setLinkHealthMode(!linkHealthMode)}
          metroClusteringMode={metroClusteringMode}
          onToggleMetroClusteringMode={() => setMetroClusteringMode(!metroClusteringMode)}
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
          {/* Render each link with its own color - using data-driven styling */}
          <Layer
            id="link-lines"
            type="line"
            paint={{
              'line-color': ['get', 'color'],
              'line-width': ['get', 'weight'],
              'line-opacity': ['get', 'opacity'],
              'line-dasharray': [4, 4],
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

        {/* Metro markers - hidden in path mode */}
        {!pathModeEnabled && metros.map(metro => {
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
                  width: isThisHovered || isThisSelected ? 28 : 24,
                  height: isThisHovered || isThisSelected ? 28 : 24,
                  backgroundColor: isThisHovered || isThisSelected ? hoverHighlight : metroColor,
                  opacity: isThisHovered || isThisSelected ? 0.5 : 0.3,
                  border: `${isThisHovered || isThisSelected ? 2 : 1}px solid ${isThisHovered || isThisSelected ? hoverHighlight : metroColor}`,
                }}
                onMouseEnter={() => setHoveredMetro(metroInfo)}
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
          if (metroClusteringMode && !stakeOverlayMode) {
            const metroIndex = metroIndexMap.get(device.metro_pk) ?? 0
            markerColor = getMetroColor(device.metro_pk, metroIndex)
            borderColor = markerColor
            borderWidth = 2
            opacity = 1
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
          } else if (isThisHovered || isThisSelected) {
            markerColor = hoverHighlight
            borderColor = hoverHighlight
            markerSize = 16
            borderWidth = 2
            opacity = 1
          }

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
                }}
                onMouseEnter={() => setHoveredDevice(deviceInfo)}
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
                  } else {
                    handleMarkerClick({ type: 'device', data: deviceInfo })
                    // If impact panel is open, update to show new device's impact
                    if (impactDevice) {
                      setImpactDevice(device.pk)
                    }
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

          const size = (isThisHovered || isThisSelected ? baseRadius + 2 : baseRadius) * 2

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
                  backgroundColor: isThisHovered || isThisSelected ? hoverHighlight : validatorColor,
                  border: `${isThisHovered || isThisSelected ? 2 : 1}px solid ${hoverHighlight}`,
                  opacity: isThisHovered || isThisSelected ? 1 : 0.9,
                }}
                onMouseEnter={() => setHoveredValidator(validatorInfo)}
                onMouseLeave={() => setHoveredValidator(null)}
                onClick={() => handleMarkerClick({ type: 'validator', data: validatorInfo })}
              />
            </Marker>
          )
        })}
      </MapGL>

      {/* Info panel - shows full details on hover (left of controls) */}
      {(hoveredLink || hoveredDevice || hoveredMetro || hoveredValidator) && (
        <div className="absolute top-4 right-16 z-[1000] bg-[var(--card)] border border-[var(--border)] rounded-lg shadow-lg p-4 min-w-[200px]">
          {hoveredLink && (
            <>
              {hoveredLink.isInterMetro ? (
                <>
                  <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Inter-Metro Link</div>
                  <div className="text-sm font-medium mb-2">{hoveredLink.code}</div>
                  <div className="space-y-1 text-xs">
                    <DetailRow label="Links" value={String(hoveredLink.linkCount ?? 0)} />
                    <DetailRow label="Avg Latency" value={hoveredLink.avgLatencyMs ?? 'N/A'} />
                  </div>
                </>
              ) : (
                <>
                  <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Link</div>
                  <div className="text-sm font-medium mb-2">{hoveredLink.code}</div>
                  <div className="space-y-1 text-xs">
                    <DetailRow label="Type" value={hoveredLink.linkType} />
                    <DetailRow label="Contributor" value={hoveredLink.contributorCode || '—'} />
                    <DetailRow label="Bandwidth" value={hoveredLink.bandwidth} />
                    <DetailRow label="Latency" value={hoveredLink.latencyMs} />
                    <DetailRow label="Jitter" value={hoveredLink.jitterMs} />
                    <DetailRow label="Loss" value={hoveredLink.lossPercent} />
                    <DetailRow label="In" value={hoveredLink.inRate} />
                    <DetailRow label="Out" value={hoveredLink.outRate} />
                    <DetailRow label="Side A" value={hoveredLink.deviceACode} />
                    <DetailRow label="Side Z" value={hoveredLink.deviceZCode} />
                    {hoveredLink.health && (
                      <>
                        <div className="border-t border-border mt-2 pt-2">
                          <div className="text-muted-foreground uppercase tracking-wider mb-1">Health</div>
                        </div>
                        <DetailRow label="Committed" value={`${(hoveredLink.health.committedRttNs / 1000000).toFixed(2)}ms`} />
                        <DetailRow
                          label="SLA Ratio"
                          value={<span className={
                            hoveredLink.health.slaRatio >= 2.0 ? 'text-red-500' :
                            hoveredLink.health.slaRatio >= 1.5 ? 'text-yellow-500' : 'text-green-500'
                          }>{(hoveredLink.health.slaRatio * 100).toFixed(0)}%</span>}
                        />
                        <DetailRow
                          label="Pkt Loss"
                          value={<span className={
                            hoveredLink.health.lossPct > 10 ? 'text-red-500' :
                            hoveredLink.health.lossPct > 0.1 ? 'text-yellow-500' : 'text-green-500'
                          }>{hoveredLink.health.lossPct.toFixed(2)}%</span>}
                        />
                        <DetailRow
                          label="Status"
                          value={<span className={
                            hoveredLink.health.status === 'critical' ? 'text-red-500' :
                            hoveredLink.health.status === 'warning' ? 'text-yellow-500' :
                            hoveredLink.health.status === 'healthy' ? 'text-green-500' : 'text-muted-foreground'
                          }>{hoveredLink.health.status}</span>}
                        />
                      </>
                    )}
                  </div>
                </>
              )}
            </>
          )}
          {hoveredDevice && !hoveredLink && (
            <>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Device</div>
              <div className="text-sm font-medium mb-2">{hoveredDevice.code}</div>
              <div className="space-y-1 text-xs">
                <DetailRow label="Type" value={hoveredDevice.deviceType} />
                <DetailRow label="Contributor" value={hoveredDevice.contributorCode || '—'} />
                <DetailRow label="Metro" value={hoveredDevice.metroName} />
                <DetailRow label="Users" value={String(hoveredDevice.userCount)} />
                <DetailRow label="Validators" value={String(hoveredDevice.validatorCount)} />
                <DetailRow label="Stake" value={`${hoveredDevice.stakeSol} SOL`} />
                <DetailRow label="Stake Share" value={hoveredDevice.stakeShare} />
              </div>
            </>
          )}
          {hoveredMetro && !hoveredLink && !hoveredDevice && !hoveredValidator && (
            <>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Metro</div>
              <div className="text-sm font-medium mb-2">{hoveredMetro.name}</div>
              <div className="space-y-1 text-xs">
                <DetailRow label="Code" value={hoveredMetro.code} />
                <DetailRow label="Devices" value={String(hoveredMetro.deviceCount)} />
              </div>
            </>
          )}
          {hoveredValidator && !hoveredLink && !hoveredDevice && (
            <>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Validator</div>
              <div className="text-sm font-medium font-mono mb-2" title={hoveredValidator.votePubkey}>{hoveredValidator.votePubkey.slice(0, 12)}...</div>
              <div className="space-y-1 text-xs">
                <DetailRow label="Location" value={`${hoveredValidator.city}, ${hoveredValidator.country}`} />
                <DetailRow label="Gossip" value={hoveredValidator.gossipIp ? `${hoveredValidator.gossipIp}:${hoveredValidator.gossipPort}` : '—'} />
                <DetailRow label="TPU QUIC" value={hoveredValidator.tpuQuicIp ? `${hoveredValidator.tpuQuicIp}:${hoveredValidator.tpuQuicPort}` : '—'} />
                <DetailRow label="Stake" value={`${hoveredValidator.stakeSol} SOL`} />
                <DetailRow label="Commission" value={`${hoveredValidator.commission}%`} />
                <DetailRow label="Version" value={hoveredValidator.version || '—'} />
                <DetailRow label="Device" value={hoveredValidator.deviceCode} />
                <DetailRow label="In" value={hoveredValidator.inRate} />
                <DetailRow label="Out" value={hoveredValidator.outRate} />
              </div>
            </>
          )}
        </div>
      )}

      {/* Path finding panel */}
      {pathModeEnabled && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-52">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Route className="h-3.5 w-3.5 text-green-500" />
              Path Finding
            </span>
            {(pathSource || pathTarget) && (
              <button onClick={clearPath} className="p-1 hover:bg-[var(--muted)] rounded" title="Clear path">
                <X className="h-3 w-3" />
              </button>
            )}
          </div>

          {/* Device count note */}
          {isisDevicePKs.size > 0 && (
            <div className="text-muted-foreground mb-2">
              {isisDevicePKs.size} ISIS-enabled devices
            </div>
          )}

          {!pathSource && (
            <div className="text-muted-foreground">Click a device to set the <span className="text-green-500 font-medium">source</span></div>
          )}
          {pathSource && !pathTarget && (
            <div className="text-muted-foreground">Click another device to set the <span className="text-red-500 font-medium">target</span></div>
          )}
          {pathLoading && (
            <div className="text-muted-foreground">Finding paths...</div>
          )}
          {pathsResult && !pathsResult.error && pathsResult.paths.length > 0 && (
            <div>
              {/* Path selector buttons */}
              <div className="flex flex-wrap gap-1 mb-2">
                {pathsResult.paths.map((_, index) => (
                  <button
                    key={index}
                    onClick={() => setSelectedPathIndex(index)}
                    className={`px-2 py-1 rounded text-xs transition-colors flex items-center gap-1 ${
                      selectedPathIndex === index
                        ? 'ring-1 ring-foreground/30'
                        : 'hover:bg-[var(--muted)]'
                    }`}
                    style={{
                      backgroundColor: selectedPathIndex === index
                        ? `${PATH_COLORS[index % PATH_COLORS.length]}30`
                        : undefined,
                      color: PATH_COLORS[index % PATH_COLORS.length],
                    }}
                    title={`Path ${index + 1}`}
                  >
                    <span
                      className="w-2 h-2 rounded-full"
                      style={{ backgroundColor: PATH_COLORS[index % PATH_COLORS.length] }}
                    />
                    {index + 1}
                  </button>
                ))}
              </div>

              {/* Selected path details */}
              {pathsResult.paths[selectedPathIndex] && (
                <>
                  <div className="space-y-1 text-muted-foreground">
                    <div>Hops: <span className="text-foreground font-medium">{pathsResult.paths[selectedPathIndex].hopCount}</span></div>
                    <div>Metric: <span className="text-foreground font-medium">{(pathsResult.paths[selectedPathIndex].totalMetric / 1000).toFixed(2)}ms</span></div>
                  </div>
                  <div className="mt-2 pt-2 border-t border-[var(--border)] space-y-0.5 max-h-32 overflow-y-auto">
                    {pathsResult.paths[selectedPathIndex].path.map((hop, i) => (
                      <div key={hop.devicePK} className="flex items-center gap-1">
                        <span className="text-muted-foreground w-4">{i + 1}.</span>
                        <span style={{ color: i === 0 ? '#22c55e' : i === pathsResult.paths[selectedPathIndex].path.length - 1 ? '#ef4444' : PATH_COLORS[selectedPathIndex % PATH_COLORS.length] }}>
                          {hop.deviceCode}
                        </span>
                      </div>
                    ))}
                  </div>
                </>
              )}
            </div>
          )}
          {pathsResult?.error && (
            <div className="text-destructive">{pathsResult.error}</div>
          )}
        </div>
      )}

      {/* Criticality panel */}
      {criticalityModeEnabled && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-52">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Shield className="h-3.5 w-3.5 text-red-500" />
              Link Criticality
            </span>
          </div>

          {!criticalLinksData && (
            <div className="text-muted-foreground">Loading critical links...</div>
          )}

          {criticalLinksData && (
            <>
              {/* Stats */}
              <div className="space-y-1 mb-3">
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5">
                    <span className="w-2 h-2 rounded-full bg-red-500" />
                    Critical
                  </span>
                  <span className="font-medium">
                    {criticalLinksData.links.filter(l => l.criticality === 'critical').length}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5">
                    <span className="w-2 h-2 rounded-full bg-yellow-500" />
                    Important
                  </span>
                  <span className="font-medium">
                    {criticalLinksData.links.filter(l => l.criticality === 'important').length}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5">
                    <span className="w-2 h-2 rounded-full bg-green-500" />
                    Redundant
                  </span>
                  <span className="font-medium">
                    {criticalLinksData.links.filter(l => l.criticality === 'redundant').length}
                  </span>
                </div>
              </div>

              {/* Critical links list */}
              {criticalLinksData.links.filter(l => l.criticality === 'critical').length > 0 && (
                <div className="pt-2 border-t border-[var(--border)]">
                  <div className="text-muted-foreground mb-1">Critical Links:</div>
                  <div className="space-y-0.5 max-h-24 overflow-y-auto">
                    {criticalLinksData.links.filter(l => l.criticality === 'critical').slice(0, 5).map((link, i) => (
                      <div key={i} className="text-red-500">{link.sourceCode} ↔ {link.targetCode}</div>
                    ))}
                    {criticalLinksData.links.filter(l => l.criticality === 'critical').length > 5 && (
                      <div className="text-muted-foreground">
                        +{criticalLinksData.links.filter(l => l.criticality === 'critical').length - 5} more
                      </div>
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      )}

      {/* Stake Overlay Legend */}
      {stakeOverlayMode && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-52">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Coins className="h-3.5 w-3.5 text-yellow-500" />
              Stake Distribution
            </span>
          </div>

          {/* Summary stats */}
          <div className="space-y-1 mb-3">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Devices with stake</span>
              <span className="font-medium">
                {devices.filter(d => (d.stake_sol ?? 0) > 0).length}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Total validators</span>
              <span className="font-medium">
                {devices.reduce((sum, d) => sum + (d.validator_count ?? 0), 0)}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Total stake</span>
              <span className="font-medium">
                {(() => {
                  const total = devices.reduce((sum, d) => sum + (d.stake_sol ?? 0), 0)
                  if (total >= 1e9) return `${(total / 1e9).toFixed(1)}B`
                  if (total >= 1e6) return `${(total / 1e6).toFixed(1)}M`
                  if (total >= 1e3) return `${(total / 1e3).toFixed(0)}k`
                  return total.toFixed(0)
                })()} SOL
              </span>
            </div>
          </div>

          {/* Size legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-2">Device size = stake amount</div>
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-1">
                <div className="w-2 h-2 rounded-full bg-gray-500" />
                <span className="text-muted-foreground">No stake</span>
              </div>
              <div className="flex items-center gap-1">
                <div className="w-3 h-3 rounded-full bg-yellow-500" />
                <span className="text-muted-foreground">100k</span>
              </div>
              <div className="flex items-center gap-1">
                <div className="w-4 h-4 rounded-full bg-orange-500" />
                <span className="text-muted-foreground">1M+</span>
              </div>
            </div>
          </div>

          {/* Top devices by stake */}
          <div className="pt-2 mt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1">Top by stake:</div>
            <div className="space-y-0.5 max-h-24 overflow-y-auto">
              {[...devices]
                .filter(d => (d.stake_sol ?? 0) > 0)
                .sort((a, b) => (b.stake_sol ?? 0) - (a.stake_sol ?? 0))
                .slice(0, 5)
                .map((device) => (
                  <div key={device.pk} className="flex items-center justify-between">
                    <span className="text-yellow-500">{device.code}</span>
                    <span className="text-muted-foreground">
                      {(device.stake_sol ?? 0) >= 1e6 ? `${(device.stake_sol / 1e6).toFixed(1)}M` : `${((device.stake_sol ?? 0) / 1e3).toFixed(0)}k`}
                    </span>
                  </div>
                ))}
            </div>
          </div>
        </div>
      )}

      {/* Link Health Legend */}
      {linkHealthMode && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Activity className="h-3.5 w-3.5 text-green-500" />
              Link Health (SLA)
            </span>
            <button
              onClick={() => setLinkHealthMode(false)}
              className="p-1 hover:bg-[var(--muted)] rounded"
              title="Close"
            >
              <X className="h-3 w-3" />
            </button>
          </div>

          {!linkHealthData && (
            <div className="text-muted-foreground">Loading health data...</div>
          )}

          {linkHealthData && (
            <div className="space-y-3">
              {/* Summary stats */}
              <div className="space-y-1.5">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Total Links</span>
                  <span className="font-medium">{linkHealthData.total_links}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-green-500">Healthy</span>
                  <span className="font-medium text-green-500">{linkHealthData.healthy_count}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-yellow-500">Warning</span>
                  <span className="font-medium text-yellow-500">{linkHealthData.warning_count}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-red-500">Critical</span>
                  <span className="font-medium text-red-500">{linkHealthData.critical_count}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Unknown</span>
                  <span className="font-medium text-muted-foreground">{linkHealthData.unknown_count}</span>
                </div>
              </div>

              {/* Critical links list */}
              {linkHealthData.critical_count > 0 && (
                <div className="pt-2 border-t border-[var(--border)]">
                  <div className="flex items-center gap-1.5 mb-2">
                    <AlertTriangle className="h-3.5 w-3.5 text-red-500" />
                    <span className="font-medium text-red-500">SLA Violations</span>
                  </div>
                  <div className="space-y-1 max-h-24 overflow-y-auto">
                    {linkHealthData.links
                      .filter(l => l.sla_status === 'critical')
                      .slice(0, 5)
                      .map(link => (
                        <div key={link.link_pk} className="text-red-400 truncate text-[10px]">
                          {link.side_a_code} — {link.side_z_code}
                          <span className="text-muted-foreground ml-1">
                            ({(link.avg_rtt_us / 1000).toFixed(1)}ms vs {(link.committed_rtt_ns / 1_000_000).toFixed(1)}ms SLA)
                          </span>
                        </div>
                      ))}
                    {linkHealthData.critical_count > 5 && (
                      <div className="text-muted-foreground">+{linkHealthData.critical_count - 5} more</div>
                    )}
                  </div>
                </div>
              )}

              {/* Legend */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Link Colors</div>
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-green-500 rounded" />
                    <span>Healthy (&lt;80% of SLA)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-yellow-500 rounded" />
                    <span>Warning (80-100% of SLA)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-1 bg-red-500 rounded" />
                    <span>Critical (exceeds SLA)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-gray-400 rounded opacity-50" />
                    <span>Unknown (no data)</span>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Metro Clustering Legend */}
      {metroClusteringMode && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <MapPin className="h-3.5 w-3.5 text-blue-500" />
              Metro Clustering
            </span>
            <button
              onClick={() => setMetroClusteringMode(false)}
              className="p-1 hover:bg-[var(--muted)] rounded"
              title="Close"
            >
              <X className="h-3 w-3" />
            </button>
          </div>

          {/* Collapse/Expand all buttons */}
          <div className="flex gap-2 mb-3">
            <button
              onClick={() => setCollapsedMetros(new Set(metros.map(m => m.pk)))}
              className="flex-1 px-2 py-1 text-[10px] bg-[var(--muted)] hover:bg-[var(--muted)]/80 rounded transition-colors"
            >
              Collapse All
            </button>
            <button
              onClick={() => setCollapsedMetros(new Set())}
              className="flex-1 px-2 py-1 text-[10px] bg-[var(--muted)] hover:bg-[var(--muted)]/80 rounded transition-colors"
            >
              Expand All
            </button>
          </div>

          {/* Summary */}
          <div className="flex items-center justify-between mb-2">
            <span className="text-muted-foreground">Metros</span>
            <span className="font-medium">{metros.length}</span>
          </div>
          <div className="flex items-center justify-between mb-3">
            <span className="text-muted-foreground">Collapsed</span>
            <span className="font-medium">{collapsedMetros.size}</span>
          </div>

          {/* Metro list */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Click to collapse/expand:</div>
            <div className="space-y-1 max-h-40 overflow-y-auto">
              {[...metros]
                .sort((a, b) => a.code.localeCompare(b.code))
                .map((metro) => {
                  const metroIndex = metroIndexMap.get(metro.pk) ?? 0
                  const color = getMetroColor(metro.pk, metroIndex)
                  const isCollapsed = collapsedMetros.has(metro.pk)
                  const deviceCount = devicesByMetro.get(metro.pk)?.length ?? 0
                  return (
                    <div
                      key={metro.pk}
                      className={`flex items-center justify-between cursor-pointer hover:bg-[var(--muted)] rounded px-1 py-0.5 transition-colors ${isCollapsed ? 'opacity-60' : ''}`}
                      onClick={() => toggleMetroCollapse(metro.pk)}
                    >
                      <div className="flex items-center gap-1.5">
                        <div
                          className="w-3 h-3 rounded-full"
                          style={{ backgroundColor: color }}
                        />
                        <span style={{ color }}>{metro.code}</span>
                      </div>
                      <span className="text-muted-foreground">
                        {isCollapsed ? `(${deviceCount})` : deviceCount}
                      </span>
                    </div>
                  )
                })}
            </div>
          </div>
        </div>
      )}

      {/* What-If Link Removal panel */}
      {whatifRemovalMode && (
        <div className="absolute top-[320px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-52">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <MinusCircle className="h-3.5 w-3.5 text-red-500" />
              Simulate Link Removal
            </span>
            {removalLink && (
              <button
                onClick={() => {
                  setRemovalLink(null)
                  setRemovalResult(null)
                }}
                className="p-1 hover:bg-[var(--muted)] rounded"
                title="Clear"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>

          {!removalLink && (
            <div className="text-muted-foreground">Click a link to simulate its removal</div>
          )}

          {removalLoading && (
            <div className="text-muted-foreground">Analyzing impact...</div>
          )}

          {removalResult && !removalResult.error && (
            <div className="space-y-2">
              <div className="text-muted-foreground">
                Removing: <span className="text-foreground font-medium">{removalResult.sourceCode}</span> — <span className="text-foreground font-medium">{removalResult.targetCode}</span>
              </div>

              {removalResult.causesPartition && (
                <div className="p-2 bg-red-500/10 border border-red-500/30 rounded text-red-500 flex items-center gap-1.5">
                  <AlertTriangle className="h-3.5 w-3.5" />
                  <span className="font-medium">Network partition!</span>
                </div>
              )}

              {/* Disconnected devices */}
              {removalResult.disconnectedCount > 0 && (
                <div className="space-y-1">
                  <div className="text-red-500 font-medium">
                    {removalResult.disconnectedCount} device{removalResult.disconnectedCount !== 1 ? 's' : ''} disconnected
                  </div>
                  <div className="space-y-0.5 max-h-24 overflow-y-auto">
                    {removalResult.disconnectedDevices.slice(0, 5).map((device) => (
                      <div key={device.pk} className="text-muted-foreground">
                        {device.code}
                      </div>
                    ))}
                    {removalResult.disconnectedCount > 5 && (
                      <div className="text-muted-foreground">+{removalResult.disconnectedCount - 5} more</div>
                    )}
                  </div>
                </div>
              )}

              {/* Affected paths */}
              {removalResult.affectedPathCount > 0 && (
                <div className="space-y-1 pt-2 border-t border-[var(--border)]">
                  <div className="text-amber-500 font-medium">
                    {removalResult.affectedPathCount} path{removalResult.affectedPathCount !== 1 ? 's' : ''} affected
                  </div>
                  <div className="space-y-1 max-h-32 overflow-y-auto">
                    {removalResult.affectedPaths.slice(0, 5).map((path, i) => (
                      <div key={i} className="text-muted-foreground">
                        <span className="text-foreground">{path.fromCode}</span> → <span className="text-foreground">{path.toCode}</span>
                        <div className="ml-2 text-[10px]">
                          {path.hasAlternate ? (
                            <span className="text-amber-500">
                              {path.beforeHops} → {path.afterHops} hops (+{path.afterHops - path.beforeHops})
                              {path.beforeMetric > 0 && path.afterMetric > 0 && (
                                <span className="ml-1 text-muted-foreground">
                                  | {(path.afterMetric / 1000).toFixed(1)}ms (+{((path.afterMetric - path.beforeMetric) / 1000).toFixed(1)}ms)
                                </span>
                              )}
                            </span>
                          ) : (
                            <span className="text-red-500">No alternate path</span>
                          )}
                        </div>
                      </div>
                    ))}
                    {removalResult.affectedPathCount > 5 && (
                      <div className="text-muted-foreground">+{removalResult.affectedPathCount - 5} more</div>
                    )}
                  </div>
                </div>
              )}

              {removalResult.disconnectedCount === 0 && removalResult.affectedPathCount === 0 && (
                <div className="text-green-500 flex items-center gap-1.5">
                  <div className="w-2 h-2 rounded-full bg-green-500" />
                  Safe to remove - no impact
                </div>
              )}
            </div>
          )}

          {removalResult?.error && (
            <div className="text-destructive">{removalResult.error}</div>
          )}
        </div>
      )}

      {/* What-If Link Addition panel */}
      {whatifAdditionMode && (
        <div className="absolute top-[320px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-52">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <PlusCircle className="h-3.5 w-3.5 text-green-500" />
              Simulate Link Addition
            </span>
            {(additionSource || additionTarget) && (
              <button
                onClick={() => {
                  setAdditionSource(null)
                  setAdditionTarget(null)
                  setAdditionResult(null)
                }}
                className="p-1 hover:bg-[var(--muted)] rounded"
                title="Clear"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>

          {/* Metric input */}
          <div className="mb-2">
            <div className="text-muted-foreground mb-1">Link Latency</div>
            <div className="flex gap-1">
              {[1000, 5000, 10000, 50000].map(m => (
                <button
                  key={m}
                  onClick={() => setAdditionMetric(m)}
                  className={`px-2 py-1 rounded text-[10px] transition-colors ${
                    additionMetric === m
                      ? 'bg-green-500/20 text-green-500'
                      : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                  }`}
                >
                  {m / 1000}ms
                </button>
              ))}
            </div>
          </div>

          {!additionSource && (
            <div className="text-muted-foreground">Click a device to set the <span className="text-green-500 font-medium">source</span></div>
          )}
          {additionSource && !additionTarget && (
            <div className="text-muted-foreground">Click another device to set the <span className="text-red-500 font-medium">target</span></div>
          )}

          {additionLoading && (
            <div className="text-muted-foreground">Analyzing benefits...</div>
          )}

          {additionResult && !additionResult.error && (
            <div className="space-y-2">
              <div className="text-muted-foreground">
                New link: <span className="text-green-500 font-medium">{additionResult.sourceCode}</span> — <span className="text-red-500 font-medium">{additionResult.targetCode}</span>
              </div>

              {additionResult.redundancyCount > 0 && (
                <div className="text-cyan-500 flex items-center gap-1.5">
                  <Shield className="h-3 w-3" />
                  {additionResult.redundancyCount} device{additionResult.redundancyCount !== 1 ? 's' : ''} gain redundancy
                </div>
              )}

              {additionResult.improvedPathCount > 0 && (
                <div className="text-green-500">
                  {additionResult.improvedPathCount} path{additionResult.improvedPathCount !== 1 ? 's' : ''} would improve
                </div>
              )}

              {additionResult.redundancyCount === 0 && additionResult.improvedPathCount === 0 && (
                <div className="text-muted-foreground">No significant improvements</div>
              )}
            </div>
          )}

          {additionResult?.error && (
            <div className="text-destructive">{additionResult.error}</div>
          )}
        </div>
      )}

      {/* Failure Impact panel */}
      {(impactDevice || impactLoading) && (
        <div className="absolute top-[320px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-52">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Zap className="h-3.5 w-3.5 text-purple-500" />
              Failure Impact
            </span>
            <button
              onClick={() => {
                setImpactDevice(null)
                setImpactResult(null)
              }}
              className="p-1 hover:bg-[var(--muted)] rounded"
              title="Close"
            >
              <X className="h-3 w-3" />
            </button>
          </div>

          {impactLoading && (
            <div className="text-muted-foreground">Analyzing impact...</div>
          )}

          {impactResult && !impactResult.error && (
            <div className="space-y-2">
              <div className="text-muted-foreground">
                If <span className="font-medium text-foreground">{impactResult.deviceCode}</span> goes down:
              </div>

              {impactResult.unreachableCount === 0 ? (
                <div className="text-green-500 flex items-center gap-1.5">
                  <div className="w-2 h-2 rounded-full bg-green-500" />
                  No devices would become unreachable
                </div>
              ) : (
                <div className="space-y-2">
                  <div className="text-red-500 font-medium">
                    {impactResult.unreachableCount} device{impactResult.unreachableCount !== 1 ? 's' : ''} would become unreachable
                  </div>
                  <div className="space-y-0.5 max-h-32 overflow-y-auto">
                    {impactResult.unreachableDevices.map(device => (
                      <div key={device.pk} className="flex items-center gap-1.5">
                        <div className={`w-2 h-2 rounded-full ${device.status === 'active' ? 'bg-green-500' : 'bg-red-500'}`} />
                        <span>{device.code}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {impactResult?.error && (
            <div className="text-destructive">{impactResult.error}</div>
          )}
        </div>
      )}

      {/* Detail drawer */}
      {selectedItem && (
        <TopologyDrawer
          selectedItem={selectedItem}
          onClose={() => setSelectedItem(null)}
          isDark={isDark}
        />
      )}
    </>
  )
}

// Traffic data point type
interface TrafficDataPoint {
  time: string
  avgIn: number
  avgOut: number
  peakIn: number
  peakOut: number
}

// Fetch traffic history for a link, device, or validator
async function fetchTrafficHistory(type: 'link' | 'device' | 'validator', pk: string): Promise<TrafficDataPoint[]> {
  const res = await fetch(`/api/topology/traffic?type=${type}&pk=${encodeURIComponent(pk)}`)
  if (!res.ok) return []
  const data = await res.json()
  return data.points || []
}

// Drawer component
interface TopologyDrawerProps {
  selectedItem: SelectedItem
  onClose: () => void
  isDark: boolean
}

function TopologyDrawer({ selectedItem, onClose, isDark }: TopologyDrawerProps) {
  // Drawer width state with localStorage persistence
  const [drawerWidth, setDrawerWidth] = useState(() => {
    const saved = localStorage.getItem('topology-drawer-width')
    return saved ? parseInt(saved, 10) : 320
  })
  const [isResizing, setIsResizing] = useState(false)

  // Track sidebar collapsed state to position drawer correctly
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    const saved = localStorage.getItem('sidebar-collapsed')
    return saved !== 'false' // Default to collapsed if not set
  })

  // Listen for sidebar state changes
  useEffect(() => {
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === 'sidebar-collapsed') {
        setSidebarCollapsed(e.newValue !== 'false')
      }
    }
    window.addEventListener('storage', handleStorageChange)

    // Also poll for changes (for same-tab updates)
    const interval = setInterval(() => {
      const current = localStorage.getItem('sidebar-collapsed')
      setSidebarCollapsed(current !== 'false')
    }, 100)

    return () => {
      window.removeEventListener('storage', handleStorageChange)
      clearInterval(interval)
    }
  }, [])

  // Sidebar width: 48px collapsed, 256px expanded
  const sidebarWidth = sidebarCollapsed ? 48 : 256

  // Handle resize drag
  useEffect(() => {
    if (!isResizing) return

    const handleMouseMove = (e: MouseEvent) => {
      // Calculate new width based on mouse position relative to sidebar
      const newWidth = Math.max(280, Math.min(600, e.clientX - sidebarWidth))
      setDrawerWidth(newWidth)
    }

    const handleMouseUp = () => {
      setIsResizing(false)
      localStorage.setItem('topology-drawer-width', String(drawerWidth))
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    document.body.style.cursor = 'ew-resize'
    document.body.style.userSelect = 'none'

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
  }, [isResizing, drawerWidth])

  // Fetch traffic data for links, devices, and validators (via their tunnel_id)
  const trafficType = selectedItem.type === 'link' ? 'link'
    : selectedItem.type === 'device' ? 'device'
    : selectedItem.type === 'validator' ? 'validator'
    : null
  const trafficPk = selectedItem.type === 'link' ? selectedItem.data.pk
    : selectedItem.type === 'device' ? selectedItem.data.pk
    : selectedItem.type === 'validator' ? String(selectedItem.data.tunnelId)
    : null

  const { data: trafficData } = useQuery({
    queryKey: ['topology-traffic', trafficType, trafficPk],
    queryFn: () => fetchTrafficHistory(trafficType as 'link' | 'device' | 'validator', trafficPk!),
    enabled: !!trafficPk && !!trafficType,
    refetchInterval: 60000,
  })

  const chartColor = isDark ? '#60a5fa' : '#2563eb'
  const chartColorSecondary = isDark ? '#f97316' : '#ea580c'

  // Build stats based on selected item type
  const stats: { label: string; value: React.ReactNode }[] = useMemo(() => {
    if (selectedItem.type === 'link') {
      const link = selectedItem.data
      return [
        { label: 'Contributor', value: link.contributorPk ? <EntityLink to={`/dz/contributors/${link.contributorPk}`}>{link.contributorCode}</EntityLink> : link.contributorCode || '—' },
        { label: 'Bandwidth', value: link.bandwidth },
        { label: 'Latency', value: link.latencyMs },
        { label: 'Jitter', value: link.jitterMs },
        { label: 'Loss', value: link.lossPercent },
        { label: 'Current In', value: link.inRate },
        { label: 'Current Out', value: link.outRate },
      ]
    }
    if (selectedItem.type === 'device') {
      const device = selectedItem.data
      return [
        { label: 'Type', value: device.deviceType },
        { label: 'Contributor', value: device.contributorPk ? <EntityLink to={`/dz/contributors/${device.contributorPk}`}>{device.contributorCode}</EntityLink> : device.contributorCode || '—' },
        { label: 'Metro', value: device.metroPk ? <EntityLink to={`/dz/metros/${device.metroPk}`}>{device.metroName}</EntityLink> : device.metroName },
        { label: 'Users', value: String(device.userCount) },
        { label: 'Validators', value: String(device.validatorCount) },
        { label: 'Stake', value: `${device.stakeSol} SOL` },
        { label: 'Stake Share', value: device.stakeShare },
      ]
    }
    if (selectedItem.type === 'metro') {
      const metro = selectedItem.data
      return [
        { label: 'Code', value: metro.code },
        { label: 'Devices', value: String(metro.deviceCount) },
      ]
    }
    if (selectedItem.type === 'validator') {
      const validator = selectedItem.data
      return [
        { label: 'Location', value: `${validator.city}, ${validator.country}` },
        { label: 'Device', value: validator.devicePk ? <EntityLink to={`/dz/devices/${validator.devicePk}`} className="font-mono">{validator.deviceCode}</EntityLink> : validator.deviceCode },
        { label: 'Metro', value: validator.metroPk ? <EntityLink to={`/dz/metros/${validator.metroPk}`}>{validator.metroName}</EntityLink> : validator.metroName },
        { label: 'Stake', value: `${validator.stakeSol} SOL` },
        { label: 'DZ Stake Share', value: validator.stakeShare },
        { label: 'Commission', value: `${validator.commission}%` },
        { label: 'Version', value: validator.version || '—' },
        { label: 'Current In', value: validator.inRate },
        { label: 'Current Out', value: validator.outRate },
      ]
    }
    return []
  }, [selectedItem])

  const hasTrafficData = selectedItem.type === 'link' || selectedItem.type === 'device' || selectedItem.type === 'validator'

  return (
    <div
      className="absolute top-0 bottom-0 z-[1000] bg-[var(--card)] border-r border-[var(--border)] shadow-xl flex flex-col"
      style={{ width: drawerWidth, left: sidebarWidth }}
    >
      {/* Resize handle */}
      <div
        className="absolute top-0 bottom-0 right-0 w-1 cursor-ew-resize hover:bg-blue-500/50 transition-colors"
        onMouseDown={() => setIsResizing(true)}
      />
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-[var(--border)] min-w-0">
        <div className="min-w-0 flex-1 mr-2">
          <div className="text-xs text-muted-foreground uppercase tracking-wider">
            {selectedItem.type}
          </div>
          <div className="text-sm font-medium min-w-0 flex-1">
            {selectedItem.type === 'link' && (
              <EntityLink to={`/dz/links/${selectedItem.data.pk}`}>
                {selectedItem.data.code}
              </EntityLink>
            )}
            {selectedItem.type === 'device' && (
              <EntityLink to={`/dz/devices/${selectedItem.data.pk}`}>
                {selectedItem.data.code}
              </EntityLink>
            )}
            {selectedItem.type === 'metro' && (
              <EntityLink to={`/dz/metros/${selectedItem.data.pk}`}>
                {selectedItem.data.name}
              </EntityLink>
            )}
            {selectedItem.type === 'validator' && (
              <EntityLink
                to={`/solana/validators/${selectedItem.data.votePubkey}`}
                className="font-mono block truncate"
                title={selectedItem.data.votePubkey}
              >
                {selectedItem.data.votePubkey}
              </EntityLink>
            )}
          </div>
          {selectedItem.type === 'link' && (
            <div className="text-xs text-muted-foreground mt-0.5">
              <EntityLink to={`/dz/devices/${selectedItem.data.deviceAPk}`}>{selectedItem.data.deviceACode}</EntityLink>
              {' ↔ '}
              <EntityLink to={`/dz/devices/${selectedItem.data.deviceZPk}`}>{selectedItem.data.deviceZCode}</EntityLink>
            </div>
          )}
        </div>
        <button
          onClick={onClose}
          className="p-1.5 hover:bg-[var(--muted)] rounded transition-colors"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {/* Stats grid */}
        <div className="grid grid-cols-2 gap-2">
          {stats.map((stat, i) => (
            <div key={i} className="text-center p-2 bg-[var(--muted)]/30 rounded-lg">
              <div className="text-base font-medium tabular-nums tracking-tight">
                {stat.value}
              </div>
              <div className="text-xs text-muted-foreground">{stat.label}</div>
            </div>
          ))}
        </div>

        {/* Validator identity section */}
        {selectedItem.type === 'validator' && (
          <div className="border-t border-[var(--border)] pt-4 space-y-2">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Identity</div>
            <div className="space-y-1.5 text-xs">
              <div>
                <div className="text-muted-foreground mb-0.5">Vote Pubkey</div>
                <EntityLink
                  to={`/solana/validators/${selectedItem.data.votePubkey}`}
                  className="font-mono truncate block"
                  title={selectedItem.data.votePubkey}
                >
                  {selectedItem.data.votePubkey}
                </EntityLink>
              </div>
              <div>
                <div className="text-muted-foreground mb-0.5">Node Pubkey</div>
                <EntityLink
                  to={`/solana/gossip-nodes/${selectedItem.data.nodePubkey}`}
                  className="font-mono truncate block"
                  title={selectedItem.data.nodePubkey}
                >
                  {selectedItem.data.nodePubkey}
                </EntityLink>
              </div>
            </div>
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2 mt-4">Network</div>
            <div className="space-y-1.5 text-xs">
              <div className="flex justify-between">
                <span className="text-muted-foreground">Gossip</span>
                <span className="font-mono">{selectedItem.data.gossipIp ? `${selectedItem.data.gossipIp}:${selectedItem.data.gossipPort}` : '—'}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">TPU QUIC</span>
                <span className="font-mono">{selectedItem.data.tpuQuicIp ? `${selectedItem.data.tpuQuicIp}:${selectedItem.data.tpuQuicPort}` : '—'}</span>
              </div>
            </div>
          </div>
        )}

        {/* Charts section */}
        {!hasTrafficData && (
          <div className="text-sm text-muted-foreground text-center py-4">
            No traffic data available for {selectedItem.type}s
          </div>
        )}

        {hasTrafficData && !trafficData && (
          <div className="text-sm text-muted-foreground text-center py-4">
            Loading traffic data...
          </div>
        )}

        {hasTrafficData && trafficData && trafficData.length === 0 && (
          <div className="text-sm text-muted-foreground text-center py-4">
            No traffic data available
          </div>
        )}

        {hasTrafficData && trafficData && trafficData.length > 0 && (
          <>
            {/* Average Traffic Chart */}
            <div>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
                Avg Traffic Rate (24h)
              </div>
              <div className="h-36">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={trafficData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.5} />
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 9 }}
                      tickLine={false}
                      axisLine={false}
                    />
                    <YAxis
                      tick={{ fontSize: 9 }}
                      tickLine={false}
                      axisLine={false}
                      tickFormatter={(v) => formatChartAxisRate(v)}
                      width={40}
                    />
                    <RechartsTooltip
                      contentStyle={{
                        backgroundColor: 'var(--card)',
                        border: '1px solid var(--border)',
                        borderRadius: '6px',
                        fontSize: '11px',
                      }}
                      formatter={(value) => formatChartTooltipRate(value as number)}
                    />
                    <Line
                      type="monotone"
                      dataKey="avgIn"
                      stroke={chartColor}
                      strokeWidth={1.5}
                      dot={false}
                      name="In"
                    />
                    <Line
                      type="monotone"
                      dataKey="avgOut"
                      stroke={chartColorSecondary}
                      strokeWidth={1.5}
                      dot={false}
                      name="Out"
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
              <div className="flex justify-center gap-4 text-xs mt-1">
                <span className="flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColor }} />
                  In
                </span>
                <span className="flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColorSecondary }} />
                  Out
                </span>
              </div>
            </div>

            {/* Peak Traffic Chart */}
            <div>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
                Peak Traffic Rate (24h)
              </div>
              <div className="h-36">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={trafficData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.5} />
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 9 }}
                      tickLine={false}
                      axisLine={false}
                    />
                    <YAxis
                      tick={{ fontSize: 9 }}
                      tickLine={false}
                      axisLine={false}
                      tickFormatter={(v) => formatChartAxisRate(v)}
                      width={40}
                    />
                    <RechartsTooltip
                      contentStyle={{
                        backgroundColor: 'var(--card)',
                        border: '1px solid var(--border)',
                        borderRadius: '6px',
                        fontSize: '11px',
                      }}
                      formatter={(value) => formatChartTooltipRate(value as number)}
                    />
                    <Line
                      type="monotone"
                      dataKey="peakIn"
                      stroke={chartColor}
                      strokeWidth={1.5}
                      dot={false}
                      name="In"
                    />
                    <Line
                      type="monotone"
                      dataKey="peakOut"
                      stroke={chartColorSecondary}
                      strokeWidth={1.5}
                      dot={false}
                      name="Out"
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
              <div className="flex justify-center gap-4 text-xs mt-1">
                <span className="flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColor }} />
                  In
                </span>
                <span className="flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColorSecondary }} />
                  Out
                </span>
              </div>
            </div>
          </>
        )}

        {/* External link for validators */}
        {selectedItem.type === 'validator' && (
          <div className="pt-2 border-t border-[var(--border)]">
            <a
              href={`https://www.validators.app/validators/${selectedItem.data.nodePubkey}?locale=en&network=mainnet`}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-muted-foreground hover:text-blue-500 hover:underline"
            >
              View on validators.app →
            </a>
          </div>
        )}
      </div>
    </div>
  )
}

function DetailRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4">
      <span className="text-muted-foreground">{label}:</span>
      <span>{value}</span>
    </div>
  )
}

// Entity link component - normal color styling with hover underline
interface EntityLinkProps {
  to: string
  children: React.ReactNode
  className?: string
  title?: string
}

function EntityLink({ to, children, className = '', title }: EntityLinkProps) {
  const navigate = useNavigate()

  const handleClick = (e: React.MouseEvent) => {
    // Support cmd/ctrl-click to open in new tab
    if (e.metaKey || e.ctrlKey) {
      window.open(to, '_blank')
      e.preventDefault()
    } else {
      navigate(to)
      e.preventDefault()
    }
  }

  return (
    <Link
      to={to}
      onClick={handleClick}
      className={`hover:underline cursor-pointer ${className}`}
      title={title}
    >
      {children}
    </Link>
  )
}

// Format rate for chart axis (compact)
function formatChartAxisRate(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)}T`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)}G`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)}M`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)}K`
  return `${bps.toFixed(0)}`
}

// Format rate for chart tooltip (full)
function formatChartTooltipRate(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(2)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(2)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(2)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(2)} Kbps`
  return `${bps.toFixed(0)} bps`
}
