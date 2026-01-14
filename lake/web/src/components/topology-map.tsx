import { useMemo, useEffect, useRef, useState, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { MapContainer, TileLayer, CircleMarker, Polyline, useMap, useMapEvents } from 'react-leaflet'
import { ZoomIn, ZoomOut, Maximize, Users, X } from 'lucide-react'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { useQuery } from '@tanstack/react-query'
import type { Polyline as LeafletPolyline } from 'leaflet'
import { useTheme } from '@/hooks/use-theme'
import type { TopologyMetro, TopologyDevice, TopologyLink, TopologyValidator } from '@/lib/api'
import type { LatLngTuple } from 'leaflet'

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
  deviceA: string
  deviceZ: string
}

// Hovered device info type
interface HoveredDeviceInfo {
  pk: string
  code: string
  deviceType: string
  status: string
  metro: string
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
  deviceCode: string
  devicePk: string
  inRate: string
  outRate: string
}

// Selected item type for drawer
type SelectedItem =
  | { type: 'link'; data: HoveredLinkInfo }
  | { type: 'device'; data: HoveredDeviceInfo }
  | { type: 'metro'; data: HoveredMetroInfo }
  | { type: 'validator'; data: HoveredValidatorInfo }

// Animated polyline component with flowing dashes based on latency
interface AnimatedPolylineProps {
  positions: LatLngTuple[]
  color: string
  weight: number
  latencyUs: number
  isWan: boolean
  isHovered: boolean
  isDark: boolean
  onHover: (hovered: boolean) => void
  onClick?: () => void
}

function AnimatedPolyline({ positions, color, weight, latencyUs, isWan, isHovered, isDark, onHover, onClick }: AnimatedPolylineProps) {
  // Hover color: light in dark mode, dark in light mode
  const hoverColor = isDark ? '#fff' : '#000'
  const polylineRef = useRef<LeafletPolyline>(null)

  useEffect(() => {
    const polyline = polylineRef.current
    if (!polyline) return

    const element = polyline.getElement() as SVGElement | null
    if (!element) return

    // Calculate animation duration based on latency
    // Lower latency = faster animation (shorter duration)
    // Range: 100us -> 0.5s, 10000us -> 3s
    const minLatency = 100
    const maxLatency = 10000
    const clampedLatency = Math.max(minLatency, Math.min(maxLatency, latencyUs || 1000))
    const duration = 0.5 + ((clampedLatency - minLatency) / (maxLatency - minLatency)) * 2.5

    // Apply CSS animation
    element.style.animation = `flowingDash ${duration}s linear infinite`
  }, [latencyUs])

  return (
    <>
      {/* Invisible wider hit area for easier hovering */}
      <Polyline
        positions={positions}
        pathOptions={{
          color: 'transparent',
          weight: Math.max(20, weight * 3),
          opacity: 0,
        }}
        eventHandlers={{
          mouseover: () => onHover(true),
          mouseout: () => onHover(false),
          click: onClick,
        }}
      />
      {/* Visible animated line */}
      <Polyline
        ref={polylineRef}
        positions={positions}
        pathOptions={{
          color: isHovered ? hoverColor : color,
          weight: isHovered ? weight + 2 : weight,
          opacity: isHovered ? 1 : 0.8,
          dashArray: isWan ? '8, 4' : '4, 4',
          lineCap: 'round',
        }}
        interactive={false}
      />
    </>
  )
}

// Map controls component
interface MapControlsProps {
  metros: TopologyMetro[]
  showValidators: boolean
  onToggleValidators: () => void
  validatorCount: number
}

function MapControls({ metros, showValidators, onToggleValidators, validatorCount }: MapControlsProps) {
  const map = useMap()

  const handleZoomIn = () => map.zoomIn()
  const handleZoomOut = () => map.zoomOut()
  const handleReset = () => {
    if (metros.length === 0) return
    const lats = metros.map(m => m.latitude)
    const lngs = metros.map(m => m.longitude)
    map.fitBounds(
      [[Math.min(...lats), Math.min(...lngs)], [Math.max(...lats), Math.max(...lngs)]],
      { padding: [50, 50], maxZoom: 5 }
    )
  }

  return (
    <div className="absolute top-4 right-4 z-[1000] flex flex-col gap-1">
      <button
        onClick={handleZoomIn}
        className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors"
        title="Zoom in"
      >
        <ZoomIn className="h-4 w-4" />
      </button>
      <button
        onClick={handleZoomOut}
        className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors"
        title="Zoom out"
      >
        <ZoomOut className="h-4 w-4" />
      </button>
      <button
        onClick={handleReset}
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

// Component to handle map resize and fit bounds after mount
function MapFitBounds({ metros }: { metros: TopologyMetro[] }) {
  const map = useMap()

  useEffect(() => {
    if (metros.length === 0) return

    // Calculate bounds from metros
    const lats = metros.map(m => m.latitude)
    const lngs = metros.map(m => m.longitude)

    const minLat = Math.min(...lats)
    const maxLat = Math.max(...lats)
    const minLng = Math.min(...lngs)
    const maxLng = Math.max(...lngs)

    // Fit bounds after a short delay to account for container sizing
    const timer = setTimeout(() => {
      map.invalidateSize()
      map.fitBounds(
        [[minLat, minLng], [maxLat, maxLng]],
        { padding: [50, 50], maxZoom: 5 }
      )
    }, 100)

    return () => clearTimeout(timer)
  }, [map, metros])

  return null
}

// Component to handle map clicks (close drawer when clicking empty area)
function MapClickHandler({ onMapClick, markerClickedRef }: { onMapClick: () => void; markerClickedRef: React.RefObject<boolean> }) {
  useMapEvents({
    click: () => {
      // Only close drawer if a marker wasn't just clicked
      if (!markerClickedRef.current) {
        onMapClick()
      }
    },
  })
  return null
}

// Calculate device position with radial offset for multiple devices at same metro
function calculateDevicePosition(
  metroLat: number,
  metroLng: number,
  deviceIndex: number,
  totalDevices: number
): LatLngTuple {
  if (totalDevices === 1) {
    return [metroLat, metroLng]
  }

  // Distribute devices in a circle around metro center
  const radius = 0.3 // degrees offset
  const angle = (2 * Math.PI * deviceIndex) / totalDevices
  const latOffset = radius * Math.cos(angle)
  // Adjust for latitude distortion
  const lngOffset = radius * Math.sin(angle) / Math.cos(metroLat * Math.PI / 180)

  return [metroLat + latOffset, metroLng + lngOffset]
}

// Calculate curved path between two points
function calculateCurvedPath(
  start: LatLngTuple,
  end: LatLngTuple,
  curveOffset: number = 0.15
): LatLngTuple[] {
  const midLat = (start[0] + end[0]) / 2
  const midLng = (start[1] + end[1]) / 2

  // Calculate perpendicular offset for curve
  const dx = end[1] - start[1]
  const dy = end[0] - start[0]
  const length = Math.sqrt(dx * dx + dy * dy)

  if (length === 0) return [start, end]

  const controlLat = midLat + (dx / length) * curveOffset * length
  const controlLng = midLng - (dy / length) * curveOffset * length

  // Generate points along quadratic bezier curve
  const points: LatLngTuple[] = []
  const segments = 20
  for (let i = 0; i <= segments; i++) {
    const t = i / segments
    const lat = (1 - t) * (1 - t) * start[0] + 2 * (1 - t) * t * controlLat + t * t * end[0]
    const lng = (1 - t) * (1 - t) * start[1] + 2 * (1 - t) * t * controlLng + t * t * end[1]
    points.push([lat, lng])
  }
  return points
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
  const markerClickedRef = useRef(false)

  // Update URL when selected item changes
  const setSelectedItem = useCallback((item: SelectedItem | null) => {
    setSelectedItemState(item)
    if (item === null) {
      setSearchParams({}, { replace: true })
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
      setSearchParams(params, { replace: true })
    }
  }, [setSearchParams])

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
    const positions = new Map<string, LatLngTuple>()

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

  // Calculate map center - use Atlantic-centered view for global network
  const mapCenter = useMemo((): LatLngTuple => {
    if (metros.length === 0) return [30, 0]

    // Center shifted right to account for sidebar on the left
    return [30, 0]
  }, [metros])

  // Prepare link data with positions
  const linkData = useMemo(() => {
    return links
      .map(link => {
        const startPos = devicePositions.get(link.side_a_pk)
        const endPos = devicePositions.get(link.side_z_pk)

        if (!startPos || !endPos) return null

        const deviceA = deviceMap.get(link.side_a_pk)
        const deviceZ = deviceMap.get(link.side_z_pk)

        return {
          link,
          path: calculateCurvedPath(startPos, endPos),
          deviceA,
          deviceZ,
        }
      })
      .filter((d): d is NonNullable<typeof d> => d !== null)
  }, [links, devicePositions, deviceMap])

  // Restore selected item from URL params on initial load
  const initialLoadRef = useRef(true)
  useEffect(() => {
    if (!initialLoadRef.current) return
    initialLoadRef.current = false

    const type = searchParams.get('type')
    const id = searchParams.get('id')
    if (!type || !id) return

    if (type === 'validator') {
      const validator = validatorMap.get(id)
      if (validator) {
        const device = deviceMap.get(validator.device_pk)
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
            deviceCode: device?.code || 'Unknown',
            devicePk: validator.device_pk,
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
            metro: metro?.name || 'Unknown',
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
            deviceA: deviceA?.code || 'Unknown',
            deviceZ: deviceZ?.code || 'Unknown',
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

  // Tile layer URL based on theme
  const tileUrl = isDark
    ? 'https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png'
    : 'https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png'

  // Colors
  const deviceColor = isDark ? '#f97316' : '#ea580c' // orange
  const metroColor = isDark ? '#4b5563' : '#9ca3af' // gray
  const validatorColor = isDark ? '#a855f7' : '#9333ea' // purple
  const validatorLinkColor = isDark ? '#7c3aed' : '#6d28d9' // darker purple for links

  return (
    <>
    <MapContainer
      center={mapCenter}
      zoom={3}
      minZoom={2}
      className="h-full w-full"
      scrollWheelZoom={true}
      style={{ background: 'var(--background)' }}
    >
      <MapFitBounds metros={metros} />
      <MapClickHandler onMapClick={() => setSelectedItem(null)} markerClickedRef={markerClickedRef} />
      <MapControls
        metros={metros}
        showValidators={showValidators}
        onToggleValidators={() => setShowValidators(!showValidators)}
        validatorCount={validators.length}
      />
      <TileLayer
        attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>'
        url={tileUrl}
      />

      {/* Metro markers (background circles) */}
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
          <CircleMarker
            key={`metro-${metro.pk}`}
            center={[metro.latitude, metro.longitude]}
            radius={isThisHovered || isThisSelected ? 14 : 12}
            pathOptions={{
              fillColor: isThisHovered || isThisSelected ? hoverHighlight : metroColor,
              fillOpacity: isThisHovered || isThisSelected ? 0.5 : 0.3,
              stroke: true,
              color: isThisHovered || isThisSelected ? hoverHighlight : metroColor,
              weight: isThisHovered || isThisSelected ? 2 : 1,
              opacity: isThisHovered || isThisSelected ? 0.8 : 0.5,
            }}
            eventHandlers={{
              mouseover: () => setHoveredMetro(metroInfo),
              mouseout: () => setHoveredMetro(null),
              click: () => handleMarkerClick({ type: 'metro', data: metroInfo }),
            }}
          />
        )
      })}

      {/* Links */}
      {linkData.map(({ link, path, deviceA, deviceZ }) => {
        const isWan = link.link_type === 'WAN'
        const hasLatencyData = (link.sample_count ?? 0) > 0
        const color = getLossColor(link.loss_percent, hasLatencyData, isDark)
        const weight = calculateLinkWeight(link.bandwidth_bps)
        const isThisHovered = hoveredLink?.code === link.code
        const isThisSelected = selectedItem?.type === 'link' && selectedItem.data.pk === link.pk
        const linkInfo: HoveredLinkInfo = {
          pk: link.pk,
          code: link.code,
          linkType: link.link_type,
          bandwidth: formatBandwidth(link.bandwidth_bps),
          latencyMs: hasLatencyData ? (link.latency_us > 0 ? `${(link.latency_us / 1000).toFixed(2)}ms` : '0.00ms') : 'N/A',
          jitterMs: hasLatencyData ? ((link.jitter_us ?? 0) > 0 ? `${(link.jitter_us / 1000).toFixed(3)}ms` : '0.000ms') : 'N/A',
          lossPercent: hasLatencyData ? `${(link.loss_percent ?? 0).toFixed(2)}%` : 'N/A',
          inRate: formatTrafficRate(link.in_bps),
          outRate: formatTrafficRate(link.out_bps),
          deviceA: deviceA?.code || 'Unknown',
          deviceZ: deviceZ?.code || 'Unknown',
        }

        return (
          <AnimatedPolyline
            key={`link-${link.pk}`}
            positions={path}
            color={color}
            weight={weight}
            latencyUs={link.latency_us}
            isWan={isWan}
            isHovered={isThisHovered || isThisSelected}
            isDark={isDark}
            onHover={(hovered) => {
              if (hovered) {
                setHoveredLink(linkInfo)
              } else {
                setHoveredLink(null)
              }
            }}
            onClick={() => handleMarkerClick({ type: 'link', data: linkInfo })}
          />
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
          metro: metro?.name || 'Unknown',
          userCount: device.user_count ?? 0,
          validatorCount: device.validator_count ?? 0,
          stakeSol: (device.stake_sol ?? 0) >= 1e6 ? `${(device.stake_sol / 1e6).toFixed(2)}M` : (device.stake_sol ?? 0) >= 1e3 ? `${(device.stake_sol / 1e3).toFixed(0)}k` : `${(device.stake_sol ?? 0).toFixed(0)}`,
          stakeShare: (device.stake_share ?? 0) > 0 ? `${device.stake_share.toFixed(2)}%` : '0%',
        }

        return (
          <CircleMarker
            key={`device-${device.pk}`}
            center={pos}
            radius={isThisHovered || isThisSelected ? 8 : 6}
            pathOptions={{
              fillColor: isThisHovered || isThisSelected ? hoverHighlight : deviceColor,
              fillOpacity: 0.9,
              stroke: true,
              color: hoverHighlight,
              weight: isThisHovered || isThisSelected ? 2 : 1,
              opacity: isThisHovered || isThisSelected ? 1 : 0.5,
            }}
            eventHandlers={{
              mouseover: () => setHoveredDevice(deviceInfo),
              mouseout: () => setHoveredDevice(null),
              click: () => handleMarkerClick({ type: 'device', data: deviceInfo }),
            }}
          />
        )
      })}

      {/* Validator connections (shown when toggled) */}
      {showValidators && validators.map(validator => {
        const devicePos = devicePositions.get(validator.device_pk)
        if (!devicePos) return null

        const validatorPos: LatLngTuple = [validator.latitude, validator.longitude]
        const isThisHovered = hoveredValidator?.votePubkey === validator.vote_pubkey

        return (
          <Polyline
            key={`validator-link-${validator.vote_pubkey}`}
            positions={[validatorPos, devicePos]}
            pathOptions={{
              color: isThisHovered ? hoverHighlight : validatorLinkColor,
              weight: isThisHovered ? 2 : 1,
              opacity: isThisHovered ? 0.9 : 0.4,
              dashArray: '4, 4',
            }}
          />
        )
      })}

      {/* Validator markers (shown when toggled) */}
      {showValidators && validators.map(validator => {
        const devicePos = devicePositions.get(validator.device_pk)
        if (!devicePos) return null

        const validatorPos: LatLngTuple = [validator.latitude, validator.longitude]
        const device = deviceMap.get(validator.device_pk)
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
          deviceCode: device?.code || 'Unknown',
          devicePk: validator.device_pk,
          inRate: formatTrafficRate(validator.in_bps),
          outRate: formatTrafficRate(validator.out_bps),
        }

        return (
          <CircleMarker
            key={`validator-${validator.vote_pubkey}`}
            center={validatorPos}
            radius={isThisHovered || isThisSelected ? baseRadius + 2 : baseRadius}
            pathOptions={{
              fillColor: isThisHovered || isThisSelected ? hoverHighlight : validatorColor,
              fillOpacity: 0.9,
              stroke: true,
              color: hoverHighlight,
              weight: isThisHovered || isThisSelected ? 2 : 1,
              opacity: isThisHovered || isThisSelected ? 1 : 0.5,
            }}
            eventHandlers={{
              mouseover: () => setHoveredValidator(validatorInfo),
              mouseout: () => setHoveredValidator(null),
              click: () => handleMarkerClick({ type: 'validator', data: validatorInfo }),
            }}
          />
        )
      })}
    </MapContainer>

    {/* Info panel - shows full details on hover (top right) */}
    {(hoveredLink || hoveredDevice || hoveredMetro || hoveredValidator) && (
      <div className="absolute top-4 right-4 z-[1000] bg-[var(--card)] border border-[var(--border)] rounded-lg shadow-lg p-4 min-w-[200px]">
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
              <DetailRow label="Side A" value={hoveredLink.deviceA} />
              <DetailRow label="Side Z" value={hoveredLink.deviceZ} />
            </div>
          </>
        )}
        {hoveredDevice && !hoveredLink && (
          <>
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Device</div>
            <div className="text-sm font-medium mb-2">{hoveredDevice.code}</div>
            <div className="space-y-1 text-xs">
              <DetailRow label="Type" value={hoveredDevice.deviceType} />
              <DetailRow label="Metro" value={hoveredDevice.metro} />
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
            <div className="text-sm font-medium font-mono mb-2">{hoveredValidator.votePubkey.slice(0, 12)}...</div>
            <div className="space-y-1 text-xs">
              <DetailRow label="Location" value={`${hoveredValidator.city}, ${hoveredValidator.country}`} />
              <DetailRow label="Stake" value={`${hoveredValidator.stakeSol} SOL`} />
              <DetailRow label="DZ Stake Share" value={hoveredValidator.stakeShare} />
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
  const stats = useMemo(() => {
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
        { label: 'Metro', value: device.metro },
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
        { label: 'Device', value: validator.deviceCode },
        { label: 'Stake', value: `${validator.stakeSol} SOL` },
        { label: 'DZ Stake Share', value: validator.stakeShare },
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
            {selectedItem.type === 'link' && selectedItem.data.code}
            {selectedItem.type === 'device' && selectedItem.data.code}
            {selectedItem.type === 'metro' && selectedItem.data.name}
            {selectedItem.type === 'validator' && (
              <span
                className="font-mono block truncate"
                title={selectedItem.data.votePubkey}
              >
                {selectedItem.data.votePubkey}
              </span>
            )}
          </div>
          {selectedItem.type === 'link' && (
            <div className="text-xs text-muted-foreground mt-0.5">
              {selectedItem.data.deviceA} ↔ {selectedItem.data.deviceZ}
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
