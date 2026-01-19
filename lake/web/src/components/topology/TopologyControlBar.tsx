import {
  Search,
  ZoomIn,
  ZoomOut,
  Maximize,
  Users,
  Route,
  Shield,
  MinusCircle,
  PlusCircle,
  Zap,
  Coins,
  Activity,
  BarChart3,
  MapPin,
  GitCompare,
} from 'lucide-react'
import { useTopology, type TopologyMode } from './TopologyContext'

interface TopologyControlBarProps {
  // Zoom controls (view-specific)
  onZoomIn?: () => void
  onZoomOut?: () => void
  onReset?: () => void
  // Validator count for badge (map only)
  validatorCount?: number
  // Whether a device is selected (for impact mode)
  hasSelectedDevice?: boolean
}

interface ControlButtonProps {
  icon: React.ReactNode
  title: string
  onClick: () => void
  active?: boolean
  disabled?: boolean
  activeColor?: 'amber' | 'red' | 'green' | 'purple' | 'blue' | 'cyan' | 'yellow'
}

function ControlButton({ icon, title, onClick, active = false, disabled = false, activeColor = 'blue' }: ControlButtonProps) {
  const colorClasses: Record<string, string> = {
    amber: 'bg-amber-500/20 border-amber-500/50 text-amber-500',
    red: 'bg-red-500/20 border-red-500/50 text-red-500',
    green: 'bg-green-500/20 border-green-500/50 text-green-500',
    purple: 'bg-purple-500/20 border-purple-500/50 text-purple-500',
    blue: 'bg-blue-500/20 border-blue-500/50 text-blue-500',
    cyan: 'bg-cyan-500/20 border-cyan-500/50 text-cyan-500',
    yellow: 'bg-yellow-500/20 border-yellow-500/50 text-yellow-500',
  }

  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`p-2 border rounded shadow-sm transition-colors ${
        active
          ? colorClasses[activeColor]
          : 'bg-[var(--card)] border-[var(--border)] hover:bg-[var(--muted)]'
      } ${disabled ? 'opacity-40 cursor-not-allowed' : ''}`}
      title={title}
    >
      {icon}
    </button>
  )
}

function Divider() {
  return <div className="my-1 border-t border-[var(--border)]" />
}

export function TopologyControlBar({
  onZoomIn,
  onZoomOut,
  onReset,
  validatorCount = 0,
  hasSelectedDevice = false,
}: TopologyControlBarProps) {
  const { mode, setMode, overlays, toggleOverlay, view, panel, openPanel, closePanel } = useTopology()

  // Mode conflicts - certain modes can't be active together
  const isInAnalysisMode = mode === 'path' || mode === 'criticality' || mode === 'whatif-removal' || mode === 'whatif-addition' || mode === 'impact' || mode === 'compare'

  // Toggle mode helper
  const toggleMode = (targetMode: TopologyMode) => {
    setMode(mode === targetMode ? 'explore' : targetMode)
  }

  // Toggle overlay with panel management
  const handleToggleOverlay = (overlay: keyof typeof overlays) => {
    const currentlyActive = overlays[overlay]
    toggleOverlay(overlay)

    if (!currentlyActive) {
      // Turning on - open the panel with overlay content
      openPanel('overlay')
    } else {
      // Turning off - check if any other overlays are still active
      const otherOverlays = Object.entries(overlays)
        .filter(([key]) => key !== overlay)
        .some(([, value]) => value)

      if (!otherOverlays && panel.content === 'overlay') {
        closePanel()
      }
    }
  }

  // Calculate right offset based on panel state
  const rightOffset = panel.isOpen ? panel.width + 16 : 16

  return (
    <div
      className="absolute top-4 z-[999] flex flex-col gap-1 transition-all duration-200"
      style={{ right: rightOffset }}
    >
      {/* Search (opens omnisearch) */}
      <ControlButton
        icon={<Search className="h-4 w-4" />}
        title="Search (Cmd+K)"
        onClick={() => window.dispatchEvent(new CustomEvent('open-search'))}
      />

      <Divider />

      {/* Zoom controls */}
      {onZoomIn && (
        <ControlButton
          icon={<ZoomIn className="h-4 w-4" />}
          title="Zoom in"
          onClick={onZoomIn}
        />
      )}
      {onZoomOut && (
        <ControlButton
          icon={<ZoomOut className="h-4 w-4" />}
          title="Zoom out"
          onClick={onZoomOut}
        />
      )}
      {onReset && (
        <ControlButton
          icon={<Maximize className="h-4 w-4" />}
          title="Reset view"
          onClick={onReset}
        />
      )}

      {(onZoomIn || onZoomOut || onReset) && <Divider />}

      {/* Validators toggle (map only) */}
      {view === 'map' && (
        <ControlButton
          icon={<Users className="h-4 w-4" />}
          title={overlays.validators ? `Hide validators (${validatorCount})` : `Show validators (${validatorCount})`}
          onClick={() => toggleOverlay('validators')}
          active={overlays.validators}
          activeColor="purple"
          disabled={isInAnalysisMode}
        />
      )}

      {/* Path mode */}
      <ControlButton
        icon={<Route className="h-4 w-4" />}
        title={mode === 'path' ? 'Exit path finding (Esc)' : 'Find paths (p)'}
        onClick={() => toggleMode('path')}
        active={mode === 'path'}
        activeColor="amber"
        disabled={mode !== 'explore' && mode !== 'path'}
      />

      {/* Criticality mode */}
      <ControlButton
        icon={<Shield className="h-4 w-4" />}
        title={mode === 'criticality' ? 'Exit criticality mode (Esc)' : 'Show link criticality (c)'}
        onClick={() => toggleMode('criticality')}
        active={mode === 'criticality'}
        activeColor="red"
        disabled={mode !== 'explore' && mode !== 'criticality'}
      />

      {/* Compare mode (graph only) */}
      {view === 'graph' && (
        <ControlButton
          icon={<GitCompare className="h-4 w-4" />}
          title={mode === 'compare' ? 'Exit compare mode (Esc)' : 'Compare topology'}
          onClick={() => toggleMode('compare')}
          active={mode === 'compare'}
          activeColor="blue"
          disabled={mode !== 'explore' && mode !== 'compare'}
        />
      )}

      <Divider />

      {/* What-if removal mode */}
      <ControlButton
        icon={<MinusCircle className="h-4 w-4" />}
        title={mode === 'whatif-removal' ? 'Exit link removal simulation (Esc)' : 'Simulate link removal (r)'}
        onClick={() => toggleMode('whatif-removal')}
        active={mode === 'whatif-removal'}
        activeColor="red"
        disabled={mode !== 'explore' && mode !== 'whatif-removal'}
      />

      {/* What-if addition mode */}
      <ControlButton
        icon={<PlusCircle className="h-4 w-4" />}
        title={mode === 'whatif-addition' ? 'Exit link addition simulation (Esc)' : 'Simulate adding a link (a)'}
        onClick={() => toggleMode('whatif-addition')}
        active={mode === 'whatif-addition'}
        activeColor="green"
        disabled={mode !== 'explore' && mode !== 'whatif-addition'}
      />

      {/* Impact mode */}
      <ControlButton
        icon={<Zap className="h-4 w-4" />}
        title={mode === 'impact' ? 'Exit impact analysis (Esc)' : hasSelectedDevice ? 'Analyze failure impact (i)' : 'Select a device first'}
        onClick={() => toggleMode('impact')}
        active={mode === 'impact'}
        activeColor="purple"
        disabled={(mode !== 'explore' && mode !== 'impact') || (!hasSelectedDevice && mode !== 'impact')}
      />

      <Divider />

      {/* Overlay toggles */}
      <ControlButton
        icon={<Coins className="h-4 w-4" />}
        title={overlays.stake ? 'Hide stake overlay (s)' : 'Show stake distribution (s)'}
        onClick={() => handleToggleOverlay('stake')}
        active={overlays.stake}
        activeColor="yellow"
        disabled={isInAnalysisMode}
      />

      <ControlButton
        icon={<Activity className="h-4 w-4" />}
        title={overlays.linkHealth ? 'Hide link health (h)' : 'Show link health (h)'}
        onClick={() => handleToggleOverlay('linkHealth')}
        active={overlays.linkHealth}
        activeColor="green"
        disabled={isInAnalysisMode}
      />

      <ControlButton
        icon={<BarChart3 className="h-4 w-4" />}
        title={overlays.trafficFlow ? 'Hide traffic flow (t)' : 'Show traffic flow (t)'}
        onClick={() => handleToggleOverlay('trafficFlow')}
        active={overlays.trafficFlow}
        activeColor="cyan"
        disabled={isInAnalysisMode}
      />

      <ControlButton
        icon={<MapPin className="h-4 w-4" />}
        title={overlays.metroClustering ? 'Hide metro colors (m)' : 'Show metro colors (m)'}
        onClick={() => handleToggleOverlay('metroClustering')}
        active={overlays.metroClustering}
        activeColor="blue"
        disabled={isInAnalysisMode}
      />
    </div>
  )
}
