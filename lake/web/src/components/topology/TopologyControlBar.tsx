import { useState, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
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
  ChevronLeft,
  ChevronRight,
  Map,
  Network,
  Building2,
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

interface NavItemProps {
  icon: React.ReactNode
  label: string
  shortcut?: string
  onClick: () => void
  active?: boolean
  disabled?: boolean
  activeColor?: 'amber' | 'red' | 'green' | 'purple' | 'blue' | 'cyan' | 'yellow'
  collapsed?: boolean
}

function NavItem({ icon, label, shortcut, onClick, active = false, disabled = false, activeColor = 'blue', collapsed = false }: NavItemProps) {
  const colorClasses: Record<string, string> = {
    amber: 'bg-amber-500/20 text-amber-500',
    red: 'bg-red-500/20 text-red-500',
    green: 'bg-green-500/20 text-green-500',
    purple: 'bg-purple-500/20 text-purple-500',
    blue: 'bg-blue-500/20 text-blue-500',
    cyan: 'bg-cyan-500/20 text-cyan-500',
    yellow: 'bg-yellow-500/20 text-yellow-500',
  }

  const activeTextClasses: Record<string, string> = {
    amber: 'text-amber-500',
    red: 'text-red-500',
    green: 'text-green-500',
    purple: 'text-purple-500',
    blue: 'text-blue-500',
    cyan: 'text-cyan-500',
    yellow: 'text-yellow-500',
  }

  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`flex items-center gap-2 px-2 py-1.5 rounded text-xs transition-colors w-full ${
        active
          ? colorClasses[activeColor]
          : 'hover:bg-[var(--muted)] text-muted-foreground hover:text-foreground'
      } ${disabled ? 'opacity-40 cursor-not-allowed' : ''}`}
      title={collapsed ? `${label}${shortcut ? ` (${shortcut})` : ''}` : undefined}
    >
      <span className={`flex-shrink-0 ${active ? activeTextClasses[activeColor] : ''}`}>
        {icon}
      </span>
      {!collapsed && (
        <>
          <span className="flex-1 text-left truncate">{label}</span>
          {shortcut && (
            <kbd className="px-1 py-0.5 bg-[var(--muted)] rounded text-[10px] text-muted-foreground flex-shrink-0">
              {shortcut}
            </kbd>
          )}
        </>
      )}
    </button>
  )
}

function SectionHeader({ title, collapsed }: { title: string; collapsed: boolean }) {
  if (collapsed) {
    return <div className="my-1.5 border-t border-[var(--border)]" />
  }
  return (
    <div className="px-2 py-1 text-[10px] uppercase tracking-wide text-muted-foreground font-medium">
      {title}
    </div>
  )
}

const STORAGE_KEY = 'topology-nav-collapsed'

export function TopologyControlBar({
  onZoomIn,
  onZoomOut,
  onReset,
  validatorCount = 0,
  hasSelectedDevice = false,
}: TopologyControlBarProps) {
  const { mode, setMode, overlays, toggleOverlay, view, panel, openPanel, closePanel } = useTopology()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  // Switch view while preserving selection params
  const switchView = (targetView: 'map' | 'graph') => {
    if (view === targetView) return
    const params = searchParams.toString()
    navigate(`/topology/${targetView}${params ? `?${params}` : ''}`)
  }

  // Persist collapsed state
  const [collapsed, setCollapsed] = useState(() => {
    if (typeof window === 'undefined') return false
    return localStorage.getItem(STORAGE_KEY) === 'true'
  })

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, String(collapsed))
  }, [collapsed])

  // Mode conflicts - certain modes can't be active together
  const isInAnalysisMode = mode === 'path' || mode === 'criticality' || mode === 'whatif-removal' || mode === 'whatif-addition' || mode === 'impact' || mode === 'compare'

  // Toggle mode helper
  const toggleMode = (targetMode: TopologyMode) => {
    if (mode === targetMode) {
      setMode('explore')
      if (panel.content === 'mode') {
        closePanel()
      }
    } else {
      setMode(targetMode)
      openPanel('mode')
    }
  }

  // Toggle overlay with panel management
  const handleToggleOverlay = (overlay: keyof typeof overlays) => {
    const currentlyActive = overlays[overlay]
    toggleOverlay(overlay)

    if (!currentlyActive) {
      openPanel('overlay')
    } else {
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
      className="absolute top-4 z-[999] transition-all duration-200"
      style={{ right: rightOffset }}
    >
      <div className={`bg-[var(--card)] border border-[var(--border)] rounded-lg shadow-sm overflow-hidden transition-all duration-200 ${collapsed ? 'w-10' : 'w-44'}`}>
        {/* Collapse toggle */}
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="w-full flex items-center justify-between px-2 py-1.5 hover:bg-[var(--muted)] transition-colors border-b border-[var(--border)]"
          title={collapsed ? 'Expand controls' : 'Collapse controls'}
        >
          {!collapsed && <span className="text-xs font-medium">Controls</span>}
          {collapsed ? (
            <ChevronLeft className="h-4 w-4 mx-auto text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        <div className="p-1 space-y-0.5">
          {/* Search */}
          <NavItem
            icon={<Search className="h-3.5 w-3.5" />}
            label="Search"
            shortcut="⌘K"
            onClick={() => window.dispatchEvent(new CustomEvent('open-search'))}
            collapsed={collapsed}
          />

          {/* View controls */}
          <SectionHeader title="View" collapsed={collapsed} />

          {/* View toggle */}
          <NavItem
            icon={<Map className="h-3.5 w-3.5" />}
            label="Map view"
            onClick={() => switchView('map')}
            active={view === 'map'}
            activeColor="blue"
            collapsed={collapsed}
          />
          <NavItem
            icon={<Network className="h-3.5 w-3.5" />}
            label="Graph view"
            onClick={() => switchView('graph')}
            active={view === 'graph'}
            activeColor="blue"
            collapsed={collapsed}
          />

          {onZoomIn && (
            <NavItem
              icon={<ZoomIn className="h-3.5 w-3.5" />}
              label="Zoom in"
              onClick={onZoomIn}
              collapsed={collapsed}
            />
          )}
          {onZoomOut && (
            <NavItem
              icon={<ZoomOut className="h-3.5 w-3.5" />}
              label="Zoom out"
              onClick={onZoomOut}
              collapsed={collapsed}
            />
          )}
          {onReset && (
            <NavItem
              icon={<Maximize className="h-3.5 w-3.5" />}
              label="Reset view"
              onClick={onReset}
              collapsed={collapsed}
            />
          )}

          {/* Analysis modes */}
          <SectionHeader title="Analysis" collapsed={collapsed} />

          <NavItem
            icon={<Route className="h-3.5 w-3.5" />}
            label="Find paths"
            shortcut="p"
            onClick={() => toggleMode('path')}
            active={mode === 'path'}
            activeColor="amber"
            disabled={mode !== 'explore' && mode !== 'path'}
            collapsed={collapsed}
          />

          <NavItem
            icon={<Shield className="h-3.5 w-3.5" />}
            label="Link criticality"
            shortcut="c"
            onClick={() => toggleMode('criticality')}
            active={mode === 'criticality'}
            activeColor="red"
            disabled={mode !== 'explore' && mode !== 'criticality'}
            collapsed={collapsed}
          />

          {view === 'graph' && (
            <NavItem
              icon={<GitCompare className="h-3.5 w-3.5" />}
              label="Compare topology"
              onClick={() => toggleMode('compare')}
              active={mode === 'compare'}
              activeColor="blue"
              disabled={mode !== 'explore' && mode !== 'compare'}
              collapsed={collapsed}
            />
          )}

          {/* What-if scenarios */}
          <SectionHeader title="What-if" collapsed={collapsed} />

          <NavItem
            icon={<MinusCircle className="h-3.5 w-3.5" />}
            label="Remove link"
            shortcut="r"
            onClick={() => toggleMode('whatif-removal')}
            active={mode === 'whatif-removal'}
            activeColor="red"
            disabled={mode !== 'explore' && mode !== 'whatif-removal'}
            collapsed={collapsed}
          />

          <NavItem
            icon={<PlusCircle className="h-3.5 w-3.5" />}
            label="Add link"
            shortcut="a"
            onClick={() => toggleMode('whatif-addition')}
            active={mode === 'whatif-addition'}
            activeColor="green"
            disabled={mode !== 'explore' && mode !== 'whatif-addition'}
            collapsed={collapsed}
          />

          <NavItem
            icon={<Zap className="h-3.5 w-3.5" />}
            label="Failure impact"
            shortcut="i"
            onClick={() => toggleMode('impact')}
            active={mode === 'impact'}
            activeColor="purple"
            disabled={(mode !== 'explore' && mode !== 'impact') || (!hasSelectedDevice && mode !== 'impact')}
            collapsed={collapsed}
          />

          {/* Device Overlays */}
          <SectionHeader title="Device Overlays" collapsed={collapsed} />

          <NavItem
            icon={<MapPin className="h-3.5 w-3.5" />}
            label="Metros"
            shortcut="m"
            onClick={() => handleToggleOverlay('metroClustering')}
            active={overlays.metroClustering}
            activeColor="blue"
            disabled={isInAnalysisMode}
            collapsed={collapsed}
          />

          <NavItem
            icon={<Building2 className="h-3.5 w-3.5" />}
            label="Contributors"
            onClick={() => handleToggleOverlay('contributorDevices')}
            active={overlays.contributorDevices}
            activeColor="purple"
            disabled={isInAnalysisMode}
            collapsed={collapsed}
          />

          {view === 'map' && (
            <NavItem
              icon={<Users className="h-3.5 w-3.5" />}
              label={`Validators (${validatorCount})`}
              onClick={() => toggleOverlay('validators')}
              active={overlays.validators}
              activeColor="purple"
              disabled={isInAnalysisMode}
              collapsed={collapsed}
            />
          )}

          <NavItem
            icon={<Coins className="h-3.5 w-3.5" />}
            label="Stake"
            shortcut="s"
            onClick={() => handleToggleOverlay('stake')}
            active={overlays.stake}
            activeColor="yellow"
            disabled={isInAnalysisMode}
            collapsed={collapsed}
          />

          {/* Link Overlays */}
          <SectionHeader title="Link Overlays" collapsed={collapsed} />

          <NavItem
            icon={<Activity className="h-3.5 w-3.5" />}
            label="Health"
            shortcut="h"
            onClick={() => handleToggleOverlay('linkHealth')}
            active={overlays.linkHealth}
            activeColor="green"
            disabled={isInAnalysisMode}
            collapsed={collapsed}
          />

          <NavItem
            icon={<BarChart3 className="h-3.5 w-3.5" />}
            label="Traffic"
            shortcut="t"
            onClick={() => handleToggleOverlay('trafficFlow')}
            active={overlays.trafficFlow}
            activeColor="cyan"
            disabled={isInAnalysisMode}
            collapsed={collapsed}
          />

          <NavItem
            icon={<Building2 className="h-3.5 w-3.5" />}
            label="Contributors"
            onClick={() => handleToggleOverlay('contributorLinks')}
            active={overlays.contributorLinks}
            activeColor="purple"
            disabled={isInAnalysisMode}
            collapsed={collapsed}
          />
        </div>
      </div>
    </div>
  )
}
