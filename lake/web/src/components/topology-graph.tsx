import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import cytoscape from 'cytoscape'
import type { Core, NodeSingular, EdgeSingular } from 'cytoscape'
import { useQuery } from '@tanstack/react-query'
import { ZoomIn, ZoomOut, Maximize, Search, Filter, Route, X, GitCompare, AlertTriangle, Zap, Lightbulb, ChevronDown, ChevronUp, Shield, MinusCircle, PlusCircle, Coins, Activity, MapPin, BarChart3 } from 'lucide-react'
import { fetchISISTopology, fetchISISPaths, fetchTopologyCompare, fetchFailureImpact, fetchCriticalLinks, fetchSimulateLinkRemoval, fetchSimulateLinkAddition, fetchTopology, fetchLinkHealth } from '@/lib/api'
import type { PathMode, FailureImpactResponse, MultiPathResponse, SimulateLinkRemovalResponse, SimulateLinkAdditionResponse } from '@/lib/api'
import { useTheme } from '@/hooks/use-theme'

// Device type colors (types from serviceability smart contract: hybrid, transit, edge)
const DEVICE_TYPE_COLORS: Record<string, { light: string; dark: string }> = {
  hybrid: { light: '#7c3aed', dark: '#a78bfa' },    // purple
  transit: { light: '#2563eb', dark: '#60a5fa' },   // blue
  edge: { light: '#0891b2', dark: '#22d3ee' },      // cyan
  default: { light: '#6b7280', dark: '#9ca3af' },   // gray
}

// Path colors for K-shortest paths visualization
const PATH_COLORS = [
  { light: '#16a34a', dark: '#22c55e' },  // green - primary/shortest
  { light: '#2563eb', dark: '#3b82f6' },  // blue - alternate 1
  { light: '#9333ea', dark: '#a855f7' },  // purple - alternate 2
  { light: '#ea580c', dark: '#f97316' },  // orange - alternate 3
  { light: '#0891b2', dark: '#06b6d4' },  // cyan - alternate 4
]

// Metro colors for metro clustering visualization (10 distinct colors)
const METRO_COLORS = [
  { light: '#2563eb', dark: '#3b82f6' },  // blue
  { light: '#7c3aed', dark: '#a78bfa' },  // purple
  { light: '#db2777', dark: '#f472b6' },  // pink
  { light: '#ea580c', dark: '#f97316' },  // orange
  { light: '#16a34a', dark: '#22c55e' },  // green
  { light: '#0891b2', dark: '#22d3ee' },  // cyan
  { light: '#4f46e5', dark: '#818cf8' },  // indigo
  { light: '#ca8a04', dark: '#facc15' },  // yellow
  { light: '#0d9488', dark: '#2dd4bf' },  // teal
  { light: '#be185d', dark: '#f472b6' },  // rose
]

// Format bits per second to human readable
function formatBps(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)}Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)}Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)}Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(0)}Kbps`
  return `${bps.toFixed(0)}bps`
}

type InteractionMode = 'explore' | 'path' | 'compare' | 'criticality' | 'whatif-removal' | 'whatif-addition'

interface TopologyGraphProps {
  onDeviceSelect?: (devicePK: string | null) => void
  selectedDevicePK?: string | null
  statusFilter?: string
  deviceTypeFilter?: string
}

export function TopologyGraph({
  onDeviceSelect,
  selectedDevicePK,
  statusFilter,
  deviceTypeFilter,
}: TopologyGraphProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const cyRef = useRef<Core | null>(null)
  const [cyGeneration, setCyGeneration] = useState(0)
  const navigate = useNavigate()
  const { theme } = useTheme()
  const isDark = theme === 'dark'

  // Use refs for callbacks to avoid re-initializing the graph
  const onDeviceSelectRef = useRef(onDeviceSelect)
  const navigateRef = useRef(navigate)
  const edgeSlaStatusRef = useRef<Map<string, { status: string; avgRttUs: number; committedRttNs: number; lossPct: number; slaRatio: number }>>(new Map())
  useEffect(() => {
    onDeviceSelectRef.current = onDeviceSelect
  }, [onDeviceSelect])
  useEffect(() => {
    navigateRef.current = navigate
  }, [navigate])

  const [mode, setMode] = useState<InteractionMode>('explore')
  const [pathSource, setPathSource] = useState<string | null>(null)
  const [pathTarget, setPathTarget] = useState<string | null>(null)
  const [pathsResult, setPathsResult] = useState<MultiPathResponse | null>(null)
  const [selectedPathIndex, setSelectedPathIndex] = useState<number>(0)
  const [pathLoading, setPathLoading] = useState(false)
  const [pathMode, setPathMode] = useState<PathMode>('hops')

  // Failure impact state
  const [impactDevice, setImpactDevice] = useState<string | null>(null)
  const [impactResult, setImpactResult] = useState<FailureImpactResponse | null>(null)
  const [impactLoading, setImpactLoading] = useState(false)

  // What-If Link Removal state
  const [removalLink, setRemovalLink] = useState<{ sourcePK: string; targetPK: string } | null>(null)
  const [removalResult, setRemovalResult] = useState<SimulateLinkRemovalResponse | null>(null)
  const [removalLoading, setRemovalLoading] = useState(false)

  // What-If Link Addition state
  const [additionSource, setAdditionSource] = useState<string | null>(null)
  const [additionTarget, setAdditionTarget] = useState<string | null>(null)
  const [additionMetric, setAdditionMetric] = useState<number>(1000)
  const [additionResult, setAdditionResult] = useState<SimulateLinkAdditionResponse | null>(null)
  const [additionLoading, setAdditionLoading] = useState(false)

  const [hoveredNode, setHoveredNode] = useState<{
    id: string
    label: string
    status: string
    deviceType: string
    systemId?: string
    degree: number
    x: number
    y: number
  } | null>(null)

  const [hoveredEdge, setHoveredEdge] = useState<{
    id: string
    source: string
    target: string
    metric: number
    x: number
    y: number
    health?: {
      status: string
      avgRttUs: number
      committedRttNs: number
      lossPct: number
      slaRatio: number
    }
    isInterMetroEdge?: boolean
    linkCount?: number
    avgMetric?: number | null
  } | null>(null)

  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<{ id: string; label: string }[]>([])
  const [showSearch, setShowSearch] = useState(false)
  const [showFilters, setShowFilters] = useState(false)
  const [showGuide, setShowGuide] = useState(true)
  const [localStatusFilter, setLocalStatusFilter] = useState(statusFilter || 'all')
  const [localTypeFilter, setLocalTypeFilter] = useState(deviceTypeFilter || 'all')

  const { data, isLoading, error } = useQuery({
    queryKey: ['isis-topology'],
    queryFn: fetchISISTopology,
    refetchInterval: 60000,
  })

  // Fetch topology comparison when in compare mode
  const { data: compareData, isLoading: compareLoading } = useQuery({
    queryKey: ['topology-compare'],
    queryFn: fetchTopologyCompare,
    enabled: mode === 'compare',
    refetchInterval: 60000,
  })

  // Fetch critical links when in criticality mode
  const { data: criticalLinksData, isLoading: criticalLinksLoading } = useQuery({
    queryKey: ['critical-links'],
    queryFn: fetchCriticalLinks,
    enabled: mode === 'criticality',
    staleTime: 60000,
  })

  // Stake overlay state
  const [stakeOverlayEnabled, setStakeOverlayEnabled] = useState(false)

  // Link health overlay state
  const [linkHealthOverlayEnabled, setLinkHealthOverlayEnabled] = useState(false)

  // Traffic flow overlay state
  const [trafficFlowEnabled, setTrafficFlowEnabled] = useState(false)

  // Metro clustering overlay state
  const [metroClusteringEnabled, setMetroClusteringEnabled] = useState(false)
  const [collapsedMetros, setCollapsedMetros] = useState<Set<string>>(new Set())

  // Fetch ClickHouse topology for stake/metro/traffic data when overlay is enabled
  const { data: topologyData } = useQuery({
    queryKey: ['topology'],
    queryFn: fetchTopology,
    enabled: stakeOverlayEnabled || metroClusteringEnabled || trafficFlowEnabled,
    staleTime: 30000, // Refresh every 30 seconds for traffic data
    refetchInterval: trafficFlowEnabled ? 30000 : undefined,
  })

  // Fetch link health data when link health overlay is enabled
  const { data: linkHealthData } = useQuery({
    queryKey: ['link-health'],
    queryFn: fetchLinkHealth,
    enabled: linkHealthOverlayEnabled,
    staleTime: 30000, // Refresh every 30 seconds
  })

  // Build device stake map from topology data (maps device PK to stake info)
  const deviceStakeMap = useMemo(() => {
    const map = new Map<string, { stakeSol: number; stakeShare: number; validatorCount: number }>()
    if (!topologyData?.devices) return map
    for (const device of topologyData.devices) {
      map.set(device.pk, {
        stakeSol: device.stake_sol ?? 0,
        stakeShare: device.stake_share ?? 0,
        validatorCount: device.validator_count ?? 0,
      })
    }
    return map
  }, [topologyData])

  // Build metro info map from topology data (maps metro PK to code/name)
  const metroInfoMap = useMemo(() => {
    const map = new Map<string, { code: string; name: string; colorIndex: number }>()
    if (!topologyData?.metros) return map
    topologyData.metros.forEach((metro, index) => {
      map.set(metro.pk, {
        code: metro.code,
        name: metro.name,
        colorIndex: index % METRO_COLORS.length,
      })
    })
    return map
  }, [topologyData])

  // Get metro color by PK
  const getMetroColor = useCallback((metroPK: string | undefined) => {
    if (!metroPK) return isDark ? '#6b7280' : '#9ca3af' // gray for unknown
    const metroInfo = metroInfoMap.get(metroPK)
    if (!metroInfo) return isDark ? '#6b7280' : '#9ca3af'
    const colors = METRO_COLORS[metroInfo.colorIndex]
    return isDark ? colors.dark : colors.light
  }, [metroInfoMap, isDark])

  // Build edge criticality map from critical links data
  const edgeCriticality = useMemo(() => {
    if (!criticalLinksData?.links) return new Map<string, string>()
    const criticality = new Map<string, string>()

    for (const link of criticalLinksData.links) {
      // Create edge keys for both directions
      const key1 = `${link.sourcePK}->${link.targetPK}`
      const key2 = `${link.targetPK}->${link.sourcePK}`
      criticality.set(key1, link.criticality)
      criticality.set(key2, link.criticality)
    }
    return criticality
  }, [criticalLinksData])

  // Build edge SLA status map from link health data (maps device pair to SLA status)
  const edgeSlaStatus = useMemo(() => {
    if (!linkHealthData?.links) return new Map<string, { status: string; avgRttUs: number; committedRttNs: number; lossPct: number; slaRatio: number }>()
    const slaMap = new Map<string, { status: string; avgRttUs: number; committedRttNs: number; lossPct: number; slaRatio: number }>()

    for (const link of linkHealthData.links) {
      // Create edge keys for both directions
      const key1 = `${link.side_a_pk}->${link.side_z_pk}`
      const key2 = `${link.side_z_pk}->${link.side_a_pk}`
      const info = {
        status: link.sla_status,
        avgRttUs: link.avg_rtt_us,
        committedRttNs: link.committed_rtt_ns,
        lossPct: link.loss_pct,
        slaRatio: link.sla_ratio,
      }
      slaMap.set(key1, info)
      slaMap.set(key2, info)
    }
    return slaMap
  }, [linkHealthData])

  // Keep edgeSlaStatus ref updated for hover handlers
  useEffect(() => {
    edgeSlaStatusRef.current = edgeSlaStatus
  }, [edgeSlaStatus])

  // Build edge traffic map from topology data (maps device pair to traffic info)
  const edgeTrafficMap = useMemo(() => {
    if (!topologyData?.links) return new Map<string, { inBps: number; outBps: number; bandwidthBps: number; utilization: number }>()
    const trafficMap = new Map<string, { inBps: number; outBps: number; bandwidthBps: number; utilization: number }>()

    for (const link of topologyData.links) {
      // Create edge keys for both directions
      const key1 = `${link.side_a_pk}->${link.side_z_pk}`
      const key2 = `${link.side_z_pk}->${link.side_a_pk}`
      const totalBps = (link.in_bps ?? 0) + (link.out_bps ?? 0)
      const utilization = link.bandwidth_bps > 0 ? (totalBps / link.bandwidth_bps) * 100 : 0
      const info = {
        inBps: link.in_bps ?? 0,
        outBps: link.out_bps ?? 0,
        bandwidthBps: link.bandwidth_bps ?? 0,
        utilization,
      }
      trafficMap.set(key1, info)
      trafficMap.set(key2, info)
    }
    return trafficMap
  }, [topologyData])

  // Get traffic utilization level for coloring
  const getTrafficLevel = useCallback((utilization: number): 'low' | 'medium' | 'high' | 'critical' => {
    if (utilization >= 80) return 'critical'
    if (utilization >= 50) return 'high'
    if (utilization >= 20) return 'medium'
    return 'low'
  }, [])

  // Calculate degree for each node
  const nodesDegree = useMemo(() => {
    if (!data) return new Map<string, number>()
    const degrees = new Map<string, number>()
    data.nodes.forEach(node => degrees.set(node.data.id, 0))
    data.edges.forEach(edge => {
      degrees.set(edge.data.source, (degrees.get(edge.data.source) || 0) + 1)
      degrees.set(edge.data.target, (degrees.get(edge.data.target) || 0) + 1)
    })
    return degrees
  }, [data])

  // Build edge health status from compare data
  // Returns: 'matched' | 'missing' | 'extra' | 'mismatch' | undefined
  const edgeHealthStatus = useMemo(() => {
    if (!compareData?.discrepancies) return new Map<string, string>()
    const status = new Map<string, string>()

    for (const d of compareData.discrepancies) {
      // Create edge keys for both directions
      const key1 = `${d.deviceAPK}->${d.deviceBPK}`
      const key2 = `${d.deviceBPK}->${d.deviceAPK}`

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

  // Get unique device types for filter
  const deviceTypes = useMemo(() => {
    if (!data) return []
    const types = new Set(data.nodes.map(n => n.data.deviceType).filter(Boolean))
    return Array.from(types).sort()
  }, [data])

  // Filter nodes and edges
  const filteredData = useMemo(() => {
    if (!data) return null

    const activeStatusFilter = statusFilter || localStatusFilter
    const activeTypeFilter = deviceTypeFilter || localTypeFilter

    const filteredNodes = data.nodes.filter(node => {
      if (activeStatusFilter !== 'all') {
        const isActive = node.data.status === 'active' || node.data.status === 'activated'
        if (activeStatusFilter === 'active' && !isActive) return false
        if (activeStatusFilter === 'inactive' && isActive) return false
      }
      if (activeTypeFilter !== 'all' && node.data.deviceType !== activeTypeFilter) {
        return false
      }
      return true
    })

    const nodeIds = new Set(filteredNodes.map(n => n.data.id))
    const filteredEdges = data.edges.filter(
      edge => nodeIds.has(edge.data.source) && nodeIds.has(edge.data.target)
    )

    return { nodes: filteredNodes, edges: filteredEdges }
  }, [data, statusFilter, localStatusFilter, deviceTypeFilter, localTypeFilter])

  // Get device type color
  const getDeviceTypeColor = useCallback((deviceType: string) => {
    const colors = DEVICE_TYPE_COLORS[deviceType?.toLowerCase()] || DEVICE_TYPE_COLORS.default
    return isDark ? colors.dark : colors.light
  }, [isDark])

  // Get status border color
  const getStatusBorderColor = useCallback((status: string) => {
    if (status === 'active' || status === 'activated') {
      return isDark ? '#22c55e' : '#16a34a' // green
    }
    return isDark ? '#ef4444' : '#dc2626' // red
  }, [isDark])

  // Calculate node size based on degree
  const getNodeSize = useCallback((degree: number) => {
    const minSize = 16
    const maxSize = 40
    if (degree <= 1) return minSize
    const size = minSize + Math.log2(degree) * 6
    return Math.min(maxSize, size)
  }, [])

  // Calculate node size based on stake (for stake overlay mode)
  const getStakeNodeSize = useCallback((stakeSol: number) => {
    if (stakeSol <= 0) return 16
    const minSize = 16
    const maxSize = 48
    const minStake = 10000 // 10k SOL
    const size = minSize + Math.log10(Math.max(minStake, stakeSol) / minStake) * 8
    return Math.min(maxSize, size)
  }, [])

  // Get stake-based color
  const getStakeColor = useCallback((stakeShare: number) => {
    if (stakeShare <= 0) return isDark ? '#6b7280' : '#9ca3af' // gray for no stake
    // Orange gradient from light (low stake) to bright (high stake)
    const t = Math.min(stakeShare / 1.0, 1) // cap at 1%
    if (isDark) {
      // Dark mode: brighter oranges
      const r = Math.round(251)
      const g = Math.round(191 - t * 100)
      const b = Math.round(36)
      return `rgb(${r}, ${g}, ${b})`
    } else {
      // Light mode: deeper oranges
      const r = Math.round(234)
      const g = Math.round(179 - t * 90)
      const b = Math.round(8)
      return `rgb(${r}, ${g}, ${b})`
    }
  }, [isDark])

  // Fetch paths when source and target are set
  useEffect(() => {
    if (mode !== 'path' || !pathSource || !pathTarget) return

    setPathLoading(true)
    setSelectedPathIndex(0) // Reset to first path
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
  }, [mode, pathSource, pathTarget, pathMode])

  // Highlight paths on graph - show all paths with different colors, selected path is prominent
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    // Clear previous path highlighting
    cy.elements().removeClass('path-node path-edge path-0 path-1 path-2 path-3 path-4 path-selected')
    cy.elements().removeData('pathIndex')

    if (!pathsResult?.paths?.length) return

    // Highlight all paths with their respective colors
    pathsResult.paths.forEach((singlePath, pathIndex) => {
      if (!singlePath.path.length) return

      const pathClass = `path-${pathIndex}`
      const isSelected = pathIndex === selectedPathIndex

      // Highlight path nodes
      singlePath.path.forEach(hop => {
        const node = cy.getElementById(hop.devicePK)
        if (node.length) {
          node.addClass('path-node')
          node.addClass(pathClass)
          if (isSelected) node.addClass('path-selected')
        }
      })

      // Highlight edges between consecutive path nodes
      for (let i = 0; i < singlePath.path.length - 1; i++) {
        const from = singlePath.path[i].devicePK
        const to = singlePath.path[i + 1].devicePK
        // Try both directions since ISIS adjacencies are directed
        const edge = cy.edges(`[source="${from}"][target="${to}"], [source="${to}"][target="${from}"]`)
        edge.addClass('path-edge')
        edge.addClass(pathClass)
        if (isSelected) edge.addClass('path-selected')
      }
    })
  }, [pathsResult, selectedPathIndex, isDark])

  // Clear classes when mode changes
  useEffect(() => {
    const allClasses = 'path-node path-edge path-source path-target path-0 path-1 path-2 path-3 path-4 path-selected health-matched health-extra health-missing health-mismatch criticality-critical criticality-important criticality-redundant whatif-removed whatif-rerouted whatif-disconnected whatif-added whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained'

    if (mode === 'explore') {
      setPathSource(null)
      setPathTarget(null)
      setPathsResult(null)
      setSelectedPathIndex(0)
      setRemovalLink(null)
      setRemovalResult(null)
      setAdditionSource(null)
      setAdditionTarget(null)
      setAdditionResult(null)
      if (cyRef.current) {
        cyRef.current.elements().removeClass(allClasses)
      }
    } else if (mode === 'path') {
      // Clear other mode classes
      setRemovalLink(null)
      setRemovalResult(null)
      setAdditionSource(null)
      setAdditionTarget(null)
      setAdditionResult(null)
      if (cyRef.current) {
        cyRef.current.elements().removeClass('health-matched health-extra health-missing health-mismatch criticality-critical criticality-important criticality-redundant whatif-removed whatif-rerouted whatif-disconnected whatif-added whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
      }
    } else if (mode === 'compare') {
      setRemovalLink(null)
      setRemovalResult(null)
      setAdditionSource(null)
      setAdditionTarget(null)
      setAdditionResult(null)
      if (cyRef.current) {
        cyRef.current.elements().removeClass('path-node path-edge path-source path-target criticality-critical criticality-important criticality-redundant whatif-removed whatif-rerouted whatif-disconnected whatif-added whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
      }
    } else if (mode === 'criticality') {
      setRemovalLink(null)
      setRemovalResult(null)
      setAdditionSource(null)
      setAdditionTarget(null)
      setAdditionResult(null)
      if (cyRef.current) {
        cyRef.current.elements().removeClass('path-node path-edge path-source path-target health-matched health-extra health-missing health-mismatch whatif-removed whatif-rerouted whatif-disconnected whatif-added whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
      }
    } else if (mode === 'whatif-removal') {
      // Clear other mode classes
      setPathSource(null)
      setPathTarget(null)
      setPathsResult(null)
      setAdditionSource(null)
      setAdditionTarget(null)
      setAdditionResult(null)
      if (cyRef.current) {
        cyRef.current.elements().removeClass('path-node path-edge path-source path-target path-0 path-1 path-2 path-3 path-4 path-selected health-matched health-extra health-missing health-mismatch criticality-critical criticality-important criticality-redundant whatif-added whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
      }
    } else if (mode === 'whatif-addition') {
      // Clear other mode classes
      setPathSource(null)
      setPathTarget(null)
      setPathsResult(null)
      setRemovalLink(null)
      setRemovalResult(null)
      if (cyRef.current) {
        cyRef.current.elements().removeClass('path-node path-edge path-source path-target path-0 path-1 path-2 path-3 path-4 path-selected health-matched health-extra health-missing health-mismatch criticality-critical criticality-important criticality-redundant whatif-removed whatif-rerouted whatif-disconnected')
      }
    }
  }, [mode])

  // Apply health status classes in compare mode
  useEffect(() => {
    if (!cyRef.current || mode !== 'compare') return
    const cy = cyRef.current

    // Clear previous health classes
    cy.edges().removeClass('health-matched health-extra health-missing health-mismatch')

    if (!compareData) return

    // Apply classes based on edge health status
    cy.edges().forEach(edge => {
      const edgeId = edge.data('id') // format: source->target
      const status = edgeHealthStatus.get(edgeId)

      if (status === 'missing') {
        edge.addClass('health-missing')
      } else if (status === 'extra') {
        edge.addClass('health-extra')
      } else if (status === 'mismatch') {
        edge.addClass('health-mismatch')
      } else {
        // Default to matched if no discrepancy found
        edge.addClass('health-matched')
      }
    })
  }, [mode, compareData, edgeHealthStatus])

  // Apply criticality classes in criticality mode
  useEffect(() => {
    if (!cyRef.current || mode !== 'criticality') return
    const cy = cyRef.current

    // Clear previous criticality classes
    cy.edges().removeClass('criticality-critical criticality-important criticality-redundant')

    if (!criticalLinksData) return

    // Apply classes based on edge criticality
    cy.edges().forEach(edge => {
      const edgeId = edge.data('id') // format: source->target
      const crit = edgeCriticality.get(edgeId)

      if (crit === 'critical') {
        edge.addClass('criticality-critical')
      } else if (crit === 'important') {
        edge.addClass('criticality-important')
      } else {
        edge.addClass('criticality-redundant')
      }
    })
  }, [mode, criticalLinksData, edgeCriticality])

  // Apply stake overlay styling when enabled
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    cy.batch(() => {
      if (stakeOverlayEnabled && deviceStakeMap.size > 0) {
        // Apply stake-based sizing and coloring
        cy.nodes().forEach(node => {
          const devicePK = node.data('id')
          const stakeInfo = deviceStakeMap.get(devicePK)
          if (stakeInfo) {
            const size = getStakeNodeSize(stakeInfo.stakeSol)
            node.style({
              'width': size,
              'height': size,
              'background-color': getStakeColor(stakeInfo.stakeShare),
            })
          } else {
            // No stake data - use gray and small size
            node.style({
              'width': 16,
              'height': 16,
              'background-color': isDark ? '#6b7280' : '#9ca3af',
            })
          }
        })
      } else {
        // Revert to default degree-based sizing
        cy.nodes().forEach(node => {
          const degree = node.data('degree')
          const deviceType = node.data('deviceType')
          node.style({
            'width': getNodeSize(degree),
            'height': getNodeSize(degree),
            'background-color': getDeviceTypeColor(deviceType),
          })
        })
      }
    })
  }, [stakeOverlayEnabled, deviceStakeMap, getStakeNodeSize, getStakeColor, getNodeSize, getDeviceTypeColor, isDark])

  // Apply link health overlay styling when enabled
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    // Clear previous link health classes
    cy.edges().removeClass('sla-healthy sla-warning sla-critical sla-unknown')

    if (linkHealthOverlayEnabled && edgeSlaStatus.size > 0) {
      cy.edges().forEach(edge => {
        const edgeId = edge.data('id') // format: source->target
        const slaInfo = edgeSlaStatus.get(edgeId)

        if (slaInfo) {
          if (slaInfo.status === 'healthy') {
            edge.addClass('sla-healthy')
          } else if (slaInfo.status === 'warning') {
            edge.addClass('sla-warning')
          } else if (slaInfo.status === 'critical') {
            edge.addClass('sla-critical')
          } else {
            edge.addClass('sla-unknown')
          }
        } else {
          edge.addClass('sla-unknown')
        }
      })
    }
  }, [linkHealthOverlayEnabled, edgeSlaStatus])

  // Apply traffic flow overlay styling when enabled
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    // Clear previous traffic classes
    cy.edges().removeClass('traffic-low traffic-medium traffic-high traffic-critical traffic-idle')

    if (trafficFlowEnabled && edgeTrafficMap.size > 0) {
      cy.edges().forEach(edge => {
        const edgeId = edge.data('id') // format: source->target
        const trafficInfo = edgeTrafficMap.get(edgeId)

        if (trafficInfo) {
          const level = getTrafficLevel(trafficInfo.utilization)
          edge.addClass(`traffic-${level}`)
        } else {
          edge.addClass('traffic-idle')
        }
      })
    }
  }, [trafficFlowEnabled, edgeTrafficMap, getTrafficLevel])

  // Apply metro clustering overlay styling when enabled (only if stake overlay is not active)
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    // Skip if stake overlay is active (it takes precedence)
    if (stakeOverlayEnabled) return

    cy.batch(() => {
      if (metroClusteringEnabled && metroInfoMap.size > 0) {
        // Apply metro-based coloring (keep degree-based sizing)
        cy.nodes().forEach(node => {
          const metroPK = node.data('metroPK')
          const degree = node.data('degree')
          node.style({
            'width': getNodeSize(degree),
            'height': getNodeSize(degree),
            'background-color': getMetroColor(metroPK),
          })
        })
      } else {
        // Revert to default device type coloring
        cy.nodes().forEach(node => {
          const degree = node.data('degree')
          const deviceType = node.data('deviceType')
          node.style({
            'width': getNodeSize(degree),
            'height': getNodeSize(degree),
            'background-color': getDeviceTypeColor(deviceType),
          })
        })
      }
    })
  }, [metroClusteringEnabled, metroInfoMap, getMetroColor, getNodeSize, getDeviceTypeColor, stakeOverlayEnabled])

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

  // Ref for toggleMetroCollapse to use in event handlers without dependency issues
  const toggleMetroCollapseRef = useRef(toggleMetroCollapse)
  useEffect(() => {
    toggleMetroCollapseRef.current = toggleMetroCollapse
  }, [toggleMetroCollapse])

  // Handle metro collapse/expand - hide/show nodes and create super nodes with inter-metro edges
  useEffect(() => {
    if (!cyRef.current || !metroClusteringEnabled) return
    const cy = cyRef.current

    cy.batch(() => {
      // First, remove any existing super nodes and inter-metro edges
      cy.nodes('[?isMetroSuperNode]').remove()
      cy.edges('[?isInterMetroEdge]').remove()

      // Process each metro - create super nodes for collapsed metros
      metroInfoMap.forEach((info, metroPK) => {
        const metroNodes = cy.nodes().filter(n => n.data('metroPK') === metroPK && !n.data('isMetroSuperNode'))

        if (collapsedMetros.has(metroPK)) {
          // Metro is collapsed - hide device nodes and create super node
          metroNodes.forEach(node => {
            node.style('display', 'none')
          })

          // Calculate average position for super node
          let avgX = 0, avgY = 0, count = 0
          metroNodes.forEach(node => {
            const pos = node.position()
            avgX += pos.x
            avgY += pos.y
            count++
          })
          if (count > 0) {
            avgX /= count
            avgY /= count

            // Create super node for this metro
            const superNodeId = `metro-super-${metroPK}`
            cy.add({
              group: 'nodes',
              data: {
                id: superNodeId,
                label: `${info.code} (${count})`,
                metroPK: metroPK,
                isMetroSuperNode: true,
                deviceCount: count,
              },
              position: { x: avgX, y: avgY },
            })

            // Style the super node
            const superNode = cy.getElementById(superNodeId)
            superNode.style({
              'width': Math.min(60, 20 + count * 3),
              'height': Math.min(60, 20 + count * 3),
              'background-color': getMetroColor(metroPK),
              'border-width': 3,
              'border-color': isDark ? '#ffffff' : '#000000',
              'border-opacity': 0.5,
              'shape': 'round-rectangle',
              'font-size': '10px',
              'text-valign': 'center',
              'text-halign': 'center',
            })
          }
        } else {
          // Metro is expanded - show device nodes
          metroNodes.forEach(node => {
            node.style('display', 'element')
          })
        }
      })

      // Track inter-metro edges to aggregate them (including latency data)
      const interMetroEdges = new Map<string, {
        count: number
        sourceId: string
        targetId: string
        totalMetric: number
        metricCount: number
      }>()

      // Process original edges - hide them and track inter-metro connections
      cy.edges().forEach(edge => {
        // Skip edges we created
        if (edge.data('isInterMetroEdge')) return

        const sourceNode = cy.getElementById(edge.data('source'))
        const targetNode = cy.getElementById(edge.data('target'))

        // Skip if nodes don't exist (shouldn't happen)
        if (!sourceNode.length || !targetNode.length) return

        const sourceMetro = sourceNode.data('metroPK')
        const targetMetro = targetNode.data('metroPK')
        const sourceCollapsed = sourceMetro && collapsedMetros.has(sourceMetro)
        const targetCollapsed = targetMetro && collapsedMetros.has(targetMetro)

        // Case 1: Both endpoints in same collapsed metro - hide (intra-metro)
        if (sourceMetro && targetMetro && sourceMetro === targetMetro && sourceCollapsed) {
          edge.style('display', 'none')
          return
        }

        // Case 2: Neither endpoint in collapsed metro - show normally
        if (!sourceCollapsed && !targetCollapsed) {
          edge.style('display', 'element')
          return
        }

        // Case 3: At least one endpoint in collapsed metro - hide original, track for aggregation
        edge.style('display', 'none')

        // Determine the effective source and target (device or super node)
        const effectiveSourceId = sourceCollapsed ? `metro-super-${sourceMetro}` : edge.data('source')
        const effectiveTargetId = targetCollapsed ? `metro-super-${targetMetro}` : edge.data('target')

        // Skip self-loops (both devices in same collapsed metro - already handled above)
        if (effectiveSourceId === effectiveTargetId) return

        // Get metric for averaging
        const metric = edge.data('metric')

        // Create a canonical key (sorted to avoid duplicates for A->B and B->A)
        const edgeKey = [effectiveSourceId, effectiveTargetId].sort().join('|')
        const existing = interMetroEdges.get(edgeKey)
        if (existing) {
          existing.count++
          if (metric) {
            existing.totalMetric += metric
            existing.metricCount++
          }
        } else {
          interMetroEdges.set(edgeKey, {
            count: 1,
            sourceId: effectiveSourceId,
            targetId: effectiveTargetId,
            totalMetric: metric || 0,
            metricCount: metric ? 1 : 0,
          })
        }
      })

      // Create aggregated inter-metro edges
      interMetroEdges.forEach((edgeInfo, edgeKey) => {
        const edgeId = `inter-metro-${edgeKey}`
        const avgMetric = edgeInfo.metricCount > 0 ? edgeInfo.totalMetric / edgeInfo.metricCount : null
        cy.add({
          group: 'edges',
          data: {
            id: edgeId,
            source: edgeInfo.sourceId,
            target: edgeInfo.targetId,
            isInterMetroEdge: true,
            linkCount: edgeInfo.count,
            avgMetric: avgMetric,
          },
        })

        // Style the inter-metro edge
        const edge = cy.getElementById(edgeId)
        edge.style({
          'width': Math.min(8, 1 + edgeInfo.count),
          'line-color': isDark ? '#64748b' : '#94a3b8',
          'target-arrow-color': isDark ? '#64748b' : '#94a3b8',
          'curve-style': 'bezier',
          'label': edgeInfo.count > 1 ? `${edgeInfo.count}` : '',
          'font-size': '8px',
          'text-background-color': isDark ? '#1e293b' : '#f1f5f9',
          'text-background-opacity': 0.8,
          'text-background-padding': '2px',
        })
      })
    })
  }, [metroClusteringEnabled, collapsedMetros, metroInfoMap, getMetroColor, isDark])

  // Clear collapsed metros when metro clustering is disabled
  useEffect(() => {
    if (!metroClusteringEnabled) {
      setCollapsedMetros(new Set())
    }
  }, [metroClusteringEnabled])

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

  // Apply whatif-removal visualization styles
  useEffect(() => {
    if (!cyRef.current || !removalResult) return
    const cy = cyRef.current

    // Highlight disconnected devices
    removalResult.disconnectedDevices.forEach(device => {
      const node = cy.getElementById(device.pk)
      if (node.length) {
        node.addClass('whatif-disconnected')
      }
    })

    // Highlight rerouted paths
    removalResult.affectedPaths.forEach(path => {
      if (path.hasAlternate) {
        const fromNode = cy.getElementById(path.fromPK)
        const toNode = cy.getElementById(path.toPK)
        if (fromNode.length) fromNode.addClass('whatif-rerouted')
        if (toNode.length) toNode.addClass('whatif-rerouted')
      }
    })
  }, [removalResult])

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

  // Apply whatif-addition visualization styles
  useEffect(() => {
    if (!cyRef.current || !additionResult) return
    const cy = cyRef.current

    // Highlight devices that would have improved paths
    additionResult.improvedPaths.forEach(path => {
      const fromNode = cy.getElementById(path.fromPK)
      const toNode = cy.getElementById(path.toPK)
      if (fromNode.length) fromNode.addClass('whatif-improved')
      if (toNode.length) toNode.addClass('whatif-improved')
    })

    // Highlight devices that would gain redundancy
    additionResult.redundancyGains.forEach(gain => {
      const node = cy.getElementById(gain.devicePK)
      if (node.length) {
        node.addClass('whatif-redundancy-gained')
      }
    })
  }, [additionResult])

  // Build styles as a function so we can update them when theme changes
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const buildStyles = useCallback((): any[] => [
        {
          selector: 'node',
          style: {
            'background-color': (ele: NodeSingular) => getDeviceTypeColor(ele.data('deviceType')),
            'label': 'data(label)',
            'text-valign': 'bottom',
            'text-halign': 'center',
            'font-size': '9px',
            'color': isDark ? '#d1d5db' : '#4b5563',
            'text-margin-y': 4,
            'width': (ele: NodeSingular) => getNodeSize(ele.data('degree')),
            'height': (ele: NodeSingular) => getNodeSize(ele.data('degree')),
            'border-width': 3,
            'border-color': (ele: NodeSingular) => getStatusBorderColor(ele.data('status')),
          },
        },
        {
          selector: 'node:selected',
          style: {
            'border-width': 4,
            'border-color': '#3b82f6',
            'overlay-opacity': 0.1,
            'overlay-color': '#3b82f6',
          },
        },
        {
          selector: 'node.highlighted',
          style: {
            'border-width': 4,
            'border-color': '#f59e0b',
            'overlay-opacity': 0.15,
            'overlay-color': '#f59e0b',
          },
        },
        {
          selector: 'node.path-source',
          style: {
            'border-width': 5,
            'border-color': '#22c55e',
            'overlay-opacity': 0.2,
            'overlay-color': '#22c55e',
          },
        },
        {
          selector: 'node.path-target',
          style: {
            'border-width': 5,
            'border-color': '#ef4444',
            'overlay-opacity': 0.2,
            'overlay-color': '#ef4444',
          },
        },
        {
          selector: 'node.path-node',
          style: {
            'border-width': 4,
            'border-color': '#f59e0b',
            'overlay-opacity': 0.15,
            'overlay-color': '#f59e0b',
          },
        },
        {
          selector: 'edge',
          style: {
            'width': (ele: EdgeSingular) => {
              const metric = ele.data('metric') || 100
              return Math.max(1, Math.min(6, 600 / metric))
            },
            'line-color': isDark ? '#4b5563' : '#9ca3af',
            'curve-style': 'bezier',
            'target-arrow-shape': 'triangle',
            'target-arrow-color': isDark ? '#4b5563' : '#9ca3af',
            'arrow-scale': 0.6,
            'opacity': 0.7,
          },
        },
        {
          selector: 'edge:selected',
          style: {
            'line-color': '#3b82f6',
            'target-arrow-color': '#3b82f6',
            'width': 3,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.hover',
          style: {
            'line-color': '#f59e0b',
            'target-arrow-color': '#f59e0b',
            'opacity': 1,
          },
        },
        {
          selector: 'edge.path-edge',
          style: {
            'line-color': '#f59e0b',
            'target-arrow-color': '#f59e0b',
            'width': 4,
            'opacity': 1,
          },
        },
        // Compare mode styles
        {
          selector: 'edge.health-matched',
          style: {
            'line-color': '#22c55e',
            'target-arrow-color': '#22c55e',
            'opacity': 0.8,
          },
        },
        {
          selector: 'edge.health-extra',
          style: {
            'line-color': '#f59e0b',
            'target-arrow-color': '#f59e0b',
            'width': 3,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.health-missing',
          style: {
            'line-color': '#ef4444',
            'target-arrow-color': '#ef4444',
            'line-style': 'dashed',
            'width': 3,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.health-mismatch',
          style: {
            'line-color': '#eab308',
            'target-arrow-color': '#eab308',
            'width': 3,
            'opacity': 1,
          },
        },
        // Criticality mode styles
        {
          selector: 'edge.criticality-critical',
          style: {
            'line-color': '#ef4444',
            'target-arrow-color': '#ef4444',
            'width': 4,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.criticality-important',
          style: {
            'line-color': '#f59e0b',
            'target-arrow-color': '#f59e0b',
            'width': 3,
            'opacity': 0.9,
          },
        },
        {
          selector: 'edge.criticality-redundant',
          style: {
            'line-color': '#22c55e',
            'target-arrow-color': '#22c55e',
            'width': 2,
            'opacity': 0.6,
          },
        },
        // Multi-path styles - each path gets a distinct color
        {
          selector: 'edge.path-0',
          style: {
            'line-color': isDark ? '#22c55e' : '#16a34a',
            'target-arrow-color': isDark ? '#22c55e' : '#16a34a',
            'width': 3,
            'opacity': 0.6,
          },
        },
        {
          selector: 'edge.path-1',
          style: {
            'line-color': isDark ? '#3b82f6' : '#2563eb',
            'target-arrow-color': isDark ? '#3b82f6' : '#2563eb',
            'width': 3,
            'opacity': 0.6,
          },
        },
        {
          selector: 'edge.path-2',
          style: {
            'line-color': isDark ? '#a855f7' : '#9333ea',
            'target-arrow-color': isDark ? '#a855f7' : '#9333ea',
            'width': 3,
            'opacity': 0.6,
          },
        },
        {
          selector: 'edge.path-3',
          style: {
            'line-color': isDark ? '#f97316' : '#ea580c',
            'target-arrow-color': isDark ? '#f97316' : '#ea580c',
            'width': 3,
            'opacity': 0.6,
          },
        },
        {
          selector: 'edge.path-4',
          style: {
            'line-color': isDark ? '#06b6d4' : '#0891b2',
            'target-arrow-color': isDark ? '#06b6d4' : '#0891b2',
            'width': 3,
            'opacity': 0.6,
          },
        },
        // Selected path is more prominent
        {
          selector: 'edge.path-selected',
          style: {
            'width': 5,
            'opacity': 1,
            'z-index': 10,
          },
        },
        {
          selector: 'node.path-selected',
          style: {
            'overlay-opacity': 0.2,
            'z-index': 10,
          },
        },
        // What-If Link Removal styles
        {
          selector: 'edge.whatif-removed',
          style: {
            'line-color': '#ef4444',
            'target-arrow-color': '#ef4444',
            'line-style': 'dashed',
            'width': 4,
            'opacity': 0.6,
          },
        },
        {
          selector: 'edge.whatif-rerouted',
          style: {
            'line-color': '#f97316',
            'target-arrow-color': '#f97316',
            'width': 4,
            'opacity': 1,
          },
        },
        {
          selector: 'node.whatif-disconnected',
          style: {
            'border-width': 5,
            'border-color': '#ef4444',
            'overlay-opacity': 0.2,
            'overlay-color': '#ef4444',
          },
        },
        // What-If Link Addition styles
        {
          selector: 'node.whatif-addition-source',
          style: {
            'border-width': 5,
            'border-color': '#22c55e',
            'overlay-opacity': 0.2,
            'overlay-color': '#22c55e',
          },
        },
        {
          selector: 'node.whatif-addition-target',
          style: {
            'border-width': 5,
            'border-color': '#ef4444',
            'overlay-opacity': 0.2,
            'overlay-color': '#ef4444',
          },
        },
        {
          selector: 'node.whatif-improved',
          style: {
            'border-width': 4,
            'border-color': '#22c55e',
            'overlay-opacity': 0.15,
            'overlay-color': '#22c55e',
          },
        },
        {
          selector: 'node.whatif-redundancy-gained',
          style: {
            'border-width': 4,
            'border-color': '#06b6d4',
            'overlay-opacity': 0.15,
            'overlay-color': '#06b6d4',
          },
        },
        // Link Health (SLA compliance) styles
        {
          selector: 'edge.sla-healthy',
          style: {
            'line-color': '#22c55e',
            'target-arrow-color': '#22c55e',
            'width': 3,
            'opacity': 0.9,
          },
        },
        {
          selector: 'edge.sla-warning',
          style: {
            'line-color': '#eab308',
            'target-arrow-color': '#eab308',
            'width': 3,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.sla-critical',
          style: {
            'line-color': '#ef4444',
            'target-arrow-color': '#ef4444',
            'width': 4,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.sla-unknown',
          style: {
            'line-color': isDark ? '#6b7280' : '#9ca3af',
            'target-arrow-color': isDark ? '#6b7280' : '#9ca3af',
            'width': 2,
            'opacity': 0.5,
          },
        },
        // Traffic Flow (utilization) styles
        {
          selector: 'edge.traffic-low',
          style: {
            'line-color': '#22c55e', // green
            'target-arrow-color': '#22c55e',
            'width': 2,
            'opacity': 0.8,
          },
        },
        {
          selector: 'edge.traffic-medium',
          style: {
            'line-color': '#84cc16', // lime
            'target-arrow-color': '#84cc16',
            'width': 3,
            'opacity': 0.9,
          },
        },
        {
          selector: 'edge.traffic-high',
          style: {
            'line-color': '#eab308', // yellow
            'target-arrow-color': '#eab308',
            'width': 4,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.traffic-critical',
          style: {
            'line-color': '#ef4444', // red
            'target-arrow-color': '#ef4444',
            'width': 5,
            'opacity': 1,
          },
        },
        {
          selector: 'edge.traffic-idle',
          style: {
            'line-color': isDark ? '#6b7280' : '#9ca3af',
            'target-arrow-color': isDark ? '#6b7280' : '#9ca3af',
            'width': 1,
            'opacity': 0.4,
          },
        },
      ], [isDark, getDeviceTypeColor, getStatusBorderColor, getNodeSize])

  // Initialize Cytoscape and sync data
  useEffect(() => {
    if (!containerRef.current || !filteredData) return

    // Initialize cytoscape if needed
    if (!cyRef.current) {
      const cy = cytoscape({
        container: containerRef.current,
        elements: [],
        style: buildStyles(),
        minZoom: 0.1,
        maxZoom: 3,
        wheelSensitivity: 0.3,
      })

      cyRef.current = cy
      setCyGeneration(g => g + 1)

      // Node hover
      cy.on('mouseover', 'node', (event) => {
        const node = event.target
        const pos = node.renderedPosition()
        setHoveredNode({
          id: node.data('id'),
          label: node.data('label'),
          status: node.data('status'),
          deviceType: node.data('deviceType'),
          systemId: node.data('systemId'),
          degree: node.data('degree'),
          x: pos.x,
          y: pos.y,
        })
      })

      cy.on('mouseout', 'node', () => {
        setHoveredNode(null)
      })

      // Edge hover
      cy.on('mouseover', 'edge', (event) => {
        const edge = event.target
        edge.addClass('hover')
        const midpoint = edge.midpoint()
        const pan = cy.pan()
        const zoom = cy.zoom()
        const source = edge.data('source')
        const target = edge.data('target')
        const edgeKey = `${source}->${target}`
        const healthInfo = edgeSlaStatusRef.current.get(edgeKey)
        setHoveredEdge({
          id: edge.data('id'),
          source,
          target,
          metric: edge.data('metric'),
          x: midpoint.x * zoom + pan.x,
          y: midpoint.y * zoom + pan.y,
          health: healthInfo,
          isInterMetroEdge: edge.data('isInterMetroEdge') || false,
          linkCount: edge.data('linkCount'),
          avgMetric: edge.data('avgMetric'),
        })
      })

      cy.on('mouseout', 'edge', (event) => {
        event.target.removeClass('hover')
        setHoveredEdge(null)
      })

      // Background click - deselect
      cy.on('tap', (event) => {
        if (event.target === cy) {
          onDeviceSelectRef.current?.(null)
        }
      })
    }

    const cy = cyRef.current

    // Sync data incrementally - preserve layout for existing nodes
    const currentNodeIds = new Set(cy.nodes().map(n => n.id()))
    const newNodeIds = new Set(filteredData.nodes.map(n => n.data.id))
    const newEdgeIds = new Set(filteredData.edges.map(e => e.data.id))

    // Remove deleted elements
    cy.nodes().forEach(node => {
      if (!newNodeIds.has(node.id())) {
        node.remove()
      }
    })
    cy.edges().forEach(edge => {
      if (!newEdgeIds.has(edge.id())) {
        edge.remove()
      }
    })

    // Track new nodes for layout
    const nodesToLayout: string[] = []

    // Add or update nodes
    filteredData.nodes.forEach(node => {
      const existingNode = cy.getElementById(node.data.id)
      const degree = nodesDegree.get(node.data.id) || 0

      if (existingNode.length) {
        // Update existing node data
        existingNode.data({
          label: node.data.label,
          status: node.data.status,
          deviceType: node.data.deviceType,
          systemId: node.data.systemId,
          routerId: node.data.routerId,
          metroPK: node.data.metroPK,
          degree,
        })
      } else {
        // Add new node
        cy.add({
          group: 'nodes',
          data: {
            id: node.data.id,
            label: node.data.label,
            status: node.data.status,
            deviceType: node.data.deviceType,
            systemId: node.data.systemId,
            routerId: node.data.routerId,
            metroPK: node.data.metroPK,
            degree,
          },
        })
        nodesToLayout.push(node.data.id)
      }
    })

    // Add or update edges
    filteredData.edges.forEach(edge => {
      const existingEdge = cy.getElementById(edge.data.id)

      if (existingEdge.length) {
        // Update existing edge data
        existingEdge.data({
          metric: edge.data.metric,
        })
      } else {
        // Add new edge
        cy.add({
          group: 'edges',
          data: {
            id: edge.data.id,
            source: edge.data.source,
            target: edge.data.target,
            metric: edge.data.metric,
          },
        })
      }
    })

    // Run layout only for new nodes, or full layout if this is initial load
    if (nodesToLayout.length > 0) {
      if (currentNodeIds.size === 0) {
        // Initial load - run full layout
        cy.layout({
          name: 'cose',
          animate: false,
          nodeDimensionsIncludeLabels: true,
          idealEdgeLength: 120,
          nodeRepulsion: 10000,
          gravity: 0.2,
          numIter: 500,
        } as cytoscape.LayoutOptions).run()
      } else {
        // Incremental update - position new nodes near their neighbors
        nodesToLayout.forEach(nodeId => {
          const node = cy.getElementById(nodeId)
          const neighborNodes = node.neighborhood().nodes()

          if (neighborNodes.length > 0) {
            // Position near the centroid of neighbors
            let avgX = 0, avgY = 0
            neighborNodes.forEach(n => {
              const pos = n.position()
              avgX += pos.x
              avgY += pos.y
            })
            avgX /= neighborNodes.length
            avgY /= neighborNodes.length
            // Add some random offset to avoid overlap
            node.position({
              x: avgX + (Math.random() - 0.5) * 100,
              y: avgY + (Math.random() - 0.5) * 100,
            })
          } else {
            // No neighbors - position randomly in viewport
            const extent = cy.extent()
            node.position({
              x: extent.x1 + Math.random() * (extent.x2 - extent.x1),
              y: extent.y1 + Math.random() * (extent.y2 - extent.y1),
            })
          }
        })
      }
    }

    return () => {
      // Only destroy on unmount, not on data changes
    }
  }, [filteredData, nodesDegree, buildStyles])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (cyRef.current) {
        cyRef.current.destroy()
        cyRef.current = null
      }
    }
  }, [])

  // Handle node clicks based on mode
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    const handleNodeTap = (event: cytoscape.EventObject) => {
      const node = event.target
      const devicePK = node.data('id')

      // Handle super node clicks - expand the metro
      if (node.data('isMetroSuperNode')) {
        const metroPK = node.data('metroPK')
        if (metroPK) {
          toggleMetroCollapseRef.current(metroPK)
        }
        return
      }

      if (mode === 'explore') {
        onDeviceSelectRef.current?.(devicePK)
        // If impact panel is open, update to show new device's impact
        if (impactDevice) {
          setImpactDevice(devicePK)
        }
      } else if (mode === 'path') {
        if (!pathSource) {
          setPathSource(devicePK)
          cy.nodes().removeClass('path-source path-target')
          node.addClass('path-source')
        } else if (!pathTarget && devicePK !== pathSource) {
          setPathTarget(devicePK)
          node.addClass('path-target')
        } else {
          // Reset and start new path
          setPathSource(devicePK)
          setPathTarget(null)
          setPathsResult(null)
          setSelectedPathIndex(0)
          cy.elements().removeClass('path-node path-edge path-source path-target path-0 path-1 path-2 path-3 path-4 path-selected')
          node.addClass('path-source')
        }
      } else if (mode === 'whatif-addition') {
        if (!additionSource) {
          setAdditionSource(devicePK)
          cy.nodes().removeClass('whatif-addition-source whatif-addition-target')
          node.addClass('whatif-addition-source')
        } else if (!additionTarget && devicePK !== additionSource) {
          setAdditionTarget(devicePK)
          node.addClass('whatif-addition-target')
        } else {
          // Reset and start new addition
          setAdditionSource(devicePK)
          setAdditionTarget(null)
          setAdditionResult(null)
          cy.elements().removeClass('whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
          node.addClass('whatif-addition-source')
        }
      }
    }

    const handleNodeDblTap = (event: cytoscape.EventObject) => {
      const node = event.target
      // Don't navigate for super nodes
      if (node.data('isMetroSuperNode')) {
        return
      }
      navigateRef.current(`/devices/${node.data('id')}`)
    }

    cy.on('tap', 'node', handleNodeTap)
    cy.on('dbltap', 'node', handleNodeDblTap)

    return () => {
      cy.off('tap', 'node', handleNodeTap)
      cy.off('dbltap', 'node', handleNodeDblTap)
    }
  }, [mode, pathSource, pathTarget, additionSource, additionTarget, impactDevice, cyGeneration])

  // Handle edge clicks for whatif-removal mode
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    const handleEdgeTap = (event: cytoscape.EventObject) => {
      if (mode !== 'whatif-removal') return

      const edge = event.target
      const sourcePK = edge.data('source')
      const targetPK = edge.data('target')

      // Clear previous simulation
      cy.elements().removeClass('whatif-removed whatif-rerouted whatif-disconnected')
      setRemovalResult(null)

      // Set the selected link
      setRemovalLink({ sourcePK, targetPK })
      edge.addClass('whatif-removed')
    }

    cy.on('tap', 'edge', handleEdgeTap)

    return () => {
      cy.off('tap', 'edge', handleEdgeTap)
    }
  }, [mode, cyGeneration])

  // Handle external selection changes
  useEffect(() => {
    if (!cyRef.current || mode !== 'explore') return
    const cy = cyRef.current

    cy.nodes().removeClass('highlighted')

    if (selectedDevicePK) {
      const node = cy.getElementById(selectedDevicePK)
      if (node.length) {
        node.addClass('highlighted')
        cy.animate({
          center: { eles: node },
          zoom: 1.5,
          duration: 300,
        })
      }
    }
  }, [selectedDevicePK, mode, cyGeneration])

  // Search functionality
  useEffect(() => {
    if (!filteredData || !searchQuery.trim()) {
      setSearchResults([])
      return
    }

    const query = searchQuery.toLowerCase()
    const results = filteredData.nodes
      .filter(node =>
        node.data.label.toLowerCase().includes(query) ||
        node.data.id.toLowerCase().includes(query)
      )
      .slice(0, 10)
      .map(node => ({ id: node.data.id, label: node.data.label }))

    setSearchResults(results)
  }, [searchQuery, filteredData])

  const handleSearchSelect = (id: string) => {
    if (!cyRef.current) return
    const cy = cyRef.current
    const node = cy.getElementById(id)
    if (node.length) {
      cy.animate({
        center: { eles: node },
        zoom: 1.5,
        duration: 300,
      })
      node.select()
      if (mode === 'explore') {
        onDeviceSelectRef.current?.(id)
      }
    }
    setSearchQuery('')
    setSearchResults([])
    setShowSearch(false)
  }

  const handleZoomIn = () => cyRef.current?.zoom(cyRef.current.zoom() * 1.3)
  const handleZoomOut = () => cyRef.current?.zoom(cyRef.current.zoom() / 1.3)
  const handleFit = () => cyRef.current?.fit(undefined, 50)

  const clearPath = () => {
    setPathSource(null)
    setPathTarget(null)
    setPathsResult(null)
    setSelectedPathIndex(0)
    if (cyRef.current) {
      cyRef.current.elements().removeClass('path-node path-edge path-source path-target path-0 path-1 path-2 path-3 path-4 path-selected')
    }
  }

  const analyzeImpact = async (devicePK: string) => {
    setImpactDevice(devicePK)
    setImpactLoading(true)
    setImpactResult(null)
    try {
      const result = await fetchFailureImpact(devicePK)
      setImpactResult(result)
    } catch {
      setImpactResult({ devicePK, deviceCode: '', unreachableDevices: [], unreachableCount: 0, error: 'Failed to analyze impact' })
    } finally {
      setImpactLoading(false)
    }
  }

  const clearImpact = () => {
    setImpactDevice(null)
    setImpactResult(null)
  }

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't capture if typing in an input
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) {
        return
      }

      switch (e.key) {
        case 'Escape':
          // Exit current mode or clear selection
          if (mode !== 'explore') {
            setMode('explore')
          } else if (impactDevice) {
            clearImpact()
          } else if (selectedDevicePK) {
            onDeviceSelectRef.current?.(null)
          }
          cyRef.current?.elements().unselect()
          break
        case '?':
          // Toggle guide panel
          setShowGuide(!showGuide)
          break
        case 'p':
          // Toggle path mode
          if (!e.metaKey && !e.ctrlKey) {
            setMode(mode === 'path' ? 'explore' : 'path')
          }
          break
        case 'c':
          // Toggle compare mode
          if (!e.metaKey && !e.ctrlKey) {
            setMode(mode === 'compare' ? 'explore' : 'compare')
          }
          break
        case 'f':
          // Focus search
          if (!e.metaKey && !e.ctrlKey) {
            e.preventDefault()
            setShowSearch(true)
          }
          break
        case 'r':
          // Toggle whatif-removal mode
          if (!e.metaKey && !e.ctrlKey) {
            setMode(mode === 'whatif-removal' ? 'explore' : 'whatif-removal')
          }
          break
        case 'a':
          // Toggle whatif-addition mode
          if (!e.metaKey && !e.ctrlKey) {
            setMode(mode === 'whatif-addition' ? 'explore' : 'whatif-addition')
          }
          break
        case 's':
          // Toggle stake overlay
          if (!e.metaKey && !e.ctrlKey) {
            setStakeOverlayEnabled(prev => !prev)
          }
          break
        case 'h':
          // Toggle link health overlay
          if (!e.metaKey && !e.ctrlKey) {
            setLinkHealthOverlayEnabled(prev => !prev)
          }
          break
        case 't':
          // Toggle traffic flow overlay
          if (!e.metaKey && !e.ctrlKey) {
            setTrafficFlowEnabled(prev => !prev)
          }
          break
        case 'm':
          // Toggle metro clustering overlay
          if (!e.metaKey && !e.ctrlKey) {
            setMetroClusteringEnabled(prev => !prev)
          }
          break
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [mode, showGuide, impactDevice, selectedDevicePK])

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <div className="text-muted-foreground">Loading ISIS topology...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <div className="text-destructive">
          Failed to load ISIS topology: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    )
  }

  return (
    <div className="relative w-full h-full">
      {/* Graph container */}
      <div ref={containerRef} className="w-full h-full bg-background" />

      {/* Right side controls - matches MapControls style */}
      <div className="absolute top-4 right-4 z-[1000] flex flex-col gap-1">
        {/* Search */}
        <div className="relative">
          <button
            onClick={() => { setShowSearch(!showSearch); setShowFilters(false) }}
            className={`p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors ${showSearch ? 'bg-[var(--muted)]' : ''}`}
            title="Search devices"
          >
            <Search className="h-4 w-4" />
          </button>
          {showSearch && (
            <div className="absolute right-0 top-full mt-1 w-64 bg-[var(--card)] border border-[var(--border)] rounded-md shadow-lg z-50">
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search devices..."
                className="w-full px-3 py-2 text-sm border-b border-[var(--border)] bg-transparent focus:outline-none"
                autoFocus
              />
              {searchResults.length > 0 && (
                <div className="max-h-48 overflow-y-auto">
                  {searchResults.map(result => (
                    <button
                      key={result.id}
                      onClick={() => handleSearchSelect(result.id)}
                      className="w-full px-3 py-2 text-left text-sm hover:bg-[var(--muted)]"
                    >
                      {result.label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        <div className="my-1 border-t border-[var(--border)]" />

        {/* Zoom controls */}
        <button onClick={handleZoomIn} className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors" title="Zoom in">
          <ZoomIn className="h-4 w-4" />
        </button>
        <button onClick={handleZoomOut} className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors" title="Zoom out">
          <ZoomOut className="h-4 w-4" />
        </button>
        <button onClick={handleFit} className="p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors" title="Fit to screen">
          <Maximize className="h-4 w-4" />
        </button>

        <div className="my-1 border-t border-[var(--border)]" />

        {/* Filters */}
        <div className="relative">
          <button
            onClick={() => { setShowFilters(!showFilters); setShowSearch(false) }}
            className={`p-2 bg-[var(--card)] border border-[var(--border)] rounded shadow-sm hover:bg-[var(--muted)] transition-colors ${showFilters ? 'bg-[var(--muted)]' : ''}`}
            title="Filter devices"
          >
            <Filter className="h-4 w-4" />
          </button>
          {showFilters && (
            <div className="absolute right-0 top-full mt-1 w-48 bg-[var(--card)] border border-[var(--border)] rounded-md shadow-lg z-50 p-3">
              <div className="text-xs font-medium text-muted-foreground mb-2">Status</div>
              <select
                value={localStatusFilter}
                onChange={(e) => setLocalStatusFilter(e.target.value)}
                className="w-full px-2 py-1 text-sm border border-[var(--border)] rounded bg-[var(--card)] mb-3"
              >
                <option value="all">All</option>
                <option value="active">Active</option>
                <option value="inactive">Inactive</option>
              </select>

              <div className="text-xs font-medium text-muted-foreground mb-2">Device Type</div>
              <select
                value={localTypeFilter}
                onChange={(e) => setLocalTypeFilter(e.target.value)}
                className="w-full px-2 py-1 text-sm border border-[var(--border)] rounded bg-[var(--card)]"
              >
                <option value="all">All</option>
                {deviceTypes.map(type => (
                  <option key={type} value={type}>{type}</option>
                ))}
              </select>
            </div>
          )}
        </div>

        {/* Mode toggles */}
        <button
          onClick={() => setMode(mode === 'path' ? 'explore' : 'path')}
          className={`p-2 border rounded shadow-sm transition-colors ${
            mode === 'path'
              ? 'bg-amber-500/20 border-amber-500/50 text-amber-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={mode === 'path' ? 'Exit path finding mode' : 'Switch to path finding mode'}
        >
          <Route className="h-4 w-4" />
        </button>
        <button
          onClick={() => setMode(mode === 'compare' ? 'explore' : 'compare')}
          className={`p-2 border rounded shadow-sm transition-colors ${
            mode === 'compare'
              ? 'bg-blue-500/20 border-blue-500/50 text-blue-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={mode === 'compare' ? 'Exit compare mode' : 'Compare configured vs discovered topology'}
        >
          <GitCompare className="h-4 w-4" />
        </button>
        <button
          onClick={() => setMode(mode === 'criticality' ? 'explore' : 'criticality')}
          className={`p-2 border rounded shadow-sm transition-colors ${
            mode === 'criticality'
              ? 'bg-red-500/20 border-red-500/50 text-red-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={mode === 'criticality' ? 'Exit criticality mode' : 'Show link criticality (single points of failure)'}
        >
          <Shield className="h-4 w-4" />
        </button>
        <button
          onClick={() => {
            if (impactDevice) {
              clearImpact()
            } else if (selectedDevicePK) {
              analyzeImpact(selectedDevicePK)
            }
          }}
          className={`p-2 border rounded shadow-sm transition-colors ${
            impactDevice
              ? 'bg-purple-500/20 border-purple-500/50 text-purple-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          } ${!selectedDevicePK && !impactDevice ? 'opacity-50 cursor-not-allowed' : ''}`}
          title={impactDevice ? 'Close impact analysis' : selectedDevicePK ? 'Analyze failure impact of selected device' : 'Select a device first'}
          disabled={!selectedDevicePK && !impactDevice}
        >
          <Zap className="h-4 w-4" />
        </button>

        <div className="my-1 border-t border-[var(--border)]" />

        {/* What-If mode toggles */}
        <button
          onClick={() => setMode(mode === 'whatif-removal' ? 'explore' : 'whatif-removal')}
          className={`p-2 border rounded shadow-sm transition-colors ${
            mode === 'whatif-removal'
              ? 'bg-red-500/20 border-red-500/50 text-red-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={mode === 'whatif-removal' ? 'Exit link removal simulation' : 'Simulate removing a link (r)'}
        >
          <MinusCircle className="h-4 w-4" />
        </button>
        <button
          onClick={() => setMode(mode === 'whatif-addition' ? 'explore' : 'whatif-addition')}
          className={`p-2 border rounded shadow-sm transition-colors ${
            mode === 'whatif-addition'
              ? 'bg-green-500/20 border-green-500/50 text-green-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={mode === 'whatif-addition' ? 'Exit link addition simulation' : 'Simulate adding a new link (a)'}
        >
          <PlusCircle className="h-4 w-4" />
        </button>

        <div className="my-1 border-t border-[var(--border)]" />

        {/* Stake overlay toggle */}
        <button
          onClick={() => setStakeOverlayEnabled(prev => !prev)}
          className={`p-2 border rounded shadow-sm transition-colors ${
            stakeOverlayEnabled
              ? 'bg-amber-500/20 border-amber-500/50 text-amber-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={stakeOverlayEnabled ? 'Hide stake overlay (s)' : 'Show stake overlay (s)'}
        >
          <Coins className="h-4 w-4" />
        </button>
        <button
          onClick={() => setLinkHealthOverlayEnabled(prev => !prev)}
          className={`p-2 border rounded shadow-sm transition-colors ${
            linkHealthOverlayEnabled
              ? 'bg-green-500/20 border-green-500/50 text-green-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={linkHealthOverlayEnabled ? 'Hide link health overlay (h)' : 'Show link health overlay (h)'}
        >
          <Activity className="h-4 w-4" />
        </button>
        <button
          onClick={() => setTrafficFlowEnabled(prev => !prev)}
          className={`p-2 border rounded shadow-sm transition-colors ${
            trafficFlowEnabled
              ? 'bg-cyan-500/20 border-cyan-500/50 text-cyan-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={trafficFlowEnabled ? 'Hide traffic flow overlay (t)' : 'Show traffic flow overlay (t)'}
        >
          <BarChart3 className="h-4 w-4" />
        </button>
        <button
          onClick={() => setMetroClusteringEnabled(prev => !prev)}
          className={`p-2 border rounded shadow-sm transition-colors ${
            metroClusteringEnabled
              ? 'bg-blue-500/20 border-blue-500/50 text-blue-500'
              : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
          }`}
          title={metroClusteringEnabled ? 'Hide metro colors (m)' : 'Show metro colors (m)'}
        >
          <MapPin className="h-4 w-4" />
        </button>
      </div>

      {/* Path mode panel - on the right side below controls */}
      {mode === 'path' && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-52">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Route className="h-3.5 w-3.5 text-amber-500" />
              Path Finding
            </span>
            {(pathSource || pathTarget) && (
              <button onClick={clearPath} className="p-1 hover:bg-[var(--muted)] rounded" title="Clear path">
                <X className="h-3 w-3" />
              </button>
            )}
          </div>

          {/* Mode toggle */}
          <div className="flex gap-1 mb-3 p-0.5 bg-[var(--muted)] rounded">
            <button
              onClick={() => setPathMode('hops')}
              className={`flex-1 px-2 py-1 rounded text-xs transition-colors ${
                pathMode === 'hops' ? 'bg-[var(--card)] shadow-sm' : 'hover:bg-[var(--card)]/50'
              }`}
              title="Find path with fewest hops"
            >
              Fewest Hops
            </button>
            <button
              onClick={() => setPathMode('latency')}
              className={`flex-1 px-2 py-1 rounded text-xs transition-colors ${
                pathMode === 'latency' ? 'bg-[var(--card)] shadow-sm' : 'hover:bg-[var(--card)]/50'
              }`}
              title="Find path with lowest latency"
            >
              Lowest Latency
            </button>
          </div>

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
              {/* Path selector - show if multiple paths */}
              {pathsResult.paths.length > 1 && (
                <div className="mb-2">
                  <div className="text-muted-foreground mb-1">
                    {pathsResult.paths.length} paths found
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {pathsResult.paths.map((_, i) => (
                      <button
                        key={i}
                        onClick={() => setSelectedPathIndex(i)}
                        className={`px-2 py-0.5 rounded text-[10px] font-medium transition-colors ${
                          selectedPathIndex === i
                            ? 'bg-primary text-primary-foreground'
                            : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                        }`}
                        style={{
                          borderLeft: `3px solid ${
                            isDark ? PATH_COLORS[i % PATH_COLORS.length].dark : PATH_COLORS[i % PATH_COLORS.length].light
                          }`,
                        }}
                      >
                        Path {i + 1}
                      </button>
                    ))}
                  </div>
                </div>
              )}

              {/* Selected path details */}
              {pathsResult.paths[selectedPathIndex] && (
                <>
                  <div className="space-y-1 text-muted-foreground">
                    <div>Hops: <span className="text-foreground font-medium">{pathsResult.paths[selectedPathIndex].hopCount}</span></div>
                    <div>Latency: <span className="text-foreground font-medium">{(pathsResult.paths[selectedPathIndex].totalMetric / 1000).toFixed(2)}ms</span></div>
                  </div>
                  <div className="mt-2 pt-2 border-t border-[var(--border)] space-y-0.5 max-h-32 overflow-y-auto">
                    {pathsResult.paths[selectedPathIndex].path.map((hop, i) => (
                      <div key={hop.devicePK} className="flex items-center gap-1">
                        <span className="text-muted-foreground w-4">{i + 1}.</span>
                        <span className={i === 0 ? 'text-green-500' : i === pathsResult.paths[selectedPathIndex].path.length - 1 ? 'text-red-500' : 'text-foreground'}>
                          {hop.deviceCode}
                        </span>
                        {hop.edgeMetric !== undefined && hop.edgeMetric > 0 && (
                          <span className="text-muted-foreground text-[10px]">({(hop.edgeMetric / 1000).toFixed(1)}ms)</span>
                        )}
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

      {/* Compare mode panel */}
      {mode === 'compare' && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center gap-1.5 mb-3">
            <GitCompare className="h-3.5 w-3.5 text-blue-500" />
            <span className="font-medium">Topology Health</span>
          </div>

          {compareLoading && (
            <div className="text-muted-foreground">Loading comparison...</div>
          )}

          {compareData && !compareData.error && (
            <div className="space-y-3">
              {/* Summary stats */}
              <div className="space-y-1.5">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Configured Links</span>
                  <span className="font-medium">{compareData.configuredLinks}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">ISIS Adjacencies</span>
                  <span className="font-medium">{compareData.isisAdjacencies}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Matched</span>
                  <span className="font-medium text-green-500">{compareData.matchedLinks}</span>
                </div>
              </div>

              {/* Discrepancy summary */}
              {compareData.discrepancies.length > 0 && (
                <div className="pt-2 border-t border-[var(--border)]">
                  <div className="flex items-center gap-1.5 mb-2">
                    <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
                    <span className="font-medium">{compareData.discrepancies.length} Issues</span>
                  </div>
                  <div className="space-y-1">
                    {compareData.discrepancies.filter(d => d.type === 'missing_isis').length > 0 && (
                      <div className="flex items-center gap-1.5">
                        <div className="w-3 h-0.5 bg-red-500" style={{ borderStyle: 'dashed', borderWidth: '1px', borderColor: '#ef4444' }} />
                        <span className="text-red-500">{compareData.discrepancies.filter(d => d.type === 'missing_isis').length} missing ISIS</span>
                      </div>
                    )}
                    {compareData.discrepancies.filter(d => d.type === 'extra_isis').length > 0 && (
                      <div className="flex items-center gap-1.5">
                        <div className="w-3 h-0.5 bg-amber-500" />
                        <span className="text-amber-500">{compareData.discrepancies.filter(d => d.type === 'extra_isis').length} extra adjacencies</span>
                      </div>
                    )}
                    {compareData.discrepancies.filter(d => d.type === 'metric_mismatch').length > 0 && (
                      <div className="flex items-center gap-1.5">
                        <div className="w-3 h-0.5 bg-yellow-500" />
                        <span className="text-yellow-500">{compareData.discrepancies.filter(d => d.type === 'metric_mismatch').length} metric mismatches</span>
                      </div>
                    )}
                  </div>
                </div>
              )}

              {compareData.discrepancies.length === 0 && (
                <div className="pt-2 border-t border-[var(--border)] text-green-500 flex items-center gap-1.5">
                  <div className="w-2 h-2 rounded-full bg-green-500" />
                  All links healthy
                </div>
              )}

              {/* Edge legend */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Edge Colors</div>
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-green-500" />
                    <span>Matched</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-red-500" style={{ borderTop: '2px dashed #ef4444' }} />
                    <span>Missing ISIS</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-amber-500" />
                    <span>Extra adjacency</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-yellow-500" />
                    <span>Metric mismatch</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {compareData?.error && (
            <div className="text-destructive">{compareData.error}</div>
          )}
        </div>
      )}

      {/* Criticality mode panel */}
      {mode === 'criticality' && (
        <div className="absolute top-[280px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center gap-1.5 mb-3">
            <Shield className="h-3.5 w-3.5 text-red-500" />
            <span className="font-medium">Link Criticality</span>
          </div>

          {criticalLinksLoading && (
            <div className="text-muted-foreground">Analyzing links...</div>
          )}

          {criticalLinksData && !criticalLinksData.error && (
            <div className="space-y-3">
              {/* Summary stats */}
              <div className="space-y-1.5">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Total Links</span>
                  <span className="font-medium">{criticalLinksData.links.length}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-red-500">Critical</span>
                  <span className="font-medium text-red-500">
                    {criticalLinksData.links.filter(l => l.criticality === 'critical').length}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-amber-500">Important</span>
                  <span className="font-medium text-amber-500">
                    {criticalLinksData.links.filter(l => l.criticality === 'important').length}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-green-500">Redundant</span>
                  <span className="font-medium text-green-500">
                    {criticalLinksData.links.filter(l => l.criticality === 'redundant').length}
                  </span>
                </div>
              </div>

              {/* Critical links list */}
              {criticalLinksData.links.filter(l => l.criticality === 'critical').length > 0 && (
                <div className="pt-2 border-t border-[var(--border)]">
                  <div className="flex items-center gap-1.5 mb-2">
                    <AlertTriangle className="h-3.5 w-3.5 text-red-500" />
                    <span className="font-medium text-red-500">Single Points of Failure</span>
                  </div>
                  <div className="space-y-1 max-h-24 overflow-y-auto">
                    {criticalLinksData.links.filter(l => l.criticality === 'critical').slice(0, 5).map((link, i) => (
                      <div key={i} className="text-red-400 truncate">
                        {link.sourceCode} — {link.targetCode}
                      </div>
                    ))}
                    {criticalLinksData.links.filter(l => l.criticality === 'critical').length > 5 && (
                      <div className="text-muted-foreground">
                        +{criticalLinksData.links.filter(l => l.criticality === 'critical').length - 5} more
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* Legend */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Edge Colors</div>
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-1 bg-red-500 rounded" />
                    <span>Critical (no redundancy)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-amber-500 rounded" />
                    <span>Important (limited)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-green-500 rounded opacity-60" />
                    <span>Redundant (safe)</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {criticalLinksData?.error && (
            <div className="text-destructive">{criticalLinksData.error}</div>
          )}
        </div>
      )}

      {/* Impact analysis panel */}
      {(impactDevice || impactLoading) && (
        <div className="absolute top-[320px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Zap className="h-3.5 w-3.5 text-purple-500" />
              Failure Impact
            </span>
            <button onClick={clearImpact} className="p-1 hover:bg-[var(--muted)] rounded" title="Close">
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

      {/* What-If Link Removal panel */}
      {mode === 'whatif-removal' && (
        <div className="absolute top-[340px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
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
                  cyRef.current?.elements().removeClass('whatif-removed whatif-rerouted whatif-disconnected')
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
            <div className="space-y-3">
              <div className="text-muted-foreground">
                Removing link: <span className="font-medium text-foreground">{removalResult.sourceCode}</span> — <span className="font-medium text-foreground">{removalResult.targetCode}</span>
              </div>

              {/* Partition warning */}
              {removalResult.causesPartition && (
                <div className="p-2 bg-red-500/10 border border-red-500/30 rounded text-red-500 flex items-center gap-1.5">
                  <AlertTriangle className="h-3.5 w-3.5" />
                  <span className="font-medium">Would cause network partition!</span>
                </div>
              )}

              {/* Disconnected devices */}
              {removalResult.disconnectedCount > 0 && (
                <div className="space-y-1">
                  <div className="text-red-500 font-medium">
                    {removalResult.disconnectedCount} device{removalResult.disconnectedCount !== 1 ? 's' : ''} would become unreachable
                  </div>
                  <div className="space-y-0.5 max-h-24 overflow-y-auto">
                    {removalResult.disconnectedDevices.slice(0, 5).map(device => (
                      <div key={device.pk} className="flex items-center gap-1.5 text-red-400">
                        <div className="w-2 h-2 rounded-full bg-red-500" />
                        <span>{device.code}</span>
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

              {/* Legend */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Legend</div>
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-red-500" style={{ borderTop: '2px dashed #ef4444' }} />
                    <span>Removed link</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-3 rounded-full border-2 border-red-500" />
                    <span>Disconnected device</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {removalResult?.error && (
            <div className="text-destructive">{removalResult.error}</div>
          )}
        </div>
      )}

      {/* What-If Link Addition panel */}
      {mode === 'whatif-addition' && (
        <div className="absolute top-[340px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
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
                  cyRef.current?.elements().removeClass('whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
                }}
                className="p-1 hover:bg-[var(--muted)] rounded"
                title="Clear"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>

          {/* Metric input */}
          <div className="mb-3">
            <div className="text-muted-foreground mb-1.5">Link Latency</div>
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
            <div className="space-y-3">
              <div className="text-muted-foreground">
                New link: <span className="font-medium text-green-500">{additionResult.sourceCode}</span> — <span className="font-medium text-red-500">{additionResult.targetCode}</span>
              </div>

              {/* Link already exists warning */}
              {additionResult.error === 'Link already exists between these devices' && (
                <div className="p-2 bg-amber-500/10 border border-amber-500/30 rounded text-amber-500">
                  Link already exists
                </div>
              )}

              {/* Redundancy gains */}
              {additionResult.redundancyCount > 0 && (
                <div className="space-y-1">
                  <div className="text-cyan-500 font-medium flex items-center gap-1.5">
                    <Shield className="h-3 w-3" />
                    {additionResult.redundancyCount} device{additionResult.redundancyCount !== 1 ? 's' : ''} would gain redundancy
                  </div>
                  <div className="space-y-0.5 max-h-16 overflow-y-auto">
                    {additionResult.redundancyGains.map(gain => (
                      <div key={gain.devicePK} className="flex items-center gap-1.5 text-cyan-400">
                        <div className="w-2 h-2 rounded-full bg-cyan-500" />
                        <span>{gain.deviceCode}</span>
                        {gain.wasLeaf && <span className="text-[10px] text-muted-foreground">(was leaf)</span>}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Improved paths */}
              {additionResult.improvedPathCount > 0 && (
                <div className="space-y-1 pt-2 border-t border-[var(--border)]">
                  <div className="text-green-500 font-medium">
                    {additionResult.improvedPathCount} path{additionResult.improvedPathCount !== 1 ? 's' : ''} would improve
                  </div>
                  <div className="space-y-1 max-h-32 overflow-y-auto">
                    {additionResult.improvedPaths.slice(0, 5).map((path, i) => (
                      <div key={i} className="text-muted-foreground">
                        <span className="text-foreground">{path.fromCode}</span> → <span className="text-foreground">{path.toCode}</span>
                        <div className="ml-2 text-[10px] text-green-500">
                          {path.beforeHops} → {path.afterHops} hops (-{path.hopReduction})
                        </div>
                      </div>
                    ))}
                    {additionResult.improvedPathCount > 5 && (
                      <div className="text-muted-foreground">+{additionResult.improvedPathCount - 5} more</div>
                    )}
                  </div>
                </div>
              )}

              {additionResult.redundancyCount === 0 && additionResult.improvedPathCount === 0 && (
                <div className="text-muted-foreground flex items-center gap-1.5">
                  <div className="w-2 h-2 rounded-full bg-muted-foreground" />
                  No significant improvements
                </div>
              )}

              {/* Legend */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Legend</div>
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-3 rounded-full border-2 border-green-500" />
                    <span>Source device</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-3 rounded-full border-2 border-red-500" />
                    <span>Target device</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-3 rounded-full border-2 border-cyan-500" />
                    <span>Gains redundancy</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {additionResult?.error && additionResult.error !== 'Link already exists between these devices' && (
            <div className="text-destructive">{additionResult.error}</div>
          )}
        </div>
      )}

      {/* Stake overlay panel */}
      {stakeOverlayEnabled && (
        <div className="absolute top-[340px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Coins className="h-3.5 w-3.5 text-amber-500" />
              Stake Overlay
            </span>
            <button
              onClick={() => setStakeOverlayEnabled(false)}
              className="p-1 hover:bg-[var(--muted)] rounded"
              title="Close"
            >
              <X className="h-3 w-3" />
            </button>
          </div>

          {!topologyData && (
            <div className="text-muted-foreground">Loading stake data...</div>
          )}

          {topologyData && (
            <div className="space-y-3">
              {/* Summary stats */}
              <div className="space-y-1.5">
                {(() => {
                  const devicesWithStake = Array.from(deviceStakeMap.entries()).filter(([, s]) => s.stakeSol > 0)
                  const totalStake = devicesWithStake.reduce((sum, [, s]) => sum + s.stakeSol, 0)
                  const totalValidators = devicesWithStake.reduce((sum, [, s]) => sum + s.validatorCount, 0)
                  return (
                    <>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Devices w/ Stake</span>
                        <span className="font-medium">{devicesWithStake.length}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Total Validators</span>
                        <span className="font-medium">{totalValidators.toLocaleString()}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Total Stake</span>
                        <span className="font-medium">{(totalStake / 1_000_000).toFixed(1)}M SOL</span>
                      </div>
                    </>
                  )
                })()}
              </div>

              {/* Top devices by stake */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Top by Stake</div>
                <div className="space-y-1 max-h-24 overflow-y-auto">
                  {Array.from(deviceStakeMap.entries())
                    .filter(([, s]) => s.stakeSol > 0)
                    .sort((a, b) => b[1].stakeSol - a[1].stakeSol)
                    .slice(0, 5)
                    .map(([pk, stake]) => {
                      const node = cyRef.current?.getElementById(pk)
                      const label = node?.data('label') || pk.substring(0, 8)
                      return (
                        <div key={pk} className="flex items-center justify-between gap-2">
                          <span className="truncate">{label}</span>
                          <span className="text-amber-500 font-medium whitespace-nowrap">
                            {stake.stakeSol >= 1_000_000
                              ? `${(stake.stakeSol / 1_000_000).toFixed(1)}M`
                              : `${(stake.stakeSol / 1_000).toFixed(0)}K`}
                          </span>
                        </div>
                      )
                    })}
                </div>
              </div>

              {/* Legend */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Node Size = Stake Amount</div>
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5">
                    <div className="w-5 h-5 rounded-full" style={{ backgroundColor: getStakeColor(1.0) }} />
                    <span>High stake (&gt;1% share)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-4 rounded-full" style={{ backgroundColor: getStakeColor(0.3) }} />
                    <span>Medium stake</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-3 rounded-full" style={{ backgroundColor: getStakeColor(0) }} />
                    <span>No validators</span>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Link Health overlay panel */}
      {linkHealthOverlayEnabled && (
        <div className="absolute top-[340px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <Activity className="h-3.5 w-3.5 text-green-500" />
              Link Health (SLA)
            </span>
            <button
              onClick={() => setLinkHealthOverlayEnabled(false)}
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

      {/* Traffic Flow overlay panel */}
      {trafficFlowEnabled && (
        <div className="absolute top-[340px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <BarChart3 className="h-3.5 w-3.5 text-cyan-500" />
              Traffic Flow
            </span>
            <button
              onClick={() => setTrafficFlowEnabled(false)}
              className="p-1 hover:bg-[var(--muted)] rounded"
              title="Close"
            >
              <X className="h-3 w-3" />
            </button>
          </div>

          {!topologyData && (
            <div className="text-muted-foreground">Loading traffic data...</div>
          )}

          {topologyData && (
            <div className="space-y-3">
              {/* Summary stats */}
              <div className="space-y-1.5">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Links with traffic</span>
                  <span className="font-medium">{edgeTrafficMap.size / 2}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-red-500">Critical (≥80%)</span>
                  <span className="font-medium text-red-500">
                    {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization >= 80).length}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-yellow-500">High (50-80%)</span>
                  <span className="font-medium text-yellow-500">
                    {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization >= 50 && v.utilization < 80).length}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-lime-500">Medium (20-50%)</span>
                  <span className="font-medium text-lime-500">
                    {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization >= 20 && v.utilization < 50).length}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-green-500">Low (&lt;20%)</span>
                  <span className="font-medium text-green-500">
                    {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization > 0 && v.utilization < 20).length}
                  </span>
                </div>
              </div>

              {/* High utilization links */}
              {(() => {
                const highUtilLinks = topologyData.links
                  .filter(l => {
                    const totalBps = (l.in_bps ?? 0) + (l.out_bps ?? 0)
                    const util = l.bandwidth_bps > 0 ? (totalBps / l.bandwidth_bps) * 100 : 0
                    return util >= 50
                  })
                  .sort((a, b) => {
                    const utilA = a.bandwidth_bps > 0 ? ((a.in_bps ?? 0) + (a.out_bps ?? 0)) / a.bandwidth_bps : 0
                    const utilB = b.bandwidth_bps > 0 ? ((b.in_bps ?? 0) + (b.out_bps ?? 0)) / b.bandwidth_bps : 0
                    return utilB - utilA
                  })
                  .slice(0, 5)

                if (highUtilLinks.length === 0) return null

                return (
                  <div className="pt-2 border-t border-[var(--border)]">
                    <div className="flex items-center gap-1.5 mb-2">
                      <AlertTriangle className="h-3.5 w-3.5 text-yellow-500" />
                      <span className="font-medium text-yellow-500">High Utilization</span>
                    </div>
                    <div className="space-y-1 max-h-24 overflow-y-auto">
                      {highUtilLinks.map(link => {
                        const totalBps = (link.in_bps ?? 0) + (link.out_bps ?? 0)
                        const util = link.bandwidth_bps > 0 ? (totalBps / link.bandwidth_bps) * 100 : 0
                        const color = util >= 80 ? 'text-red-400' : 'text-yellow-400'
                        return (
                          <div key={link.pk} className={`${color} truncate text-[10px]`}>
                            {link.code}
                            <span className="text-muted-foreground ml-1">
                              ({util.toFixed(0)}% - {formatBps(totalBps)})
                            </span>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )
              })()}

              {/* Legend */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Link Colors (by utilization)</div>
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-green-500 rounded" />
                    <span>Low (&lt;20%)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-lime-500 rounded" />
                    <span>Medium (20-50%)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-1 bg-yellow-500 rounded" />
                    <span>High (50-80%)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-1.5 bg-red-500 rounded" />
                    <span>Critical (≥80%)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <div className="w-4 h-0.5 bg-gray-400 rounded opacity-40" />
                    <span>Idle (no traffic)</span>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Metro Clustering overlay panel */}
      {metroClusteringEnabled && (
        <div className="absolute top-[340px] right-4 z-[999] bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-3 text-xs max-w-56">
          <div className="flex items-center justify-between mb-2">
            <span className="font-medium flex items-center gap-1.5">
              <MapPin className="h-3.5 w-3.5 text-blue-500" />
              Metro Colors
            </span>
            <button
              onClick={() => setMetroClusteringEnabled(false)}
              className="p-1 hover:bg-[var(--muted)] rounded"
              title="Close"
            >
              <X className="h-3 w-3" />
            </button>
          </div>

          {!topologyData && (
            <div className="text-muted-foreground">Loading metro data...</div>
          )}

          {topologyData && (
            <div className="space-y-3">
              {/* Summary stats */}
              <div className="space-y-1.5">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Total Metros</span>
                  <span className="font-medium">{metroInfoMap.size}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Devices</span>
                  <span className="font-medium">{filteredData?.nodes.length ?? 0}</span>
                </div>
              </div>

              {/* Metro list with colors - clickable to collapse/expand */}
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="text-muted-foreground mb-1.5">Metros (click to collapse)</div>
                <div className="space-y-0.5 max-h-40 overflow-y-auto">
                  {Array.from(metroInfoMap.entries())
                    .sort((a, b) => a[1].code.localeCompare(b[1].code))
                    .map(([pk, info]) => {
                      const deviceCount = filteredData?.nodes.filter(n => n.data.metroPK === pk).length ?? 0
                      if (deviceCount === 0) return null
                      const isCollapsed = collapsedMetros.has(pk)
                      return (
                        <button
                          key={pk}
                          onClick={() => toggleMetroCollapse(pk)}
                          className={`w-full flex items-center justify-between gap-2 px-1.5 py-1 rounded transition-colors ${
                            isCollapsed
                              ? 'bg-blue-500/20 border border-blue-500/30'
                              : 'hover:bg-[var(--muted)]'
                          }`}
                          title={isCollapsed ? 'Click to expand' : 'Click to collapse'}
                        >
                          <div className="flex items-center gap-1.5">
                            <div
                              className={`w-3 h-3 flex-shrink-0 ${isCollapsed ? 'rounded' : 'rounded-full'}`}
                              style={{ backgroundColor: getMetroColor(pk) }}
                            />
                            <span className="truncate">{info.code}</span>
                          </div>
                          <span className={isCollapsed ? 'text-blue-400 font-medium' : 'text-muted-foreground'}>
                            {isCollapsed ? `(${deviceCount})` : deviceCount}
                          </span>
                        </button>
                      )
                    })}
                </div>
              </div>

              {/* Collapse all / Expand all buttons */}
              {metroInfoMap.size > 0 && (
                <div className="pt-2 border-t border-[var(--border)] flex gap-2">
                  <button
                    onClick={() => setCollapsedMetros(new Set(metroInfoMap.keys()))}
                    className="flex-1 px-2 py-1 bg-[var(--muted)] hover:bg-[var(--muted)]/80 rounded text-[10px]"
                    disabled={collapsedMetros.size === metroInfoMap.size}
                  >
                    Collapse All
                  </button>
                  <button
                    onClick={() => setCollapsedMetros(new Set())}
                    className="flex-1 px-2 py-1 bg-[var(--muted)] hover:bg-[var(--muted)]/80 rounded text-[10px]"
                    disabled={collapsedMetros.size === 0}
                  >
                    Expand All
                  </button>
                </div>
              )}

              {/* Keyboard shortcut hint */}
              <div className="pt-2 border-t border-[var(--border)] text-muted-foreground">
                Press <kbd className="px-1 py-0.5 bg-[var(--muted)] rounded text-[10px]">m</kbd> to toggle
              </div>
            </div>
          )}
        </div>
      )}

      {/* Bottom right panels - Guided questions, Legend, Stats */}
      <div className="absolute bottom-4 right-4 z-[998] flex flex-col gap-2 items-end">
        {/* Guided questions panel */}
        <div className="bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm text-xs max-w-48">
          <button
            onClick={() => setShowGuide(!showGuide)}
            className="w-full flex items-center justify-between p-2 hover:bg-[var(--muted)] rounded-md transition-colors"
          >
            <span className="flex items-center gap-1.5 font-medium">
              <Lightbulb className="h-3.5 w-3.5 text-amber-500" />
              Explore
            </span>
            {showGuide ? <ChevronDown className="h-3 w-3" /> : <ChevronUp className="h-3 w-3" />}
          </button>

          {showGuide && (
            <div className="px-2 pb-2 space-y-2">
              {/* Contextual suggestions when device selected */}
              {selectedDevicePK && mode === 'explore' && (
                <div className="space-y-1">
                  <div className="text-muted-foreground text-[10px] uppercase tracking-wide">Selected Device</div>
                  <button
                    onClick={() => analyzeImpact(selectedDevicePK)}
                    className="w-full text-left px-2 py-1 hover:bg-[var(--muted)] rounded text-muted-foreground hover:text-foreground transition-colors"
                  >
                    What if this device goes down?
                  </button>
                  <button
                    onClick={() => { setMode('path'); setPathSource(selectedDevicePK); }}
                    className="w-full text-left px-2 py-1 hover:bg-[var(--muted)] rounded text-muted-foreground hover:text-foreground transition-colors"
                  >
                    Find path from this device
                  </button>
                </div>
              )}

              {/* Global suggestions */}
              <div className="space-y-1">
                <div className="text-muted-foreground text-[10px] uppercase tracking-wide">Check Health</div>
                <button
                  onClick={() => setMode('compare')}
                  className={`w-full text-left px-2 py-1 hover:bg-[var(--muted)] rounded transition-colors ${mode === 'compare' ? 'text-blue-500' : 'text-muted-foreground hover:text-foreground'}`}
                >
                  Compare configured vs ISIS
                </button>
              </div>

              <div className="space-y-1">
                <div className="text-muted-foreground text-[10px] uppercase tracking-wide">Routing</div>
                <button
                  onClick={() => setMode('path')}
                  className={`w-full text-left px-2 py-1 hover:bg-[var(--muted)] rounded transition-colors ${mode === 'path' ? 'text-amber-500' : 'text-muted-foreground hover:text-foreground'}`}
                >
                  Find path between devices
                </button>
              </div>

              {/* Anomaly alert if issues detected */}
              {compareData && compareData.discrepancies.length > 0 && mode !== 'compare' && (
                <div className="pt-1 border-t border-[var(--border)]">
                  <button
                    onClick={() => setMode('compare')}
                    className="w-full text-left px-2 py-1 hover:bg-[var(--muted)] rounded transition-colors text-amber-500"
                  >
                    <span className="flex items-center gap-1">
                      <AlertTriangle className="h-3 w-3" />
                      {compareData.discrepancies.length} topology issues
                    </span>
                  </button>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Legend + Stats combined */}
        <div className="bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm p-2 text-xs">
          <div className="font-medium mb-1 text-muted-foreground">Device Types</div>
          <div className="flex flex-col gap-1">
            {deviceTypes.map((type) => {
              const colors = DEVICE_TYPE_COLORS[type.toLowerCase()] || DEVICE_TYPE_COLORS.default
              return (
                <div key={type} className="flex items-center gap-1.5">
                  <div
                    className="w-3 h-3 rounded-full border-2"
                    style={{
                      backgroundColor: isDark ? colors.dark : colors.light,
                      borderColor: isDark ? '#22c55e' : '#16a34a',
                    }}
                  />
                  <span className="capitalize">{type}</span>
                </div>
              )
            })}
          </div>
          <div className="mt-2 pt-2 border-t border-[var(--border)]">
            <div className="flex items-center gap-1.5">
              <div className="w-3 h-3 rounded-full border-2" style={{ borderColor: isDark ? '#22c55e' : '#16a34a', backgroundColor: 'transparent' }} />
              <span>Active</span>
            </div>
            <div className="flex items-center gap-1.5 mt-1">
              <div className="w-3 h-3 rounded-full border-2" style={{ borderColor: isDark ? '#ef4444' : '#dc2626', backgroundColor: 'transparent' }} />
              <span>Inactive</span>
            </div>
          </div>
          {/* Stats integrated */}
          {filteredData && (
            <div className="mt-2 pt-2 border-t border-[var(--border)] text-muted-foreground">
              <div>{filteredData.nodes.length} devices · {filteredData.edges.length} adjacencies</div>
              {(localStatusFilter !== 'all' || localTypeFilter !== 'all') && (
                <div className="mt-1 text-amber-500">Filtered</div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Node tooltip */}
      {hoveredNode && (
        <div
          className="absolute pointer-events-none z-50 bg-background/95 backdrop-blur border rounded-md shadow-lg p-3 text-sm"
          style={{
            left: Math.min(hoveredNode.x + 20, (containerRef.current?.clientWidth || 500) - 200),
            top: hoveredNode.y - 10,
          }}
        >
          <div className="font-medium">{hoveredNode.label}</div>
          <div className="text-muted-foreground text-xs mt-1 space-y-0.5">
            <div>Type: <span className="capitalize">{hoveredNode.deviceType}</span></div>
            <div>Status: {hoveredNode.status}</div>
            <div>Connections: {hoveredNode.degree}</div>
            {hoveredNode.systemId && <div>System ID: {hoveredNode.systemId}</div>}
          </div>
        </div>
      )}

      {/* Edge tooltip */}
      {hoveredEdge && (
        <div
          className="absolute pointer-events-none z-50 bg-background/95 backdrop-blur border rounded-md shadow-lg p-2 text-xs"
          style={{
            left: hoveredEdge.x + 10,
            top: hoveredEdge.y - 10,
          }}
        >
          {hoveredEdge.isInterMetroEdge ? (
            // Inter-metro edge tooltip
            <div className="text-muted-foreground">
              <div className="font-medium text-foreground mb-1">Inter-Metro Link</div>
              <div>
                Links: <span className="font-medium text-blue-500">{hoveredEdge.linkCount || 1}</span>
              </div>
              <div>
                Avg Latency: <span className="font-medium text-foreground">
                  {hoveredEdge.avgMetric ? `${(hoveredEdge.avgMetric / 1000).toFixed(2)}ms` : 'N/A'}
                </span>
              </div>
            </div>
          ) : (
            // Regular edge tooltip
            <>
              <div className="text-muted-foreground">
                Latency: <span className="font-medium text-foreground">{hoveredEdge.metric ? `${(hoveredEdge.metric / 1000).toFixed(2)}ms` : 'N/A'}</span>
              </div>
              {hoveredEdge.health && (
                <>
                  <div className="text-muted-foreground">
                    Committed: <span className="font-medium text-foreground">{(hoveredEdge.health.committedRttNs / 1000000).toFixed(2)}ms</span>
                  </div>
                  <div className="text-muted-foreground">
                    SLA Ratio: <span className={`font-medium ${
                      hoveredEdge.health.slaRatio >= 2.0 ? 'text-red-500' :
                      hoveredEdge.health.slaRatio >= 1.5 ? 'text-yellow-500' : 'text-green-500'
                    }`}>{(hoveredEdge.health.slaRatio * 100).toFixed(0)}%</span>
                  </div>
                  <div className="text-muted-foreground">
                    Packet Loss: <span className={`font-medium ${
                      hoveredEdge.health.lossPct > 10 ? 'text-red-500' :
                      hoveredEdge.health.lossPct > 0.1 ? 'text-yellow-500' : 'text-green-500'
                    }`}>{hoveredEdge.health.lossPct.toFixed(2)}%</span>
                  </div>
                  <div className="text-muted-foreground">
                    Status: <span className={`font-medium ${
                      hoveredEdge.health.status === 'critical' ? 'text-red-500' :
                      hoveredEdge.health.status === 'warning' ? 'text-yellow-500' :
                      hoveredEdge.health.status === 'healthy' ? 'text-green-500' : 'text-muted-foreground'
                    }`}>{hoveredEdge.health.status}</span>
                  </div>
                </>
              )}
            </>
          )}
        </div>
      )}

    </div>
  )
}
