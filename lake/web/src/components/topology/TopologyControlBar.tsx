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
  Server,
  Link2,
  Radio,
} from 'lucide-react'
import { useTopology, type TopologyMode, type PathMode } from './TopologyContext'

interface TopologyControlBarProps {
  // Zoom controls (view-specific)
  onZoomIn?: () => void
  onZoomOut?: () => void
  onReset?: () => void
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

export function TopologyControlBar({
  onZoomIn,
  onZoomOut,
  onReset,
}: TopologyControlBarProps) {
  const { mode, setMode, pathMode, setPathMode, overlays, toggleOverlay, view, panel, openPanel, closePanel } = useTopology()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  // Switch view while preserving selection params
  const switchView = (targetView: 'map' | 'graph') => {
    if (view === targetView) return
    const params = searchParams.toString()
    navigate(`/topology/${targetView}${params ? `?${params}` : ''}`)
  }

  const STORAGE_KEY = 'topology-nav-collapsed'

  // Persist collapsed state
  const [collapsed, setCollapsed] = useState(() => {
    if (typeof window === 'undefined') return false
    return localStorage.getItem(STORAGE_KEY) === 'true'
  })

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, String(collapsed))
  }, [collapsed])

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

  // Toggle path mode with specific path type
  const togglePathMode = (pathType: PathMode) => {
    // If already in this exact path mode, toggle off
    if (mode === 'path' && pathMode === pathType) {
      setMode('explore')
      if (panel.content === 'mode') {
        closePanel()
      }
    } else {
      setPathMode(pathType)
      setMode('path')
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

  return (
    <div
      className="absolute top-4 right-4 z-[999] flex flex-col max-h-[calc(100vh-2rem)]"
    >
      <div className={`bg-[var(--card)] border border-[var(--border)] rounded-lg shadow-sm overflow-hidden transition-all duration-200 flex flex-col max-h-full ${collapsed ? 'w-10' : 'w-44'}`}>
        {/* Collapse toggle */}
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="w-full flex items-center justify-between px-2 py-1.5 hover:bg-[var(--muted)] transition-colors border-b border-[var(--border)] flex-shrink-0"
          title={collapsed ? 'Expand controls' : 'Collapse controls'}
        >
          {!collapsed && <span className="text-xs font-medium">Controls</span>}
          {collapsed ? (
            <ChevronLeft className="h-4 w-4 mx-auto text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>

        <div className="p-1 space-y-0.5 overflow-y-auto flex-1">
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

          {/* Find paths */}
          <SectionHeader title="Find Paths" collapsed={collapsed} />

          <NavItem
            icon={<Route className="h-3.5 w-3.5" />}
            label="Fewest hops"
            shortcut="p"
            onClick={() => togglePathMode('hops')}
            active={mode === 'path' && pathMode === 'hops'}
            activeColor="amber"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Route className="h-3.5 w-3.5" />}
            label="Lowest latency"
            shortcut="l"
            onClick={() => togglePathMode('latency')}
            active={mode === 'path' && pathMode === 'latency'}
            activeColor="amber"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Building2 className="h-3.5 w-3.5" />}
            label="Metro paths"
            onClick={() => toggleMode('metro-path')}
            active={mode === 'metro-path'}
            activeColor="cyan"
            collapsed={collapsed}
          />

          {/* What-if scenarios */}
          <SectionHeader title="What-if" collapsed={collapsed} />

          <NavItem
            icon={<MinusCircle className="h-3.5 w-3.5" />}
            label="Remove link"
            shortcut="r"
            onClick={() => toggleMode('whatif-removal')}
            active={mode === 'whatif-removal'}
            activeColor="red"
            collapsed={collapsed}
          />

          <NavItem
            icon={<PlusCircle className="h-3.5 w-3.5" />}
            label="Add link"
            shortcut="a"
            onClick={() => toggleMode('whatif-addition')}
            active={mode === 'whatif-addition'}
            activeColor="green"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Zap className="h-3.5 w-3.5" />}
            label="Device failure"
            shortcut="i"
            onClick={() => toggleMode('impact')}
            active={mode === 'impact'}
            activeColor="purple"
            collapsed={collapsed}
          />

          {/* Device Overlays */}
          <SectionHeader title="Device Overlays" collapsed={collapsed} />

          <NavItem
            icon={<Server className="h-3.5 w-3.5" />}
            label="Type"
            shortcut="d"
            onClick={() => handleToggleOverlay('deviceType')}
            active={overlays.deviceType}
            activeColor="blue"
            collapsed={collapsed}
          />

          <NavItem
            icon={<MapPin className="h-3.5 w-3.5" />}
            label="Metros"
            shortcut="m"
            onClick={() => handleToggleOverlay('metroClustering')}
            active={overlays.metroClustering}
            activeColor="blue"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Building2 className="h-3.5 w-3.5" />}
            label="Contributors"
            onClick={() => handleToggleOverlay('contributorDevices')}
            active={overlays.contributorDevices}
            activeColor="purple"
            collapsed={collapsed}
          />

          {view === 'map' && (
            <NavItem
              icon={<Users className="h-3.5 w-3.5" />}
              label="Validators"
              onClick={() => handleToggleOverlay('validators')}
              active={overlays.validators}
              activeColor="purple"
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
            collapsed={collapsed}
          />

          {/* Link Overlays */}
          <SectionHeader title="Link Overlays" collapsed={collapsed} />

          <NavItem
            icon={<Link2 className="h-3.5 w-3.5" />}
            label="Type"
            shortcut="l"
            onClick={() => handleToggleOverlay('linkType')}
            active={overlays.linkType}
            activeColor="blue"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Activity className="h-3.5 w-3.5" />}
            label="Bandwidth"
            shortcut="b"
            onClick={() => handleToggleOverlay('bandwidth')}
            active={overlays.bandwidth}
            activeColor="blue"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Building2 className="h-3.5 w-3.5" />}
            label="Contributors"
            onClick={() => handleToggleOverlay('contributorLinks')}
            active={overlays.contributorLinks}
            activeColor="purple"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Shield className="h-3.5 w-3.5" />}
            label="Criticality"
            shortcut="c"
            onClick={() => handleToggleOverlay('criticality')}
            active={overlays.criticality}
            activeColor="red"
            collapsed={collapsed}
          />

          <NavItem
            icon={<Activity className="h-3.5 w-3.5" />}
            label="Health"
            shortcut="h"
            onClick={() => handleToggleOverlay('linkHealth')}
            active={overlays.linkHealth}
            activeColor="green"
            collapsed={collapsed}
          />

          <NavItem
            icon={<GitCompare className="h-3.5 w-3.5" />}
            label="ISIS"
            onClick={() => handleToggleOverlay('isisHealth')}
            active={overlays.isisHealth}
            activeColor="green"
            collapsed={collapsed}
          />

          <NavItem
            icon={<BarChart3 className="h-3.5 w-3.5" />}
            label="Traffic"
            shortcut="t"
            onClick={() => handleToggleOverlay('trafficFlow')}
            active={overlays.trafficFlow}
            activeColor="cyan"
            collapsed={collapsed}
          />

          {/* Multicast Trees */}
          <SectionHeader title="Multicast Trees" collapsed={collapsed} />

          <NavItem
            icon={<Radio className="h-3.5 w-3.5" />}
            label="Multicast"
            onClick={() => handleToggleOverlay('multicastTrees')}
            active={overlays.multicastTrees}
            activeColor="purple"
            collapsed={collapsed}
          />
        </div>
      </div>
    </div>
  )
}
