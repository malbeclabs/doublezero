import { useMemo, useEffect, useRef, useState } from 'react'
import { MapContainer, TileLayer, CircleMarker, Polyline, useMap } from 'react-leaflet'
import { ZoomIn, ZoomOut, Maximize, Users } from 'lucide-react'
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
  code: string
  name: string
  deviceCount: number
}

// Hovered validator info type
interface HoveredValidatorInfo {
  votePubkey: string
  city: string
  country: string
  stakeSol: string
  stakeShare: string
  deviceCode: string
}

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
}

function AnimatedPolyline({ positions, color, weight, latencyUs, isWan, isHovered, isDark, onHover }: AnimatedPolylineProps) {
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
  const [hoveredLink, setHoveredLink] = useState<HoveredLinkInfo | null>(null)
  const [hoveredDevice, setHoveredDevice] = useState<HoveredDeviceInfo | null>(null)
  const [hoveredMetro, setHoveredMetro] = useState<HoveredMetroInfo | null>(null)
  const [hoveredValidator, setHoveredValidator] = useState<HoveredValidatorInfo | null>(null)
  const [showValidators, setShowValidators] = useState(false)

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
        const metroDeviceCount = devicesByMetro.get(metro.pk)?.length || 0

        return (
          <CircleMarker
            key={`metro-${metro.pk}`}
            center={[metro.latitude, metro.longitude]}
            radius={isThisHovered ? 14 : 12}
            pathOptions={{
              fillColor: isThisHovered ? hoverHighlight : metroColor,
              fillOpacity: isThisHovered ? 0.5 : 0.3,
              stroke: true,
              color: isThisHovered ? hoverHighlight : metroColor,
              weight: isThisHovered ? 2 : 1,
              opacity: isThisHovered ? 0.8 : 0.5,
            }}
            eventHandlers={{
              mouseover: () => setHoveredMetro({
                code: metro.code,
                name: metro.name,
                deviceCount: metroDeviceCount,
              }),
              mouseout: () => setHoveredMetro(null),
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

        return (
          <AnimatedPolyline
            key={`link-${link.pk}`}
            positions={path}
            color={color}
            weight={weight}
            latencyUs={link.latency_us}
            isWan={isWan}
            isHovered={isThisHovered}
            isDark={isDark}
            onHover={(hovered) => {
              if (hovered) {
                setHoveredLink({
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
                })
              } else {
                setHoveredLink(null)
              }
            }}
          />
        )
      })}

      {/* Device markers */}
      {devices.map(device => {
        const pos = devicePositions.get(device.pk)
        if (!pos) return null

        const metro = metroMap.get(device.metro_pk)
        const isThisHovered = hoveredDevice?.code === device.code

        return (
          <CircleMarker
            key={`device-${device.pk}`}
            center={pos}
            radius={isThisHovered ? 8 : 6}
            pathOptions={{
              fillColor: isThisHovered ? hoverHighlight : deviceColor,
              fillOpacity: 0.9,
              stroke: true,
              color: hoverHighlight,
              weight: isThisHovered ? 2 : 1,
              opacity: isThisHovered ? 1 : 0.5,
            }}
            eventHandlers={{
              mouseover: () => setHoveredDevice({
                code: device.code,
                deviceType: device.device_type,
                status: device.status,
                metro: metro?.name || 'Unknown',
                userCount: device.user_count ?? 0,
                validatorCount: device.validator_count ?? 0,
                stakeSol: (device.stake_sol ?? 0) >= 1e6 ? `${(device.stake_sol / 1e6).toFixed(2)}M` : (device.stake_sol ?? 0) >= 1e3 ? `${(device.stake_sol / 1e3).toFixed(0)}k` : `${(device.stake_sol ?? 0).toFixed(0)}`,
                stakeShare: (device.stake_share ?? 0) > 0 ? `${device.stake_share.toFixed(2)}%` : '0%',
              }),
              mouseout: () => setHoveredDevice(null),
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
        const baseRadius = calculateValidatorRadius(validator.stake_sol)

        return (
          <CircleMarker
            key={`validator-${validator.vote_pubkey}`}
            center={validatorPos}
            radius={isThisHovered ? baseRadius + 2 : baseRadius}
            pathOptions={{
              fillColor: isThisHovered ? hoverHighlight : validatorColor,
              fillOpacity: 0.9,
              stroke: true,
              color: hoverHighlight,
              weight: isThisHovered ? 2 : 1,
              opacity: isThisHovered ? 1 : 0.5,
            }}
            eventHandlers={{
              mouseover: () => setHoveredValidator({
                votePubkey: validator.vote_pubkey,
                city: validator.city || 'Unknown',
                country: validator.country || 'Unknown',
                stakeSol: (validator.stake_sol ?? 0) >= 1e6 ? `${(validator.stake_sol / 1e6).toFixed(2)}M` : (validator.stake_sol ?? 0) >= 1e3 ? `${(validator.stake_sol / 1e3).toFixed(0)}k` : `${(validator.stake_sol ?? 0).toFixed(0)}`,
                stakeShare: (validator.stake_share ?? 0) > 0 ? `${validator.stake_share.toFixed(2)}%` : '0%',
                deviceCode: device?.code || 'Unknown',
              }),
              mouseout: () => setHoveredValidator(null),
            }}
          />
        )
      })}
    </MapContainer>

    {/* Info panel */}
    {(hoveredLink || hoveredDevice || hoveredMetro || hoveredValidator) && (
      <div className="absolute top-4 right-16 z-[1000] bg-[var(--card)] border border-[var(--border)] rounded-lg shadow-lg p-4 min-w-[200px]">
        {hoveredLink && (
          <>
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Link</div>
            <div className="text-sm font-medium mb-2">{hoveredLink.code}</div>
            <div className="space-y-1 text-xs">
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Type:</span>
                <span>{hoveredLink.linkType}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Bandwidth:</span>
                <span>{hoveredLink.bandwidth}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Latency:</span>
                <span>{hoveredLink.latencyMs}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Jitter:</span>
                <span>{hoveredLink.jitterMs}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Loss:</span>
                <span>{hoveredLink.lossPercent}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">In:</span>
                <span>{hoveredLink.inRate}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Out:</span>
                <span>{hoveredLink.outRate}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Side A:</span>
                <span>{hoveredLink.deviceA}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Side Z:</span>
                <span>{hoveredLink.deviceZ}</span>
              </div>
            </div>
          </>
        )}
        {hoveredDevice && !hoveredLink && (
          <>
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Device</div>
            <div className="text-sm font-medium mb-2">{hoveredDevice.code}</div>
            <div className="space-y-1 text-xs">
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Type:</span>
                <span>{hoveredDevice.deviceType}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Metro:</span>
                <span>{hoveredDevice.metro}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Users:</span>
                <span>{hoveredDevice.userCount}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Validators:</span>
                <span>{hoveredDevice.validatorCount}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Stake:</span>
                <span>{hoveredDevice.stakeSol} SOL</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Stake Share:</span>
                <span>{hoveredDevice.stakeShare}</span>
              </div>
            </div>
          </>
        )}
        {hoveredMetro && !hoveredLink && !hoveredDevice && !hoveredValidator && (
          <>
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Metro</div>
            <div className="text-sm font-medium mb-2">{hoveredMetro.name}</div>
            <div className="space-y-1 text-xs">
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Code:</span>
                <span>{hoveredMetro.code}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Devices:</span>
                <span>{hoveredMetro.deviceCount}</span>
              </div>
            </div>
          </>
        )}
        {hoveredValidator && !hoveredLink && !hoveredDevice && (
          <>
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Validator</div>
            <div className="text-sm font-medium font-mono mb-2">{hoveredValidator.votePubkey.slice(0, 12)}...</div>
            <div className="space-y-1 text-xs">
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Location:</span>
                <span>{hoveredValidator.city}, {hoveredValidator.country}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Stake:</span>
                <span>{hoveredValidator.stakeSol} SOL</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">DZ Stake Share:</span>
                <span>{hoveredValidator.stakeShare}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-muted-foreground">Device:</span>
                <span>{hoveredValidator.deviceCode}</span>
              </div>
            </div>
          </>
        )}
      </div>
    )}
    </>
  )
}
