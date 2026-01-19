import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'

// Topology interaction modes
export type TopologyMode =
  | 'explore'      // Default mode - clicking entities selects them
  | 'path'         // Path finding mode - click source then target
  | 'whatif-removal'   // Simulate link removal
  | 'whatif-addition'  // Simulate link addition
  | 'impact'       // Device failure impact analysis

// Path finding optimization mode
export type PathMode = 'hops' | 'latency'

// Selection types that can be displayed in the panel
export type SelectionType = 'device' | 'link' | 'metro' | 'validator'

export interface Selection {
  type: SelectionType
  id: string
}

// Panel state
export interface PanelState {
  isOpen: boolean
  width: number
  content: 'details' | 'mode' | 'overlay'  // What the panel is showing
}

// Overlay toggles (visualization modes)
export interface OverlayState {
  validators: boolean          // Show validator markers (map only)
  // Device overlays (mutually exclusive)
  stake: boolean               // Stake distribution overlay (devices)
  metroClustering: boolean     // Color devices by metro
  contributorDevices: boolean  // Color devices by contributor
  // Link overlays (mutually exclusive)
  bandwidth: boolean           // Link bandwidth/capacity visualization
  isisMetric: boolean          // ISIS metric/latency visualization
  linkHealth: boolean          // Link health/SLA overlay
  trafficFlow: boolean         // Traffic flow visualization
  contributorLinks: boolean    // Color links by contributor
  criticality: boolean         // Link criticality analysis
  isisHealth: boolean          // ISIS health comparison (graph only)
}

// Context value type
export interface TopologyContextValue {
  // Current mode
  mode: TopologyMode
  setMode: (mode: TopologyMode) => void

  // Path finding mode (hops vs latency)
  pathMode: PathMode
  setPathMode: (mode: PathMode) => void

  // Selection state (synced with URL)
  selection: Selection | null
  setSelection: (selection: Selection | null) => void

  // Panel state
  panel: PanelState
  openPanel: (content: 'details' | 'mode' | 'overlay') => void
  closePanel: () => void
  setPanelWidth: (width: number) => void

  // Overlay toggles
  overlays: OverlayState
  toggleOverlay: (overlay: keyof OverlayState) => void

  // View type (provided by parent)
  view: 'map' | 'graph'

  // Hover state (for cursor-following popover)
  hoveredEntity: { type: SelectionType; id: string; x: number; y: number } | null
  setHoveredEntity: (entity: { type: SelectionType; id: string; x: number; y: number } | null) => void
}

const TopologyContext = createContext<TopologyContextValue | null>(null)

interface TopologyProviderProps {
  children: ReactNode
  view: 'map' | 'graph'
}

const DEFAULT_PANEL_WIDTH = 320

// Parse overlays from URL param (comma-separated)
// If no param, use view-specific defaults
function parseOverlaysFromUrl(param: string | null, view: 'map' | 'graph'): OverlayState {
  const defaultState: OverlayState = {
    validators: false,
    stake: false,
    metroClustering: false,
    contributorDevices: false,
    bandwidth: view === 'map',      // Default on for map view
    isisMetric: view === 'graph',   // Default on for graph view
    linkHealth: false,
    trafficFlow: false,
    contributorLinks: false,
    criticality: false,
    isisHealth: false,
  }
  if (!param) return defaultState

  // If URL has overlays param, parse it (overrides defaults)
  const parsed: OverlayState = {
    validators: false,
    stake: false,
    metroClustering: false,
    contributorDevices: false,
    bandwidth: false,
    isisMetric: false,
    linkHealth: false,
    trafficFlow: false,
    contributorLinks: false,
    criticality: false,
    isisHealth: false,
  }
  const activeOverlays = param.split(',').filter(Boolean)
  for (const overlay of activeOverlays) {
    if (overlay in parsed) {
      parsed[overlay as keyof OverlayState] = true
    }
  }
  return parsed
}

// Serialize overlays to URL param (comma-separated)
function serializeOverlaysToUrl(overlays: OverlayState): string | null {
  const active = Object.entries(overlays)
    .filter(([, value]) => value)
    .map(([key]) => key)
  return active.length > 0 ? active.join(',') : null
}

export function TopologyProvider({ children, view }: TopologyProviderProps) {
  const [searchParams, setSearchParams] = useSearchParams()

  // Mode state
  const [mode, setModeInternal] = useState<TopologyMode>('explore')

  // Path finding mode (hops vs latency)
  const [pathMode, setPathMode] = useState<PathMode>('hops')

  // Panel state with localStorage persistence for width
  const [panel, setPanel] = useState<PanelState>(() => ({
    isOpen: false,
    width: parseInt(localStorage.getItem('topology-panel-width') ?? String(DEFAULT_PANEL_WIDTH), 10),
    content: 'details' as const,
  }))

  // Overlay state - initialized from URL params with view-specific defaults
  const [overlays, setOverlays] = useState<OverlayState>(() =>
    parseOverlaysFromUrl(searchParams.get('overlays'), view)
  )

  // Hover state
  const [hoveredEntity, setHoveredEntity] = useState<{ type: SelectionType; id: string; x: number; y: number } | null>(null)

  // Get selection from URL params
  const selection: Selection | null = (() => {
    const type = searchParams.get('type') as SelectionType | null
    const id = searchParams.get('id')
    if (type && id) {
      return { type, id }
    }
    return null
  })()

  // Set selection (updates URL)
  const setSelection = useCallback((newSelection: Selection | null) => {
    setSearchParams(prev => {
      if (newSelection === null) {
        prev.delete('type')
        prev.delete('id')
      } else {
        prev.set('type', newSelection.type)
        prev.set('id', newSelection.id)
      }
      return prev
    })
  }, [setSearchParams])

  // Set mode with side effects
  const setMode = useCallback((newMode: TopologyMode) => {
    setModeInternal(newMode)

    // When entering a mode, open the panel with mode content
    if (newMode !== 'explore') {
      setPanel(prev => ({ ...prev, isOpen: true, content: 'mode' }))
    } else {
      // When returning to explore, close panel if showing mode content
      setPanel(prev => prev.content === 'mode' ? { ...prev, isOpen: false } : prev)
    }
  }, [])

  // Panel controls
  const openPanel = useCallback((content: 'details' | 'mode' | 'overlay') => {
    setPanel(prev => ({ ...prev, isOpen: true, content }))
  }, [])

  const closePanel = useCallback(() => {
    setPanel(prev => ({ ...prev, isOpen: false }))
    // If in a mode and closing panel, return to explore
    if (mode !== 'explore') {
      setModeInternal('explore')
    }
  }, [mode])

  const setPanelWidth = useCallback((width: number) => {
    const clampedWidth = Math.max(280, Math.min(600, width))
    setPanel(prev => ({ ...prev, width: clampedWidth }))
    localStorage.setItem('topology-panel-width', String(clampedWidth))
  }, [])

  // Overlay groups - overlays in the same group are mutually exclusive
  // Device overlays: stake, metroClustering, contributorDevices
  // Link overlays: bandwidth, isisMetric, linkHealth, trafficFlow, contributorLinks, criticality, isisHealth
  // Independent: validators
  const linkOverlays: (keyof OverlayState)[] = ['bandwidth', 'isisMetric', 'linkHealth', 'trafficFlow', 'contributorLinks', 'criticality', 'isisHealth']
  const overlayGroups: Record<keyof OverlayState, (keyof OverlayState)[]> = {
    // Device overlays (mutually exclusive)
    stake: ['metroClustering', 'contributorDevices'],
    metroClustering: ['stake', 'contributorDevices'],
    contributorDevices: ['stake', 'metroClustering'],
    // Link overlays (mutually exclusive) - each excludes all others
    bandwidth: linkOverlays.filter(o => o !== 'bandwidth'),
    isisMetric: linkOverlays.filter(o => o !== 'isisMetric'),
    linkHealth: linkOverlays.filter(o => o !== 'linkHealth'),
    trafficFlow: linkOverlays.filter(o => o !== 'trafficFlow'),
    contributorLinks: linkOverlays.filter(o => o !== 'contributorLinks'),
    criticality: linkOverlays.filter(o => o !== 'criticality'),
    isisHealth: linkOverlays.filter(o => o !== 'isisHealth'),
    // Independent
    validators: [],
  }

  // Overlay toggle - also updates URL and handles mutual exclusion
  const toggleOverlay = useCallback((overlay: keyof OverlayState) => {
    setOverlays(prev => {
      const newValue = !prev[overlay]
      const newState = { ...prev, [overlay]: newValue }

      // If turning on, turn off conflicting overlays in the same group
      if (newValue) {
        for (const conflicting of overlayGroups[overlay]) {
          newState[conflicting] = false
        }
      }

      // Update URL params
      setSearchParams(params => {
        const serialized = serializeOverlaysToUrl(newState)
        if (serialized) {
          params.set('overlays', serialized)
        } else {
          params.delete('overlays')
        }
        return params
      })
      return newState
    })
  }, [setSearchParams])

  const value: TopologyContextValue = {
    mode,
    setMode,
    pathMode,
    setPathMode,
    selection,
    setSelection,
    panel,
    openPanel,
    closePanel,
    setPanelWidth,
    overlays,
    toggleOverlay,
    view,
    hoveredEntity,
    setHoveredEntity,
  }

  return (
    <TopologyContext.Provider value={value}>
      {children}
    </TopologyContext.Provider>
  )
}

export function useTopology() {
  const context = useContext(TopologyContext)
  if (!context) {
    throw new Error('useTopology must be used within a TopologyProvider')
  }
  return context
}
