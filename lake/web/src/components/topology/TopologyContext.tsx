import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'

// Topology interaction modes
export type TopologyMode =
  | 'explore'      // Default mode - clicking entities selects them
  | 'path'         // Path finding mode - click source then target
  | 'metro-path'   // Metro path finding mode - select source and target metros
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
  deviceType: boolean          // Color devices by type (hybrid, transit, edge) - DEFAULT
  stake: boolean               // Stake distribution overlay (devices)
  metroClustering: boolean     // Color devices by metro
  contributorDevices: boolean  // Color devices by contributor
  // Link overlays
  linkType: boolean            // Color links by type (dark fiber, wavelength, etc.) - DEFAULT
  bandwidth: boolean           // Link bandwidth/capacity - affects thickness only
  // Link color overlays (mutually exclusive with each other and linkType)
  linkHealth: boolean          // Link health/SLA overlay
  trafficFlow: boolean         // Traffic flow visualization
  contributorLinks: boolean    // Color links by contributor
  criticality: boolean         // Link criticality analysis
  isisHealth: boolean          // ISIS overlay - color by health, thickness by metric
  // Independent overlays
  multicastTrees: boolean      // Multicast tree visualization
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

  // Impact mode multi-select (devices selected for failure analysis)
  impactDevices: string[]
  toggleImpactDevice: (devicePK: string) => void
  clearImpactDevices: () => void

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
    deviceType: true,               // Default device overlay
    stake: false,
    metroClustering: false,
    contributorDevices: false,
    linkType: true,                 // Default link overlay
    bandwidth: false,
    linkHealth: false,
    trafficFlow: false,
    contributorLinks: false,
    criticality: false,
    isisHealth: false,
    multicastTrees: false,
  }
  // Suppress unused variable warning
  void view
  if (!param) return defaultState

  // If URL has overlays param, parse it (overrides defaults)
  const parsed: OverlayState = {
    validators: false,
    deviceType: false,
    stake: false,
    metroClustering: false,
    contributorDevices: false,
    linkType: false,
    bandwidth: false,
    linkHealth: false,
    trafficFlow: false,
    contributorLinks: false,
    criticality: false,
    isisHealth: false,
    multicastTrees: false,
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
  // Default to closed - panel opens when user selects an item or enters a mode
  const [panel, setPanel] = useState<PanelState>(() => ({
    isOpen: false,
    width: parseInt(localStorage.getItem('topology-panel-width') ?? String(DEFAULT_PANEL_WIDTH), 10),
    content: 'overlay' as const,
  }))

  // Overlay state - initialized from URL params with view-specific defaults
  const [overlays, setOverlays] = useState<OverlayState>(() =>
    parseOverlaysFromUrl(searchParams.get('overlays'), view)
  )

  // Hover state
  const [hoveredEntity, setHoveredEntity] = useState<{ type: SelectionType; id: string; x: number; y: number } | null>(null)

  // Impact mode multi-select state
  const [impactDevices, setImpactDevices] = useState<string[]>([])

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

  // Path-related modes that style edges (mutually exclusive with link overlays)
  const edgeStylingModes: TopologyMode[] = ['path', 'metro-path', 'whatif-removal', 'whatif-addition']
  // Link overlays (defined here for use in setMode)
  const linkOverlayKeys: (keyof OverlayState)[] = ['linkType', 'bandwidth', 'linkHealth', 'trafficFlow', 'contributorLinks', 'criticality', 'isisHealth']

  // Set mode with side effects
  const setMode = useCallback((newMode: TopologyMode) => {
    const prevMode = mode
    setModeInternal(newMode)

    // Clear impact devices when exiting impact mode
    if (prevMode === 'impact' && newMode !== 'impact') {
      setImpactDevices([])
    }

    // When entering a mode, open the panel with mode content
    if (newMode !== 'explore') {
      setPanel(prev => ({ ...prev, isOpen: true, content: 'mode' }))

      // When entering an edge-styling mode, clear all link overlays
      if (edgeStylingModes.includes(newMode)) {
        setOverlays(prev => {
          const newState = { ...prev }
          for (const overlay of linkOverlayKeys) {
            newState[overlay] = false
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
      }
    } else {
      // When returning to explore, close panel if showing mode content
      setPanel(prev => prev.content === 'mode' ? { ...prev, isOpen: false } : prev)
    }
  }, [setSearchParams, mode])

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
    // Clear selection when closing panel
    setSearchParams(prev => {
      prev.delete('type')
      prev.delete('id')
      return prev
    })
  }, [mode, setSearchParams])

  const setPanelWidth = useCallback((width: number) => {
    const clampedWidth = Math.max(280, Math.min(600, width))
    setPanel(prev => ({ ...prev, width: clampedWidth }))
    localStorage.setItem('topology-panel-width', String(clampedWidth))
  }, [])

  // Device overlays (mutually exclusive within group)
  const deviceOverlays: (keyof OverlayState)[] = ['deviceType', 'stake', 'metroClustering', 'contributorDevices']
  // Link overlays (mutually exclusive within group)
  const linkOverlays: (keyof OverlayState)[] = ['linkType', 'bandwidth', 'linkHealth', 'trafficFlow', 'contributorLinks', 'criticality', 'isisHealth']

  // Overlay toggle - one device overlay + one link overlay allowed (validators independent)
  // Enabling a link overlay exits edge-styling modes (path, whatif)
  const toggleOverlay = useCallback((overlay: keyof OverlayState) => {
    setOverlays(prev => {
      const newValue = !prev[overlay]
      const newState = { ...prev, [overlay]: newValue }

      // If turning on, turn off other overlays in the same group
      if (newValue) {
        if (deviceOverlays.includes(overlay)) {
          // Turn off other device overlays
          for (const other of deviceOverlays) {
            if (other !== overlay) newState[other] = false
          }
        } else if (linkOverlays.includes(overlay)) {
          // Turn off other link overlays
          for (const other of linkOverlays) {
            if (other !== overlay) newState[other] = false
          }
          // Exit edge-styling modes when enabling a link overlay
          if (edgeStylingModes.includes(mode)) {
            setModeInternal('explore')
            setPanel(prev => prev.content === 'mode' ? { ...prev, isOpen: false } : prev)
          }
        }
        // validators is independent, no conflicts
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
  }, [setSearchParams, mode])

  // Impact device multi-select controls
  const toggleImpactDevice = useCallback((devicePK: string) => {
    setImpactDevices(prev => {
      if (prev.includes(devicePK)) {
        return prev.filter(pk => pk !== devicePK)
      } else {
        return [...prev, devicePK]
      }
    })
  }, [])

  const clearImpactDevices = useCallback(() => {
    setImpactDevices([])
  }, [])

  const value: TopologyContextValue = {
    mode,
    setMode,
    pathMode,
    setPathMode,
    selection,
    setSelection,
    impactDevices,
    toggleImpactDevice,
    clearImpactDevices,
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
