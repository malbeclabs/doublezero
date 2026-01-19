import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'

// Topology interaction modes
export type TopologyMode =
  | 'explore'      // Default mode - clicking entities selects them
  | 'path'         // Path finding mode - click source then target
  | 'criticality'  // Show link criticality analysis
  | 'whatif-removal'   // Simulate link removal
  | 'whatif-addition'  // Simulate link addition
  | 'impact'       // Device failure impact analysis
  | 'compare'      // Topology comparison (graph only)

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
  validators: boolean       // Show validator markers (map only)
  stake: boolean           // Stake distribution overlay
  linkHealth: boolean      // Link health/SLA overlay
  trafficFlow: boolean     // Traffic flow visualization
  metroClustering: boolean // Color by metro
}

// Context value type
export interface TopologyContextValue {
  // Current mode
  mode: TopologyMode
  setMode: (mode: TopologyMode) => void

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

export function TopologyProvider({ children, view }: TopologyProviderProps) {
  const [searchParams, setSearchParams] = useSearchParams()

  // Mode state
  const [mode, setModeInternal] = useState<TopologyMode>('explore')

  // Panel state with localStorage persistence for width
  const [panel, setPanel] = useState<PanelState>(() => ({
    isOpen: false,
    width: parseInt(localStorage.getItem('topology-panel-width') ?? String(DEFAULT_PANEL_WIDTH), 10),
    content: 'details' as const,
  }))

  // Overlay state
  const [overlays, setOverlays] = useState<OverlayState>({
    validators: false,
    stake: false,
    linkHealth: false,
    trafficFlow: false,
    metroClustering: false,
  })

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

  // Overlay toggle
  const toggleOverlay = useCallback((overlay: keyof OverlayState) => {
    setOverlays(prev => ({ ...prev, [overlay]: !prev[overlay] }))
  }, [])

  const value: TopologyContextValue = {
    mode,
    setMode,
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
