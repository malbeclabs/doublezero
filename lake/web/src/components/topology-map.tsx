import { useMemo, useEffect, useRef, useState, useCallback } from 'react'
import { useSearchParams, Link, useNavigate } from 'react-router-dom'
import MapGL, { Source, Layer, Marker } from 'react-map-gl/maplibre'
import type { MapRef, MapLayerMouseEvent, LngLatBoundsLike } from 'react-map-gl/maplibre'
import type { StyleSpecification } from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { ZoomIn, ZoomOut, Maximize, Users, X, Search } from 'lucide-react'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { useQuery } from '@tanstack/react-query'
import { useTheme } from '@/hooks/use-theme'
import type { TopologyMetro, TopologyDevice, TopologyLink, TopologyValidator } from '@/lib/api'

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
}

// Hovered device info type
interface HoveredDeviceInfo {
  pk: string
  code: string
  deviceType: string
  status: string
  metroPk: string
  metroName: string
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
}

function MapControls({ onZoomIn, onZoomOut, onReset, showValidators, onToggleValidators, validatorCount }: MapControlsProps) {
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
        className={`p-2 border rounded shadow-sm transition-colors ${
          showValidators
            ? 'bg-purple-500/20 border-purple-500/50 text-purple-500'
            : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
        }`}
        title={`${showValidators ? 'Hide' : 'Show'} validators (${validatorCount})`}
      >
        <Users className="h-4 w-4" />
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

  // Close drawer on Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && selectedItem) {
        setSelectedItem(null)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [selectedItem, setSelectedItem])

  // Helper to handle marker clicks - sets flag to prevent map click from clearing selection
  const handleMarkerClick = useCallback((item: SelectedItem) => {
    markerClickedRef.current = true
    setSelectedItem(item)
    setTimeout(() => { markerClickedRef.current = false }, 0)
  }, [setSelectedItem])

  // Hover highlight color: light in dark mode, dark in light mode
  const hoverHighlight = isDark ? '#fff' : '#000'

  // Build metro lookup map
  const metroMap = useMemo(() => {
    const map = new Map<string, TopologyMetro>()
    for (const metro of metros) {
      map.set(metro.pk, metro)
    }
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

  // GeoJSON for link lines
  const linkGeoJson = useMemo(() => {
    const features = links.map(link => {
      const startPos = devicePositions.get(link.side_a_pk)
      const endPos = devicePositions.get(link.side_z_pk)

      if (!startPos || !endPos) return null

      const hasLatencyData = (link.sample_count ?? 0) > 0
      const color = getLossColor(link.loss_percent, hasLatencyData, isDark)
      const weight = calculateLinkWeight(link.bandwidth_bps)
      const isHovered = hoveredLink?.pk === link.pk
      const isSelected = selectedItem?.type === 'link' && selectedItem.data.pk === link.pk

      return {
        type: 'Feature' as const,
        properties: {
          pk: link.pk,
          code: link.code,
          color: isHovered || isSelected ? hoverHighlight : color,
          weight: isHovered || isSelected ? weight + 2 : weight,
          opacity: isHovered || isSelected ? 1 : 0.8,
          dashArray: link.link_type === 'WAN' ? [8, 4] : [4, 4],
        },
        geometry: {
          type: 'LineString' as const,
          coordinates: calculateCurvedPath(startPos, endPos),
        },
      }
    }).filter((f): f is NonNullable<typeof f> => f !== null)

    return {
      type: 'FeatureCollection' as const,
      features,
    }
  }, [links, devicePositions, isDark, hoveredLink, selectedItem, hoverHighlight])

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
    }
  }, [deviceMap])

  // Handle map click to deselect or select links
  const handleMapClick = useCallback((e: MapLayerMouseEvent) => {
    // If a marker was clicked, don't process map click (marker handler takes precedence)
    if (markerClickedRef.current) {
      return
    }
    // Check if a link was clicked
    if (e.features && e.features.length > 0) {
      const feature = e.features[0]
      if (feature.properties?.pk && feature.layer?.id?.includes('link')) {
        const pk = feature.properties.pk
        const link = linkMap.get(pk)
        if (link) {
          handleMarkerClick({ type: 'link', data: buildLinkInfo(link) })
          return
        }
      }
    }
    // Close drawer when clicking empty area
    setSelectedItem(null)
  }, [setSelectedItem, linkMap, buildLinkInfo, handleMarkerClick])

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

        {/* Validator links (when toggled) */}
        {showValidators && (
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

        {/* Metro markers */}
        {metros.map(metro => {
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

        {/* Device markers */}
        {devices.map(device => {
          const pos = devicePositions.get(device.pk)
          if (!pos) return null

          const metro = metroMap.get(device.metro_pk)
          const isThisHovered = hoveredDevice?.code === device.code
          const isThisSelected = selectedItem?.type === 'device' && selectedItem.data.pk === device.pk
          const deviceInfo: HoveredDeviceInfo = {
            pk: device.pk,
            code: device.code,
            deviceType: device.device_type,
            status: device.status,
            metroPk: device.metro_pk,
            metroName: metro?.name || 'Unknown',
            userCount: device.user_count ?? 0,
            validatorCount: device.validator_count ?? 0,
            stakeSol: (device.stake_sol ?? 0) >= 1e6 ? `${(device.stake_sol / 1e6).toFixed(2)}M` : (device.stake_sol ?? 0) >= 1e3 ? `${(device.stake_sol / 1e3).toFixed(0)}k` : `${(device.stake_sol ?? 0).toFixed(0)}`,
            stakeShare: (device.stake_share ?? 0) > 0 ? `${device.stake_share.toFixed(2)}%` : '0%',
          }

          return (
            <Marker
              key={`device-${device.pk}`}
              longitude={pos[0]}
              latitude={pos[1]}
              anchor="center"
            >
              <div
                className="rounded-full cursor-pointer transition-all"
                style={{
                  width: isThisHovered || isThisSelected ? 16 : 12,
                  height: isThisHovered || isThisSelected ? 16 : 12,
                  backgroundColor: isThisHovered || isThisSelected ? hoverHighlight : deviceColor,
                  border: `${isThisHovered || isThisSelected ? 2 : 1}px solid ${hoverHighlight}`,
                  opacity: isThisHovered || isThisSelected ? 1 : 0.9,
                }}
                onMouseEnter={() => setHoveredDevice(deviceInfo)}
                onMouseLeave={() => setHoveredDevice(null)}
                onClick={() => handleMarkerClick({ type: 'device', data: deviceInfo })}
              />
            </Marker>
          )
        })}

        {/* Validator markers (when toggled) */}
        {showValidators && validators.map(validator => {
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
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Link</div>
              <div className="text-sm font-medium mb-2">{hoveredLink.code}</div>
              <div className="space-y-1 text-xs">
                <DetailRow label="Type" value={hoveredLink.linkType} />
                <DetailRow label="Bandwidth" value={hoveredLink.bandwidth} />
                <DetailRow label="Latency" value={hoveredLink.latencyMs} />
                <DetailRow label="Jitter" value={hoveredLink.jitterMs} />
                <DetailRow label="Loss" value={hoveredLink.lossPercent} />
                <DetailRow label="In" value={hoveredLink.inRate} />
                <DetailRow label="Out" value={hoveredLink.outRate} />
                <DetailRow label="Side A" value={hoveredLink.deviceACode} />
                <DetailRow label="Side Z" value={hoveredLink.deviceZCode} />
              </div>
            </>
          )}
          {hoveredDevice && !hoveredLink && (
            <>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Device</div>
              <div className="text-sm font-medium mb-2">{hoveredDevice.code}</div>
              <div className="space-y-1 text-xs">
                <DetailRow label="Type" value={hoveredDevice.deviceType} />
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

function DetailRow({ label, value }: { label: string; value: string }) {
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
