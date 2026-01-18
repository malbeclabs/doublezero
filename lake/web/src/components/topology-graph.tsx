import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import cytoscape from 'cytoscape'
import type { Core, NodeSingular, EdgeSingular } from 'cytoscape'
import { useQuery } from '@tanstack/react-query'
import { ZoomIn, ZoomOut, Maximize, Search, Filter, Route, X, GitCompare, AlertTriangle, Zap, Lightbulb, ChevronDown, ChevronUp, Shield, MinusCircle, PlusCircle } from 'lucide-react'
import { fetchISISTopology, fetchISISPaths, fetchTopologyCompare, fetchFailureImpact, fetchCriticalLinks, fetchSimulateLinkRemoval, fetchSimulateLinkAddition } from '@/lib/api'
import type { PathMode, FailureImpactResponse, MultiPathResponse, SimulateLinkRemovalResponse, SimulateLinkAdditionResponse } from '@/lib/api'
import { useTheme } from '@/hooks/use-theme'

// Device type colors
const DEVICE_TYPE_COLORS: Record<string, { light: string; dark: string }> = {
  core: { light: '#7c3aed', dark: '#a78bfa' },      // purple
  edge: { light: '#2563eb', dark: '#60a5fa' },      // blue
  pop: { light: '#0891b2', dark: '#22d3ee' },       // cyan
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
  const navigate = useNavigate()
  const { theme } = useTheme()
  const isDark = theme === 'dark'

  // Use refs for callbacks to avoid re-initializing the graph
  const onDeviceSelectRef = useRef(onDeviceSelect)
  const navigateRef = useRef(navigate)
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

  // Initialize Cytoscape
  useEffect(() => {
    if (!containerRef.current || !filteredData) return

    const cy = cytoscape({
      container: containerRef.current,
      elements: [
        ...filteredData.nodes.map(node => ({
          group: 'nodes' as const,
          data: {
            id: node.data.id,
            label: node.data.label,
            status: node.data.status,
            deviceType: node.data.deviceType,
            systemId: node.data.systemId,
            routerId: node.data.routerId,
            metroPK: node.data.metroPK,
            degree: nodesDegree.get(node.data.id) || 0,
          },
        })),
        ...filteredData.edges.map(edge => ({
          group: 'edges' as const,
          data: {
            id: edge.data.id,
            source: edge.data.source,
            target: edge.data.target,
            metric: edge.data.metric,
          },
        })),
      ],
      style: [
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
      ],
      layout: {
        name: 'cose',
        animate: false,
        nodeDimensionsIncludeLabels: true,
        idealEdgeLength: 120,
        nodeRepulsion: 10000,
        gravity: 0.2,
        numIter: 500,
      },
      minZoom: 0.1,
      maxZoom: 3,
      wheelSensitivity: 0.3,
    })

    cyRef.current = cy

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
      setHoveredEdge({
        id: edge.data('id'),
        source: edge.data('source'),
        target: edge.data('target'),
        metric: edge.data('metric'),
        x: midpoint.x * zoom + pan.x,
        y: midpoint.y * zoom + pan.y,
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

    return () => {
      cy.destroy()
      cyRef.current = null
    }
  }, [filteredData, nodesDegree, isDark, getDeviceTypeColor, getStatusBorderColor, getNodeSize])

  // Handle node clicks based on mode
  useEffect(() => {
    if (!cyRef.current) return
    const cy = cyRef.current

    const handleNodeTap = (event: cytoscape.EventObject) => {
      const node = event.target
      const devicePK = node.data('id')

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
      navigateRef.current(`/devices/${node.data('id')}`)
    }

    cy.on('tap', 'node', handleNodeTap)
    cy.on('dbltap', 'node', handleNodeDblTap)

    return () => {
      cy.off('tap', 'node', handleNodeTap)
      cy.off('dbltap', 'node', handleNodeDblTap)
    }
  }, [mode, pathSource, pathTarget, additionSource, additionTarget, impactDevice])

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
  }, [mode])

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
  }, [selectedDevicePK, mode])

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
            {Object.entries(DEVICE_TYPE_COLORS).filter(([k]) => k !== 'default').map(([type, colors]) => (
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
            ))}
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
          <div className="text-muted-foreground">
            Latency: <span className="font-medium text-foreground">{hoveredEdge.metric ? `${(hoveredEdge.metric / 1000).toFixed(2)}ms` : 'N/A'}</span>
          </div>
        </div>
      )}

    </div>
  )
}
