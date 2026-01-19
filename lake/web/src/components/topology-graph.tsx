import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import cytoscape from 'cytoscape'
import type { Core, NodeSingular, EdgeSingular } from 'cytoscape'
import { useQuery } from '@tanstack/react-query'
import { X, Zap } from 'lucide-react'
import { fetchISISTopology, fetchISISPaths, fetchTopologyCompare, fetchFailureImpact, fetchCriticalLinks, fetchSimulateLinkRemoval, fetchSimulateLinkAddition, fetchTopology, fetchLinkHealth } from '@/lib/api'
import type { FailureImpactResponse, MultiPathResponse, SimulateLinkRemovalResponse, SimulateLinkAdditionResponse } from '@/lib/api'
import { useTheme } from '@/hooks/use-theme'
import { useTopology, TopologyPanel, TopologyControlBar, DeviceDetails, LinkDetails, PathModePanel, CriticalityPanel, WhatIfRemovalPanel, WhatIfAdditionPanel, ImpactPanel, ComparePanel, StakeOverlayPanel, LinkHealthOverlayPanel, TrafficFlowOverlayPanel, MetroClusteringOverlayPanel, ContributorsOverlayPanel, BandwidthOverlayPanel, type DeviceInfo, type LinkInfo } from '@/components/topology'

// Device type colors (types from serviceability smart contract: hybrid, transit, edge)
const DEVICE_TYPE_COLORS: Record<string, { light: string; dark: string }> = {
  hybrid: { light: '#7c3aed', dark: '#a78bfa' },    // purple
  transit: { light: '#2563eb', dark: '#60a5fa' },   // blue
  edge: { light: '#0891b2', dark: '#22d3ee' },      // cyan
  default: { light: '#6b7280', dark: '#9ca3af' },   // gray
}

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

// Contributor colors for contributor overlay visualization (12 distinct colors)
const CONTRIBUTOR_COLORS = [
  { light: '#7c3aed', dark: '#8b5cf6' },  // violet
  { light: '#db2777', dark: '#ec4899' },  // pink
  { light: '#0891b2', dark: '#06b6d4' },  // cyan
  { light: '#65a30d', dark: '#84cc16' },  // lime
  { light: '#d97706', dark: '#f59e0b' },  // amber
  { light: '#4f46e5', dark: '#6366f1' },  // indigo
  { light: '#0d9488', dark: '#14b8a6' },  // teal
  { light: '#ea580c', dark: '#f97316' },  // orange
  { light: '#9333ea', dark: '#a855f7' },  // purple
  { light: '#059669', dark: '#10b981' },  // emerald
  { light: '#dc2626', dark: '#ef4444' },  // red
  { light: '#0284c7', dark: '#0ea5e9' },  // sky
]

// Format bits per second to human readable
function formatBps(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)}Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)}Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)}Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(0)}Kbps`
  return `${bps.toFixed(0)}bps`
}

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

  // Refs for maps and functions used in event handlers
  const deviceInfoMapRef = useRef<Map<string, DeviceInfo>>(new Map())
  const linkInfoMapRef = useRef<Map<string, LinkInfo>>(new Map())
  const openPanelRef = useRef<(content: 'details' | 'mode') => void>(() => {})
  const closePanelRef = useRef<() => void>(() => {})
  const setSelectedDeviceRef = useRef<(device: DeviceInfo | null) => void>(() => {})
  const setSelectedLinkRef = useRef<(link: LinkInfo | null) => void>(() => {})

  // Get unified topology context
  const { mode, setMode, pathMode, setPathMode, overlays, toggleOverlay, panel, openPanel, closePanel } = useTopology()

  // Get URL params for link selection (device selection comes via props, but links need direct access)
  const [searchParams] = useSearchParams()
  const selectedLinkPK = searchParams.get('type') === 'link' ? searchParams.get('id') : null

  // Derive overlay states from context
  const stakeOverlayEnabled = overlays.stake
  const linkHealthOverlayEnabled = overlays.linkHealth
  const trafficFlowEnabled = overlays.trafficFlow
  const metroClusteringEnabled = overlays.metroClustering
  const contributorDevicesEnabled = overlays.contributorDevices
  const contributorLinksEnabled = overlays.contributorLinks
  const bandwidthEnabled = overlays.bandwidth

  // Path finding operational state (local)
  const [pathSource, setPathSource] = useState<string | null>(null)
  const [pathTarget, setPathTarget] = useState<string | null>(null)
  const [pathsResult, setPathsResult] = useState<MultiPathResponse | null>(null)
  const [selectedPathIndex, setSelectedPathIndex] = useState<number>(0)
  const [pathLoading, setPathLoading] = useState(false)

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

  // Metro clustering operational state (local)
  const [collapsedMetros, setCollapsedMetros] = useState<Set<string>>(new Set())

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


  const { data, isLoading, error } = useQuery({
    queryKey: ['isis-topology'],
    queryFn: fetchISISTopology,
    refetchInterval: 60000,
  })

  // Fetch topology comparison when ISIS health overlay is enabled
  const isisHealthEnabled = overlays.isisHealth
  const { data: compareData, isLoading: compareLoading } = useQuery({
    queryKey: ['topology-compare'],
    queryFn: fetchTopologyCompare,
    enabled: isisHealthEnabled,
    refetchInterval: 60000,
  })

  // Fetch critical links when criticality overlay is enabled
  const criticalityEnabled = overlays.criticality
  const { data: criticalLinksData, isLoading: criticalLinksLoading } = useQuery({
    queryKey: ['critical-links'],
    queryFn: fetchCriticalLinks,
    enabled: criticalityEnabled,
    staleTime: 60000,
  })

  // Fetch ClickHouse topology for stake/metro/traffic data and entity details
  const { data: topologyData } = useQuery({
    queryKey: ['topology'],
    queryFn: fetchTopology,
    staleTime: 30000, // Refresh every 30 seconds for traffic data
    refetchInterval: trafficFlowEnabled ? 30000 : undefined,
  })

  // Selected entity state for details panel
  const [selectedDevice, setSelectedDevice] = useState<DeviceInfo | null>(null)
  const [selectedLink, setSelectedLink] = useState<LinkInfo | null>(null)

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

  // Build contributor info map from topology data (maps contributor PK to info)
  const contributorInfoMap = useMemo(() => {
    const map = new Map<string, { code: string; name: string; colorIndex: number }>()
    if (!topologyData?.devices) return map
    // Get unique contributors from devices, sorted by code
    const contributorSet = new Map<string, string>() // pk -> code
    for (const device of topologyData.devices) {
      if (device.contributor_pk && !contributorSet.has(device.contributor_pk)) {
        contributorSet.set(device.contributor_pk, device.contributor_code || device.contributor_pk)
      }
    }
    const sorted = [...contributorSet.entries()].sort((a, b) => a[1].localeCompare(b[1]))
    sorted.forEach(([pk, code], index) => {
      map.set(pk, {
        code,
        name: code, // Use code as name since we don't have name from topology
        colorIndex: index % CONTRIBUTOR_COLORS.length,
      })
    })
    return map
  }, [topologyData])

  // Build device info map from topology data (maps device PK to DeviceInfo)
  const deviceInfoMap = useMemo(() => {
    const map = new Map<string, DeviceInfo>()
    if (!topologyData?.devices) return map
    for (const device of topologyData.devices) {
      const metro = topologyData.metros?.find(m => m.pk === device.metro_pk)
      map.set(device.pk, {
        pk: device.pk,
        code: device.code,
        deviceType: device.device_type || 'unknown',
        status: device.status || 'unknown',
        metroPk: device.metro_pk || '',
        metroName: metro?.name || 'Unknown',
        contributorPk: device.contributor_pk || '',
        contributorCode: device.contributor_code || '',
        userCount: device.user_count ?? 0,
        validatorCount: device.validator_count ?? 0,
        stakeSol: device.stake_sol ? (device.stake_sol / 1_000_000_000).toLocaleString(undefined, { maximumFractionDigits: 0 }) : '0',
        stakeShare: device.stake_share ? `${(device.stake_share * 100).toFixed(2)}%` : '0%',
      })
    }
    return map
  }, [topologyData])

  // Build link info map from topology data (maps link PK to LinkInfo)
  const linkInfoMap = useMemo(() => {
    const map = new Map<string, LinkInfo>()
    if (!topologyData?.links) return map
    for (const link of topologyData.links) {
      const deviceA = topologyData.devices?.find(d => d.pk === link.side_a_pk)
      const deviceZ = topologyData.devices?.find(d => d.pk === link.side_z_pk)
      map.set(link.pk, {
        pk: link.pk,
        code: link.code || `${deviceA?.code || 'Unknown'} — ${deviceZ?.code || 'Unknown'}`,
        linkType: link.link_type || 'unknown',
        bandwidth: link.bandwidth_bps ? formatBps(link.bandwidth_bps) : 'N/A',
        latencyMs: link.latency_us ? `${(link.latency_us / 1000).toFixed(2)}ms` : 'N/A',
        jitterMs: link.jitter_us ? `${(link.jitter_us / 1000).toFixed(2)}ms` : 'N/A',
        lossPercent: link.loss_percent ? `${link.loss_percent.toFixed(2)}%` : 'N/A',
        inRate: link.in_bps ? formatBps(link.in_bps) : 'N/A',
        outRate: link.out_bps ? formatBps(link.out_bps) : 'N/A',
        deviceAPk: link.side_a_pk || '',
        deviceACode: deviceA?.code || 'Unknown',
        deviceZPk: link.side_z_pk || '',
        deviceZCode: deviceZ?.code || 'Unknown',
        contributorPk: link.contributor_pk || '',
        contributorCode: link.contributor_code || '',
      })
    }
    return map
  }, [topologyData])

  // Build reverse lookup: device pair -> link info (for finding link from edge source/target)
  const linkByDevicePairMap = useMemo(() => {
    const map = new Map<string, LinkInfo>()
    linkInfoMap.forEach((linkInfo) => {
      // Store both directions for easy lookup
      map.set(`${linkInfo.deviceAPk}->${linkInfo.deviceZPk}`, linkInfo)
      map.set(`${linkInfo.deviceZPk}->${linkInfo.deviceAPk}`, linkInfo)
    })
    return map
  }, [linkInfoMap])
  const linkByDevicePairMapRef = useRef<Map<string, LinkInfo>>(new Map())

  // Keep refs updated for use in event handlers
  useEffect(() => {
    deviceInfoMapRef.current = deviceInfoMap
  }, [deviceInfoMap])
  useEffect(() => {
    linkInfoMapRef.current = linkInfoMap
  }, [linkInfoMap])
  useEffect(() => {
    linkByDevicePairMapRef.current = linkByDevicePairMap
  }, [linkByDevicePairMap])
  useEffect(() => {
    openPanelRef.current = openPanel
  }, [openPanel])
  useEffect(() => {
    closePanelRef.current = closePanel
  }, [closePanel])
  useEffect(() => {
    setSelectedDeviceRef.current = setSelectedDevice
  }, [])
  useEffect(() => {
    setSelectedLinkRef.current = setSelectedLink
  }, [])

  // Get metro color by PK
  const getMetroColor = useCallback((metroPK: string | undefined) => {
    if (!metroPK) return isDark ? '#6b7280' : '#9ca3af' // gray for unknown
    const metroInfo = metroInfoMap.get(metroPK)
    if (!metroInfo) return isDark ? '#6b7280' : '#9ca3af'
    const colors = METRO_COLORS[metroInfo.colorIndex]
    return isDark ? colors.dark : colors.light
  }, [metroInfoMap, isDark])

  // Get contributor color by PK
  const getContributorColor = useCallback((contributorPK: string | undefined) => {
    if (!contributorPK) return isDark ? '#6b7280' : '#9ca3af' // gray for unknown
    const contributorInfo = contributorInfoMap.get(contributorPK)
    if (!contributorInfo) return isDark ? '#6b7280' : '#9ca3af'
    const colors = CONTRIBUTOR_COLORS[contributorInfo.colorIndex]
    return isDark ? colors.dark : colors.light
  }, [contributorInfoMap, isDark])

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

  // Build device contributor map from topology data (maps device PK to contributor PK)
  const deviceContributorMap = useMemo(() => {
    const map = new Map<string, string>()
    if (!topologyData?.devices) return map
    for (const device of topologyData.devices) {
      if (device.contributor_pk) {
        map.set(device.pk, device.contributor_pk)
      }
    }
    return map
  }, [topologyData])

  // Build edge contributor map from topology data (maps edge key to contributor PK)
  const edgeContributorMap = useMemo(() => {
    const map = new Map<string, string>()
    if (!topologyData?.links) return map
    for (const link of topologyData.links) {
      if (link.contributor_pk) {
        const key1 = `${link.side_a_pk}->${link.side_z_pk}`
        const key2 = `${link.side_z_pk}->${link.side_a_pk}`
        map.set(key1, link.contributor_pk)
        map.set(key2, link.contributor_pk)
      }
    }
    return map
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

    const filteredNodes = data.nodes.filter(node => {
      if (statusFilter && statusFilter !== 'all') {
        const isActive = node.data.status === 'active' || node.data.status === 'activated'
        if (statusFilter === 'active' && !isActive) return false
        if (statusFilter === 'inactive' && isActive) return false
      }
      if (deviceTypeFilter && deviceTypeFilter !== 'all' && node.data.deviceType !== deviceTypeFilter) {
        return false
      }
      return true
    })

    const nodeIds = new Set(filteredNodes.map(n => n.data.id))
    const filteredEdges = data.edges.filter(
      edge => nodeIds.has(edge.data.source) && nodeIds.has(edge.data.target)
    )

    return { nodes: filteredNodes, edges: filteredEdges }
  }, [data, statusFilter, deviceTypeFilter])

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
    const allClasses = 'path-node path-edge path-source path-target path-0 path-1 path-2 path-3 path-4 path-selected health-matched health-extra health-missing health-mismatch criticality-critical criticality-important criticality-redundant whatif-removal-candidate whatif-removed whatif-rerouted whatif-disconnected whatif-added whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained'

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
        cyRef.current.elements().removeClass('whatif-removal-candidate whatif-removed whatif-rerouted whatif-disconnected whatif-added whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
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
        // Make edges more prominent for easier clicking
        cyRef.current.edges().addClass('whatif-removal-candidate')
      }
    } else if (mode === 'whatif-addition') {
      // Clear other mode classes
      setPathSource(null)
      setPathTarget(null)
      setPathsResult(null)
      setRemovalLink(null)
      setRemovalResult(null)
      if (cyRef.current) {
        cyRef.current.elements().removeClass('path-node path-edge path-source path-target path-0 path-1 path-2 path-3 path-4 path-selected health-matched health-extra health-missing health-mismatch criticality-critical criticality-important criticality-redundant whatif-removal-candidate whatif-removed whatif-rerouted whatif-disconnected')
      }
    }
  }, [mode])

  // When entering analysis modes with a device already selected, use it appropriately
  const prevModeRef = useRef<string>(mode)
  useEffect(() => {
    const cy = cyRef.current
    if (!cy) return

    // whatif-addition: use selected device as source
    if (mode === 'whatif-addition' && prevModeRef.current !== 'whatif-addition' && selectedDevicePK) {
      setAdditionSource(selectedDevicePK)
      const node = cy.getElementById(selectedDevicePK)
      if (node.length) {
        node.addClass('whatif-addition-source')
      }
    }

    // path mode: use selected device as source
    if (mode === 'path' && prevModeRef.current !== 'path' && selectedDevicePK) {
      setPathSource(selectedDevicePK)
      const node = cy.getElementById(selectedDevicePK)
      if (node.length) {
        node.addClass('path-source')
      }
    }

    // whatif-removal: highlight adjacent links of selected device
    if (mode === 'whatif-removal' && prevModeRef.current !== 'whatif-removal' && selectedDevicePK) {
      const node = cy.getElementById(selectedDevicePK)
      if (node.length) {
        // Highlight the selected device and its adjacent edges
        node.addClass('highlighted')
        node.connectedEdges().addClass('highlighted')
      }
    }

    prevModeRef.current = mode
  }, [mode, selectedDevicePK])

  // Apply health status styles when ISIS health overlay is enabled (using direct .style() for reliability)
  // Combined: color by health status, thickness by metric
  useEffect(() => {
    if (!cyRef.current || !isisHealthEnabled) return
    const cy = cyRef.current

    if (!compareData) return

    // Helper to get width from ISIS metric (lower = better = thicker)
    const getMetricWidth = (metric: number): number => {
      if (metric <= 0) return 1
      if (metric <= 1000) return 4
      if (metric <= 5000) return 3
      if (metric <= 20000) return 2
      return 2
    }

    cy.batch(() => {
      cy.edges().forEach(edge => {
        const edgeId = edge.data('id') // format: source->target
        const status = edgeHealthStatus.get(edgeId)
        const metric = edge.data('metric') ?? 0
        const width = getMetricWidth(metric)

        if (status === 'missing') {
          edge.style({
            'line-color': '#ef4444',
            'target-arrow-color': '#ef4444',
            'line-style': 'dashed',
            'width': width,
            'opacity': 1,
          })
        } else if (status === 'extra') {
          edge.style({
            'line-color': '#f59e0b',
            'target-arrow-color': '#f59e0b',
            'width': width,
            'opacity': 1,
          })
        } else if (status === 'mismatch') {
          edge.style({
            'line-color': '#eab308',
            'target-arrow-color': '#eab308',
            'width': width,
            'opacity': 1,
          })
        } else {
          // Default to matched if no discrepancy found
          edge.style({
            'line-color': '#22c55e',
            'target-arrow-color': '#22c55e',
            'width': width,
            'opacity': 0.8,
          })
        }
      })
    })
  }, [isisHealthEnabled, compareData, edgeHealthStatus])

  // Apply criticality styles when criticality overlay is enabled (using direct .style() for reliability)
  useEffect(() => {
    if (!cyRef.current || !criticalityEnabled) return
    const cy = cyRef.current

    if (!criticalLinksData) return

    cy.batch(() => {
      cy.edges().forEach(edge => {
        const edgeId = edge.data('id') // format: source->target
        const crit = edgeCriticality.get(edgeId)

        if (crit === 'critical') {
          edge.style({
            'line-color': '#ef4444',
            'target-arrow-color': '#ef4444',
            'width': 4,
            'opacity': 1,
          })
        } else if (crit === 'important') {
          edge.style({
            'line-color': '#f59e0b',
            'target-arrow-color': '#f59e0b',
            'width': 3,
            'opacity': 0.9,
          })
        } else {
          // Redundant links - dim them
          edge.style({
            'line-color': isDark ? '#4b5563' : '#9ca3af',
            'target-arrow-color': isDark ? '#4b5563' : '#9ca3af',
            'width': 1,
            'opacity': 0.4,
          })
        }
      })
    })
  }, [criticalityEnabled, criticalLinksData, edgeCriticality, isDark])

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

  // Update node and edge colors when contributors overlay is enabled
  // Skip if in an analysis mode that styles edges or if a link overlay is active (let those control edge appearance)
  const isEdgeStylingMode = isisHealthEnabled || criticalityEnabled || bandwidthEnabled || mode === 'path' || mode === 'whatif-removal' || mode === 'whatif-addition'

  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    cy.batch(() => {
      // Apply contributor-based coloring to nodes if enabled (and stake/metro not active)
      if (contributorDevicesEnabled && contributorInfoMap.size > 0 && !stakeOverlayEnabled && !metroClusteringEnabled) {
        cy.nodes().forEach(node => {
          const devicePK = node.data('id')
          const contributorPK = deviceContributorMap.get(devicePK)
          const degree = node.data('degree')
          node.style({
            'width': getNodeSize(degree),
            'height': getNodeSize(degree),
            'background-color': getContributorColor(contributorPK),
          })
        })
      } else if (!stakeOverlayEnabled && !metroClusteringEnabled) {
        // Revert to default device type coloring for nodes
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

      // Skip edge styling if in an analysis mode that controls edges
      if (isEdgeStylingMode) return

      // Apply contributor-based coloring to edges if enabled (and linkHealth/traffic not active)
      if (contributorLinksEnabled && contributorInfoMap.size > 0 && !linkHealthOverlayEnabled && !trafficFlowEnabled) {
        cy.edges().forEach(edge => {
          const edgeId = edge.data('id')
          const contributorPK = edgeContributorMap.get(edgeId)
          if (contributorPK) {
            edge.style({
              'line-color': getContributorColor(contributorPK),
              'target-arrow-color': getContributorColor(contributorPK),
              'width': 2,
              'opacity': 0.8,
            })
          } else {
            // No contributor - dim the edge
            edge.style({
              'line-color': isDark ? '#6b7280' : '#9ca3af',
              'target-arrow-color': isDark ? '#6b7280' : '#9ca3af',
              'opacity': 0.3,
            })
          }
        })
      } else if (!linkHealthOverlayEnabled && !trafficFlowEnabled) {
        // Revert to default edge styling
        cy.edges().forEach(edge => {
          const linkType = edge.data('linkType')
          edge.style({
            'line-color': linkType === 'WAN' ? (isDark ? '#60a5fa' : '#2563eb') : (isDark ? '#9ca3af' : '#6b7280'),
            'target-arrow-color': linkType === 'WAN' ? (isDark ? '#60a5fa' : '#2563eb') : (isDark ? '#9ca3af' : '#6b7280'),
            'width': linkType === 'WAN' ? 2 : 1,
            'opacity': 0.6,
          })
        })
      }
    })
  }, [contributorDevicesEnabled, contributorLinksEnabled, contributorInfoMap, deviceContributorMap, edgeContributorMap, getContributorColor, getNodeSize, getDeviceTypeColor, stakeOverlayEnabled, metroClusteringEnabled, linkHealthOverlayEnabled, trafficFlowEnabled, isDark, isEdgeStylingMode])

  // Apply bandwidth edge styling
  // Sets width based on bandwidth, and resets color to grey (other overlays will override if active)
  useEffect(() => {
    if (!cyRef.current || !bandwidthEnabled) return
    const cy = cyRef.current

    const defaultColor = isDark ? '#6b7280' : '#9ca3af'

    cy.batch(() => {
      cy.edges().forEach(edge => {
        const bandwidth = edge.data('bandwidth') ?? 0 // bandwidth in bps
        const gbps = bandwidth / 1e9
        let width: number

        if (gbps >= 100) {
          width = 5
        } else if (gbps >= 10) {
          width = 4
        } else if (gbps >= 1) {
          width = 3
        } else if (gbps > 0) {
          width = 2
        } else {
          width = 1
        }

        // Set width and reset color to default grey
        // Color overlays (linkHealth, trafficFlow, etc.) will override if active
        edge.style({
          'line-color': defaultColor,
          'target-arrow-color': defaultColor,
          'width': width,
          'opacity': 0.6,
          'line-style': 'solid',
        })
      })
    })
  }, [bandwidthEnabled, isDark])

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
          selector: 'edge.highlighted',
          style: {
            'line-color': '#3b82f6',
            'target-arrow-color': '#3b82f6',
            'width': 5,
            'opacity': 1,
            'z-index': 999,
            'overlay-color': '#3b82f6',
            'overlay-padding': 4,
            'overlay-opacity': 0.2,
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
          selector: 'edge.whatif-removal-candidate',
          style: {
            'width': 4,
            'opacity': 0.8,
            'cursor': 'pointer',
          },
        },
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
          setSelectedDeviceRef.current(null)
          setSelectedLinkRef.current(null)
          closePanelRef.current()
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

  const analyzeImpact = useCallback(async (devicePK: string) => {
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
  }, [])

  const clearImpact = useCallback(() => {
    setImpactDevice(null)
    setImpactResult(null)
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
        // Set selected device for details panel
        const deviceInfo = deviceInfoMapRef.current.get(devicePK)
        if (deviceInfo) {
          setSelectedDeviceRef.current(deviceInfo)
          setSelectedLinkRef.current(null)
          openPanelRef.current('details')
        }
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
      } else if (mode === 'impact') {
        // Analyze impact of this device failing
        analyzeImpact(devicePK)
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
  }, [mode, pathSource, pathTarget, additionSource, additionTarget, impactDevice, cyGeneration, analyzeImpact])

  // Handle edge clicks for explore mode (link selection) and whatif-removal mode
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    const handleEdgeTap = (event: cytoscape.EventObject) => {
      const edge = event.target
      const sourcePK = edge.data('source')
      const targetPK = edge.data('target')

      // Handle whatif-removal mode
      if (mode === 'whatif-removal') {
        // Clear previous simulation
        cy.elements().removeClass('whatif-removed whatif-rerouted whatif-disconnected')
        setRemovalResult(null)

        // Set the selected link
        setRemovalLink({ sourcePK, targetPK })
        edge.addClass('whatif-removed')
        return
      }

      // Handle explore mode - select link for details panel
      if (mode === 'explore') {
        // Find link info from device pair
        const linkInfo = linkByDevicePairMapRef.current.get(`${sourcePK}->${targetPK}`)
        if (linkInfo) {
          // Update URL with link selection
          navigateRef.current(`/topology/graph?type=link&id=${linkInfo.pk}`)
          // Update state for details panel
          setSelectedLinkRef.current(linkInfo)
          setSelectedDeviceRef.current(null)
          openPanelRef.current('details')
          // Highlight the edge
          cy.edges().removeClass('highlighted')
          cy.nodes().removeClass('highlighted')
          edge.addClass('highlighted')
        }
      }
    }

    cy.on('tap', 'edge', handleEdgeTap)

    return () => {
      cy.off('tap', 'edge', handleEdgeTap)
    }
  }, [mode, cyGeneration])

  // Handle external selection changes (from URL params / omnisearch)
  useEffect(() => {
    if (!cyRef.current || mode !== 'explore') return
    const cy = cyRef.current

    cy.nodes().removeClass('highlighted')
    cy.edges().removeClass('highlighted')

    if (selectedDevicePK) {
      const node = cy.getElementById(selectedDevicePK)
      if (node.length) {
        node.addClass('highlighted')
        cy.animate({
          center: { eles: node },
          zoom: Math.max(cy.zoom(), 0.4), // Zoom in slightly if very far out, otherwise keep current zoom
          duration: 300,
        })
        // Set selectedDevice for the details panel
        const deviceInfo = deviceInfoMap.get(selectedDevicePK)
        if (deviceInfo) {
          setSelectedDevice(deviceInfo)
          setSelectedLink(null)
          openPanel('details')
        }
      }
    } else if (selectedLinkPK) {
      // Handle link selection from URL
      // First get link info to find the device PKs
      const linkInfo = linkInfoMap.get(selectedLinkPK)
      if (linkInfo) {
        // Edge ID format is "source->target", try both directions
        const edgeId1 = `${linkInfo.deviceAPk}->${linkInfo.deviceZPk}`
        const edgeId2 = `${linkInfo.deviceZPk}->${linkInfo.deviceAPk}`
        const edge = cy.getElementById(edgeId1).length ? cy.getElementById(edgeId1) : cy.getElementById(edgeId2)

        if (edge.length) {
          edge.addClass('highlighted')
          cy.animate({
            center: { eles: edge },
            zoom: Math.max(cy.zoom(), 0.4), // Zoom in slightly if very far out, otherwise keep current zoom
            duration: 300,
          })
        }
        // Set selectedLink for the details panel even if edge not found in graph
        setSelectedLink(linkInfo)
        setSelectedDevice(null)
        openPanel('details')
      }
    }
  }, [selectedDevicePK, selectedLinkPK, mode, cyGeneration, openPanel, deviceInfoMap, linkInfoMap])

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

  const clearRemoval = useCallback(() => {
    setRemovalLink(null)
    setRemovalResult(null)
    cyRef.current?.elements().removeClass('whatif-removed whatif-rerouted whatif-disconnected')
  }, [])

  const clearAddition = useCallback(() => {
    setAdditionSource(null)
    setAdditionTarget(null)
    setAdditionResult(null)
    cyRef.current?.elements().removeClass('whatif-addition-source whatif-addition-target whatif-improved whatif-redundancy-gained')
  }, [])

  // Helper to toggle mode with panel state
  const toggleMode = useCallback((targetMode: 'path' | 'whatif-removal' | 'whatif-addition' | 'impact') => {
    if (mode === targetMode) {
      // Switching off - go back to explore
      setMode('explore')
      if (panel.content === 'mode') {
        closePanel()
      }
    } else {
      // Switching on - set mode and open panel
      setMode(targetMode)
      openPanel('mode')
    }
  }, [mode, setMode, panel.content, closePanel, openPanel])

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
            if (panel.content === 'mode') {
              closePanel()
            }
          } else if (impactDevice) {
            clearImpact()
          } else if (selectedDevicePK) {
            onDeviceSelectRef.current?.(null)
          }
          cyRef.current?.elements().unselect()
          break
        case 'p':
          // Toggle path mode
          if (!e.metaKey && !e.ctrlKey) {
            toggleMode('path')
          }
          break
        case 'c':
          // Toggle criticality overlay
          if (!e.metaKey && !e.ctrlKey) {
            toggleOverlay('criticality')
          }
          break
        case 'r':
          // Toggle whatif-removal mode
          if (!e.metaKey && !e.ctrlKey) {
            toggleMode('whatif-removal')
          }
          break
        case 'a':
          // Toggle whatif-addition mode
          if (!e.metaKey && !e.ctrlKey) {
            toggleMode('whatif-addition')
          }
          break
        case 's':
          // Toggle stake overlay
          if (!e.metaKey && !e.ctrlKey) {
            toggleOverlay('stake')
          }
          break
        case 'h':
          // Toggle link health overlay
          if (!e.metaKey && !e.ctrlKey) {
            toggleOverlay('linkHealth')
          }
          break
        case 't':
          // Toggle traffic flow overlay
          if (!e.metaKey && !e.ctrlKey) {
            toggleOverlay('trafficFlow')
          }
          break
        case 'm':
          // Toggle metro clustering overlay
          if (!e.metaKey && !e.ctrlKey) {
            toggleOverlay('metroClustering')
          }
          break
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [mode, impactDevice, selectedDevicePK])

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

      {/* Unified control bar */}
      <TopologyControlBar
        onZoomIn={handleZoomIn}
        onZoomOut={handleZoomOut}
        onReset={handleFit}
        hasSelectedDevice={!!selectedDevicePK}
      />

      {/* Mode panel (right panel) */}
      {panel.isOpen && panel.content === 'mode' && (
        <TopologyPanel
          title={
            mode === 'path' ? 'Path Finding' :
            mode === 'whatif-removal' ? 'Simulate Link Removal' :
            mode === 'whatif-addition' ? 'Simulate Link Addition' :
            mode === 'impact' ? 'Failure Impact' :
            'Mode'
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
              onClear={clearRemoval}
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
              onClear={clearAddition}
            />
          )}
          {mode === 'impact' && (
            <ImpactPanel
              devicePK={impactDevice}
              result={impactResult}
              isLoading={impactLoading}
              onClose={clearImpact}
            />
          )}
        </TopologyPanel>
      )}

      {/* Overlay panels (right panel) */}
      {panel.isOpen && panel.content === 'overlay' && (
        <TopologyPanel
          title={
            isisHealthEnabled ? 'ISIS Health' :
            criticalityEnabled ? 'Link Criticality' :
            'Overlay'
          }
        >
          {isisHealthEnabled && (
            <ComparePanel
              data={compareData ?? null}
              isLoading={compareLoading}
            />
          )}
          {criticalityEnabled && (
            <CriticalityPanel
              data={criticalLinksData ?? null}
              isLoading={criticalLinksLoading}
            />
          )}
        </TopologyPanel>
      )}

      {/* Details panel (right panel) */}
      {panel.isOpen && panel.content === 'details' && (selectedDevice || selectedLink) && (
        <TopologyPanel
          title={selectedDevice ? selectedDevice.code : selectedLink?.code || 'Details'}
          subtitle={selectedDevice ? selectedDevice.deviceType : selectedLink?.linkType}
        >
          {selectedDevice && <DeviceDetails device={selectedDevice} />}
          {selectedLink && <LinkDetails link={selectedLink} />}
        </TopologyPanel>
      )}

      {/* Overlay panel (right panel) */}
      {panel.isOpen && panel.content === 'overlay' && (
        <TopologyPanel
          title={
            stakeOverlayEnabled ? 'Stake' :
            linkHealthOverlayEnabled ? 'Health' :
            trafficFlowEnabled ? 'Traffic' :
            metroClusteringEnabled ? 'Metros' :
            (contributorDevicesEnabled || contributorLinksEnabled) ? 'Contributors' :
            'Overlay'
          }
        >
          {stakeOverlayEnabled && (
            <StakeOverlayPanel
              deviceStakeMap={deviceStakeMap}
              getStakeColor={getStakeColor}
              getDeviceLabel={(pk) => cyRef.current?.getElementById(pk)?.data('label') || pk.substring(0, 8)}
              isLoading={!topologyData}
            />
          )}
          {bandwidthEnabled && (
            <BandwidthOverlayPanel />
          )}
          {linkHealthOverlayEnabled && (
            <LinkHealthOverlayPanel
              linkHealthData={linkHealthData}
              isLoading={!linkHealthData}
            />
          )}
          {trafficFlowEnabled && (
            <TrafficFlowOverlayPanel
              edgeTrafficMap={edgeTrafficMap}
              links={topologyData?.links}
              isLoading={!topologyData}
            />
          )}
          {metroClusteringEnabled && (
            <MetroClusteringOverlayPanel
              metroInfoMap={metroInfoMap}
              collapsedMetros={collapsedMetros}
              getMetroColor={getMetroColor}
              getDeviceCountForMetro={(pk) => filteredData?.nodes.filter(n => n.data.metroPK === pk).length ?? 0}
              totalDeviceCount={filteredData?.nodes.length ?? 0}
              onToggleMetroCollapse={toggleMetroCollapse}
              onCollapseAll={() => setCollapsedMetros(new Set(metroInfoMap.keys()))}
              onExpandAll={() => setCollapsedMetros(new Set())}
              isLoading={!topologyData}
            />
          )}
          {(contributorDevicesEnabled || contributorLinksEnabled) && (
            <ContributorsOverlayPanel
              contributorInfoMap={contributorInfoMap}
              getContributorColor={getContributorColor}
              getDeviceCountForContributor={(pk) => {
                let count = 0
                deviceContributorMap.forEach((cpk) => { if (cpk === pk) count++ })
                return count
              }}
              getLinkCountForContributor={(pk) => {
                const seen = new Set<string>()
                edgeContributorMap.forEach((cpk, key) => {
                  if (cpk === pk && !seen.has(key)) seen.add(key)
                })
                return seen.size / 2 // Each link has 2 entries (both directions)
              }}
              totalDeviceCount={filteredData?.nodes.length ?? 0}
              totalLinkCount={topologyData?.links?.length ?? 0}
              isLoading={!topologyData}
            />
          )}
        </TopologyPanel>
      )}

      {/* Impact analysis panel - Keep as floating panel (not mode-based) */}
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

      {/* Bottom right panels - Legend, Stats */}
      <div className="absolute bottom-4 right-4 z-[998] flex flex-col gap-2 items-end">
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
