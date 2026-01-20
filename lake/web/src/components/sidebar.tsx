import { useState, useEffect } from 'react'
import { useLocation, Link } from 'react-router-dom'
import {
  PanelLeftClose,
  PanelLeftOpen,
  MessageSquare,
  Database,
  Globe,
  Activity,
  Trash2,
  MoreHorizontal,
  Pencil,
  RefreshCw,
  Sun,
  Moon,
  Settings,
  ArrowUpCircle,
  Server,
  Link2,
  MapPin,
  Users,
  Building2,
  Landmark,
  Radio,
  Home,
  Clock,
  Search,
  Route,
  Map,
  Network,
  Shield,
  Wrench,
  AlertTriangle,
  Gauge,
  Zap,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTheme } from '@/hooks/use-theme'
import { useVersionCheck } from '@/hooks/use-version-check'
import {
  type QuerySession,
  type ChatSession,
  getSessionPreview,
  getChatSessionPreview,
} from '@/lib/sessions'
import { ConfirmDialog } from './confirm-dialog'

interface SidebarProps {
  // Query sessions
  querySessions: QuerySession[]
  currentQuerySessionId: string
  onNewQuerySession: () => void
  onSelectQuerySession: (session: QuerySession) => void
  onDeleteQuerySession: (sessionId: string) => void
  onRenameQuerySession: (sessionId: string, name: string) => void
  onGenerateTitleQuerySession?: (sessionId: string) => Promise<void>
  // Chat sessions
  chatSessions: ChatSession[]
  currentChatSessionId: string
  onNewChatSession: () => void
  onSelectChatSession: (session: ChatSession) => void
  onDeleteChatSession: (sessionId: string) => void
  onRenameChatSession: (sessionId: string, name: string) => void
  onGenerateTitleChatSession?: (sessionId: string) => Promise<void>
}

export function Sidebar({
  querySessions,
  currentQuerySessionId,
  onNewQuerySession,
  onSelectQuerySession,
  onDeleteQuerySession,
  onRenameQuerySession,
  onGenerateTitleQuerySession,
  chatSessions,
  currentChatSessionId,
  onNewChatSession,
  onSelectChatSession,
  onDeleteChatSession,
  onRenameChatSession,
  onGenerateTitleChatSession,
}: SidebarProps) {
  const location = useLocation()
  const { resolvedTheme, setTheme } = useTheme()
  const { updateAvailable, reload } = useVersionCheck()
  const isLandingPage = location.pathname === '/'
  const isTopologyRoute = location.pathname === '/topology' || location.pathname.startsWith('/topology/')
  const isTopologyMapOrGraph = location.pathname === '/topology/map' || location.pathname === '/topology/graph'
  const isStatusPage = location.pathname.startsWith('/status')
  const isOutagesPage = location.pathname === '/outages'
  const isDZRoute = location.pathname.startsWith('/dz/')
  const isSolanaRoute = location.pathname.startsWith('/solana/')

  const [isCollapsed, setIsCollapsed] = useState(() => {
    // Check localStorage for saved preference
    const saved = localStorage.getItem('sidebar-collapsed')
    if (saved !== null) return saved === 'true'
    // Default to collapsed on small screens, landing page, or topology page
    if (typeof window !== 'undefined' && window.innerWidth < 1024) return true
    // On landing page, topology page, status page, outages page, or entity pages, default to collapsed
    return window.location.pathname === '/' || window.location.pathname === '/topology' || window.location.pathname === '/status' || window.location.pathname === '/outages' || window.location.pathname.startsWith('/dz/') || window.location.pathname.startsWith('/solana/')
  })
  const [userCollapsed, setUserCollapsed] = useState<boolean | null>(() => {
    const saved = localStorage.getItem('sidebar-user-collapsed')
    return saved !== null ? saved === 'true' : null
  })
  const [deleteSession, setDeleteSession] = useState<{ id: string; type: 'query' | 'chat'; title: string } | null>(null)

  // Auto-collapse/expand based on route and user preference
  useEffect(() => {
    // Always collapse on topology map/graph views (ignores user preference)
    if (isTopologyMapOrGraph) {
      setIsCollapsed(true)
      return
    }

    // If user has explicit preference, respect it
    if (userCollapsed !== null) {
      setIsCollapsed(userCollapsed)
      return
    }

    const isSmall = typeof window !== 'undefined' && window.innerWidth < 1024
    if (isSmall) {
      setIsCollapsed(true)
    } else {
      // Landing page, status page, and entity pages default to collapsed
      setIsCollapsed(isLandingPage || isStatusPage || isOutagesPage || isDZRoute || isSolanaRoute)
    }
  }, [isLandingPage, isTopologyMapOrGraph, isStatusPage, isOutagesPage, isDZRoute, isSolanaRoute, userCollapsed])

  // Auto-collapse/expand on resize based on user preference
  useEffect(() => {
    const checkWidth = () => {
      // Always collapse on topology map/graph views
      if (isTopologyMapOrGraph) {
        setIsCollapsed(true)
        return
      }

      const isSmall = window.innerWidth < 1024
      if (isSmall) {
        // Always collapse on small screens
        setIsCollapsed(true)
      } else if (userCollapsed === null) {
        // No user preference - use route-based default
        setIsCollapsed(isLandingPage || isStatusPage || isOutagesPage || isDZRoute || isSolanaRoute)
      } else {
        // Respect user preference
        setIsCollapsed(userCollapsed)
      }
    }

    window.addEventListener('resize', checkWidth)
    return () => window.removeEventListener('resize', checkWidth)
  }, [userCollapsed, isLandingPage, isTopologyMapOrGraph, isStatusPage, isOutagesPage, isDZRoute, isSolanaRoute])

  // Save collapsed state to localStorage
  useEffect(() => {
    localStorage.setItem('sidebar-collapsed', String(isCollapsed))
  }, [isCollapsed])

  const handleSetCollapsed = (collapsed: boolean) => {
    setIsCollapsed(collapsed)
    setUserCollapsed(collapsed)
    localStorage.setItem('sidebar-user-collapsed', String(collapsed))
  }

  // Toggle between light and dark (respects current resolved theme)
  const toggleTheme = () => {
    setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')
  }

  const currentThemeIcon = () => {
    return resolvedTheme === 'dark' ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />
  }

  const isQueryRoute = location.pathname.startsWith('/query')
  const isChatRoute = location.pathname.startsWith('/chat')
  const isStatusRoute = location.pathname.startsWith('/status')
  const isTimelineRoute = location.pathname === '/timeline'
  const isOutagesRoute = location.pathname === '/outages'
  const isPerformanceRoute = location.pathname.startsWith('/performance')
  const isQuerySessions = location.pathname === '/query/sessions'
  const isChatSessions = location.pathname === '/chat/sessions'

  // Topology sub-routes
  const isTopologyMap = location.pathname === '/topology/map'
  const isTopologyGraph = location.pathname === '/topology/graph'
  const isTopologyPathCalculator = location.pathname === '/topology/path-calculator'
  const isTopologyRedundancy = location.pathname === '/topology/redundancy'
  const isTopologyMetroConnectivity = location.pathname === '/topology/metro-connectivity'
  const isTopologyMaintenance = location.pathname === '/topology/maintenance'

  // Performance sub-routes
  const isPerformanceDzVsInternet = location.pathname === '/performance/dz-vs-internet'
  const isPerformancePathLatency = location.pathname === '/performance/path-latency'

  // Entity routes
  const isDevicesRoute = location.pathname === '/dz/devices'
  const isLinksRoute = location.pathname === '/dz/links'
  const isMetrosRoute = location.pathname === '/dz/metros'
  const isContributorsRoute = location.pathname === '/dz/contributors'
  const isUsersRoute = location.pathname === '/dz/users'
  const isValidatorsRoute = location.pathname === '/solana/validators'
  const isGossipNodesRoute = location.pathname === '/solana/gossip-nodes'

  // Sort sessions by updatedAt, most recent first, filter out empty sessions, and limit to 10
  // Use id as tiebreaker for stable ordering when timestamps are equal
  const sortedQuerySessions = [...querySessions]
    .filter(s => s.history.length > 0)
    .sort((a, b) => {
      const timeDiff = b.updatedAt.getTime() - a.updatedAt.getTime()
      return timeDiff !== 0 ? timeDiff : a.id.localeCompare(b.id)
    })
    .slice(0, 10)
  const sortedChatSessions = [...chatSessions]
    .filter(s => s.messages.length > 0)
    .sort((a, b) => {
      const timeDiff = b.updatedAt.getTime() - a.updatedAt.getTime()
      return timeDiff !== 0 ? timeDiff : a.id.localeCompare(b.id)
    })
    .slice(0, 10)

  if (isCollapsed) {
    return (
      <div className="w-12 border-r bg-[var(--sidebar)] flex flex-col items-center relative z-10">
        {/* Logo icon - matches expanded header height */}
        <div className="w-full h-12 flex items-center justify-center border-b border-border/50 shrink-0">
          <button
            onClick={() => handleSetCollapsed(false)}
            className="group relative"
            title="Expand sidebar"
          >
            <img src={resolvedTheme === 'dark' ? '/logoDarkSm.svg' : '/logoLightSm.svg'} alt="Data" className="h-6 group-hover:opacity-0 transition-opacity" />
            <PanelLeftOpen className="h-5 w-5 absolute inset-0 m-auto opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground" />
          </button>
        </div>

        <div className="flex-1 flex flex-col items-center gap-1 pt-4 overflow-y-auto min-h-0">
        {/* Home nav item */}
        <Link
          to="/"
          className={cn(
            'p-2 rounded transition-colors',
            isLandingPage
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Home"
        >
          <Home className="h-4 w-4" />
        </Link>

        {/* Status nav item */}
        <Link
          to="/status"
          className={cn(
            'p-2 rounded transition-colors',
            isStatusRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Status"
        >
          <Activity className="h-4 w-4" />
        </Link>

        {/* Timeline nav item */}
        <Link
          to="/timeline"
          className={cn(
            'p-2 rounded transition-colors',
            isTimelineRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Timeline"
        >
          <Clock className="h-4 w-4" />
        </Link>

        {/* Chat nav item */}
        <button
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey) {
              window.open('/chat', '_blank')
            } else {
              onNewChatSession()
            }
          }}
          className={cn(
            'p-2 rounded transition-colors',
            isChatRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Chat"
        >
          <MessageSquare className="h-4 w-4" />
        </button>

        {/* Query nav item */}
        <button
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey) {
              window.open('/query', '_blank')
            } else {
              onNewQuerySession()
            }
          }}
          className={cn(
            'p-2 rounded transition-colors',
            isQueryRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Query"
        >
          <Database className="h-4 w-4" />
        </button>

        {/* Topology nav item */}
        <Link
          to="/topology"
          className={cn(
            'p-2 rounded transition-colors',
            isTopologyRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Topology"
        >
          <Globe className="h-4 w-4" />
        </Link>

        {/* Outages nav item */}
        <Link
          to="/outages"
          className={cn(
            'p-2 rounded transition-colors',
            isOutagesRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Outages"
        >
          <AlertTriangle className="h-4 w-4" />
        </Link>

        {/* Performance nav item */}
        <Link
          to="/performance"
          className={cn(
            'p-2 rounded transition-colors',
            isPerformanceRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Performance"
        >
          <Gauge className="h-4 w-4" />
        </Link>

        {/* Search nav item */}
        <button
          onClick={() => window.dispatchEvent(new CustomEvent('open-search'))}
          className="p-2 rounded transition-colors text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]"
          title="Search (⌘K)"
        >
          <Search className="h-4 w-4" />
        </button>

        {/* Divider */}
        <div className="w-6 border-t border-border/50 my-2" />

        {/* Topology sub-pages when on topology route */}
        {isTopologyRoute ? (
          <>
            <Link
              to="/topology/map"
              className={cn(
                'p-2 rounded transition-colors',
                isTopologyMap
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="Map"
            >
              <Map className="h-4 w-4" />
            </Link>
            <Link
              to="/topology/graph"
              className={cn(
                'p-2 rounded transition-colors',
                isTopologyGraph
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="Graph"
            >
              <Network className="h-4 w-4" />
            </Link>
            <Link
              to="/topology/path-calculator"
              className={cn(
                'p-2 rounded transition-colors',
                isTopologyPathCalculator
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="Path Calculator"
            >
              <Route className="h-4 w-4" />
            </Link>
            <Link
              to="/topology/redundancy"
              className={cn(
                'p-2 rounded transition-colors',
                isTopologyRedundancy
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="Redundancy"
            >
              <Shield className="h-4 w-4" />
            </Link>
            <Link
              to="/topology/metro-connectivity"
              className={cn(
                'p-2 rounded transition-colors',
                isTopologyMetroConnectivity
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="Metro Connectivity"
            >
              <Network className="h-4 w-4" />
            </Link>
            <Link
              to="/topology/maintenance"
              className={cn(
                'p-2 rounded transition-colors',
                isTopologyMaintenance
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="Maintenance"
            >
              <Wrench className="h-4 w-4" />
            </Link>
          </>
        ) : (
          <>
            {/* DZ nav item */}
            <Link
              to="/dz/devices"
              className={cn(
                'p-2 rounded transition-colors',
                isDZRoute
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="DoubleZero"
            >
              <Server className="h-4 w-4" />
            </Link>

            {/* Solana nav item */}
            <Link
              to="/solana/validators"
              className={cn(
                'p-2 rounded transition-colors',
                isSolanaRoute
                  ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
              title="Solana"
            >
              <Landmark className="h-4 w-4" />
            </Link>
          </>
        )}
        </div>

        {/* Theme toggle and collapse toggle at bottom */}
        <div className="flex flex-col items-center gap-1 mb-3 shrink-0">
          {updateAvailable && (
            <button
              onClick={reload}
              className="p-2 text-blue-500 hover:text-blue-400 transition-colors animate-pulse"
              title="Click to reload and get the latest version"
            >
              <ArrowUpCircle className="h-4 w-4" />
            </button>
          )}
          <button
            onClick={toggleTheme}
            className="p-2 text-muted-foreground hover:text-foreground transition-colors"
            title={`Switch to ${resolvedTheme === 'dark' ? 'light' : 'dark'} mode`}
          >
            {currentThemeIcon()}
          </button>
          <Link
            to="/settings"
            className="p-2 text-muted-foreground hover:text-foreground transition-colors"
            title="Settings"
          >
            <Settings className="h-4 w-4" />
          </Link>
          <button
            onClick={() => handleSetCollapsed(false)}
            className="p-2 text-muted-foreground hover:text-foreground transition-colors"
            title="Expand sidebar"
          >
            <PanelLeftOpen className="h-4 w-4" />
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="w-64 border-r bg-[var(--sidebar)] flex flex-col relative z-10">
      {/* Header with logo and collapse */}
      <div className="px-3 h-12 flex items-center justify-between border-b border-border/50 shrink-0">
        <Link
          to="/"
          className="flex items-center gap-2 hover:opacity-80 transition-opacity cursor-pointer"
        >
          <img src={resolvedTheme === 'dark' ? '/logoDark.svg' : '/logoLight.svg'} alt="DoubleZero" className="h-6" />
          <span className="wordmark text-lg">Data</span>
        </Link>
        <button
          onClick={() => handleSetCollapsed(true)}
          className="p-1 text-muted-foreground hover:text-foreground transition-colors"
          title="Collapse sidebar"
        >
          <PanelLeftClose className="h-4 w-4 translate-y-0.5" />
        </button>
      </div>

      {/* Scrollable content area */}
      <div className="flex-1 flex flex-col overflow-y-auto min-h-0">
      {/* Tools section */}
      <div className="px-3 pt-4">
        <div className="px-3 mb-2">
          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Tools</span>
        </div>
        <div className="space-y-1">
          <Link
            to="/"
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isLandingPage
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Home className="h-4 w-4" />
            Explore
          </Link>
          <Link
            to="/status"
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isStatusRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Activity className="h-4 w-4" />
            Status
          </Link>
          <Link
            to="/timeline"
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isTimelineRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Clock className="h-4 w-4" />
            Timeline
          </Link>
          <button
            onClick={(e) => {
              if (e.metaKey || e.ctrlKey) {
                window.open('/chat', '_blank')
              } else {
                onNewChatSession()
              }
            }}
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isChatRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <MessageSquare className="h-4 w-4" />
            Chat
          </button>
          <button
            onClick={(e) => {
              if (e.metaKey || e.ctrlKey) {
                window.open('/query', '_blank')
              } else {
                onNewQuerySession()
              }
            }}
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isQueryRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Database className="h-4 w-4" />
            Query
          </button>
          <Link
            to="/topology"
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isTopologyRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Globe className="h-4 w-4" />
            Topology
          </Link>
          <Link
            to="/outages"
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isOutagesRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <AlertTriangle className="h-4 w-4" />
            Outages
          </Link>
          <Link
            to="/performance"
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isPerformanceRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Gauge className="h-4 w-4" />
            Performance
          </Link>
          <button
            onClick={() => window.dispatchEvent(new CustomEvent('open-search'))}
            className="w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]"
          >
            <Search className="h-4 w-4" />
            <span className="flex-1 text-left">Search</span>
            <kbd className="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded">⌘K</kbd>
          </button>
        </div>
      </div>

      {/* DoubleZero section - hidden on tool pages */}
      {!isChatRoute && !isQueryRoute && !isTopologyRoute && !isPerformanceRoute && (
        <div className="px-3 pt-4">
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">DoubleZero</span>
          </div>
          <div className="space-y-1">
            <Link
              to="/dz/devices"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isDevicesRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Server className="h-4 w-4" />
              Devices
            </Link>
            <Link
              to="/dz/links"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isLinksRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Link2 className="h-4 w-4" />
              Links
            </Link>
            <Link
              to="/dz/metros"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isMetrosRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <MapPin className="h-4 w-4" />
              Metros
            </Link>
            <Link
              to="/dz/contributors"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isContributorsRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Building2 className="h-4 w-4" />
              Contributors
            </Link>
            <Link
              to="/dz/users"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isUsersRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Users className="h-4 w-4" />
              Users
            </Link>
          </div>
        </div>
      )}

      {/* Solana section - hidden on tool pages */}
      {!isChatRoute && !isQueryRoute && !isTopologyRoute && !isPerformanceRoute && (
        <div className="px-3 pt-4">
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Solana</span>
          </div>
          <div className="space-y-1">
            <Link
              to="/solana/validators"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isValidatorsRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Landmark className="h-4 w-4" />
              Validators
            </Link>
            <Link
              to="/solana/gossip-nodes"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isGossipNodesRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Radio className="h-4 w-4" />
              Gossip Nodes
            </Link>
          </div>
        </div>
      )}

      {/* Query sub-section */}
      {isQueryRoute && (
        <div className="flex-1 flex flex-col min-h-0 mt-6">
          {/* Section title */}
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Query</span>
          </div>

          {/* Sub-nav */}
          <div className="px-3 space-y-1">
            {(() => {
              const isNewSession = !sortedQuerySessions.some(s => s.id === currentQuerySessionId)
              const isNewSessionActive = isNewSession && !isQuerySessions
              return (
                <button
                  onClick={(e) => {
                    if (e.metaKey || e.ctrlKey) {
                      window.open('/query', '_blank')
                    } else {
                      onNewQuerySession()
                    }
                  }}
                  className={cn(
                    'w-full text-left px-3 py-2 text-sm rounded transition-colors',
                    isNewSessionActive
                      ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                      : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
                  )}
                >
                  New query
                </button>
              )
            })()}
            <Link
              to="/query/sessions"
              className={cn(
                'w-full text-left block px-3 py-2 text-sm rounded transition-colors',
                isQuerySessions
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              History
            </Link>
          </div>

          {/* Sessions history */}
          <div className="flex-1 overflow-y-auto mt-4">
            <div className="px-3 mb-2">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Recent</span>
            </div>
            <div className="px-2 space-y-1">
              {sortedQuerySessions.map(session => (
                <SessionItem
                  key={session.id}
                  title={session.name || getSessionPreview(session)}
                  isActive={session.id === currentQuerySessionId && !isQuerySessions}
                  url={`/query/${session.id}`}
                  onClick={() => onSelectQuerySession(session)}
                  onDelete={() => setDeleteSession({
                    id: session.id,
                    type: 'query',
                    title: session.name || getSessionPreview(session),
                  })}
                  onRename={(name) => onRenameQuerySession(session.id, name)}
                  onGenerateTitle={onGenerateTitleQuerySession ? () => onGenerateTitleQuerySession(session.id) : undefined}
                />
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Chat sub-section */}
      {isChatRoute && (
        <div className="flex-1 flex flex-col min-h-0 mt-6">
          {/* Section title */}
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Chat</span>
          </div>

          {/* Sub-nav */}
          <div className="px-3 space-y-1">
            {(() => {
              const isNewSession = !sortedChatSessions.some(s => s.id === currentChatSessionId)
              const isNewSessionActive = isNewSession && !isChatSessions
              return (
                <button
                  onClick={(e) => {
                    if (e.metaKey || e.ctrlKey) {
                      window.open('/chat', '_blank')
                    } else {
                      onNewChatSession()
                    }
                  }}
                  className={cn(
                    'w-full text-left px-3 py-2 text-sm rounded transition-colors',
                    isNewSessionActive
                      ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                      : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
                  )}
                >
                  New chat
                </button>
              )
            })()}
            <Link
              to="/chat/sessions"
              className={cn(
                'w-full text-left block px-3 py-2 text-sm rounded transition-colors',
                isChatSessions
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              History
            </Link>
          </div>

          {/* Sessions history */}
          <div className="flex-1 overflow-y-auto mt-4">
            <div className="px-3 mb-2">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Recent</span>
            </div>
            <div className="px-2 space-y-1">
              {sortedChatSessions.map(session => (
                <SessionItem
                  key={session.id}
                  title={session.name || getChatSessionPreview(session)}
                  isActive={session.id === currentChatSessionId && !isChatSessions}
                  url={`/chat/${session.id}`}
                  onClick={() => onSelectChatSession(session)}
                  onDelete={() => setDeleteSession({
                    id: session.id,
                    type: 'chat',
                    title: session.name || getChatSessionPreview(session),
                  })}
                  onRename={(name) => onRenameChatSession(session.id, name)}
                  onGenerateTitle={onGenerateTitleChatSession ? () => onGenerateTitleChatSession(session.id) : undefined}
                />
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Topology sub-section */}
      {isTopologyRoute && (
        <div className="flex-1 flex flex-col min-h-0 mt-6">
          {/* Section title */}
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Topology</span>
          </div>

          {/* Sub-nav */}
          <div className="px-3 space-y-1">
            <Link
              to="/topology/map"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isTopologyMap
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Map className="h-4 w-4" />
              Map
            </Link>
            <Link
              to="/topology/graph"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isTopologyGraph
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Network className="h-4 w-4" />
              Graph
            </Link>
            <Link
              to="/topology/path-calculator"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isTopologyPathCalculator
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Route className="h-4 w-4" />
              Path Calculator
            </Link>
            <Link
              to="/topology/redundancy"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isTopologyRedundancy
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Shield className="h-4 w-4" />
              Redundancy
            </Link>
            <Link
              to="/topology/metro-connectivity"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isTopologyMetroConnectivity
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Network className="h-4 w-4" />
              Metro Connectivity
            </Link>
            <Link
              to="/topology/maintenance"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isTopologyMaintenance
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Wrench className="h-4 w-4" />
              Maintenance
            </Link>
          </div>
        </div>
      )}

      {/* Performance sub-section */}
      {isPerformanceRoute && (
        <div className="flex-1 flex flex-col min-h-0 mt-6">
          {/* Section title */}
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Performance</span>
          </div>

          {/* Sub-nav */}
          <div className="px-3 space-y-1">
            <Link
              to="/performance/dz-vs-internet"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isPerformanceDzVsInternet
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Zap className="h-4 w-4" />
              DZ vs Internet
            </Link>
            <Link
              to="/performance/path-latency"
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-sm rounded transition-colors',
                isPerformancePathLatency
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Route className="h-4 w-4" />
              Path Latency
            </Link>
          </div>
        </div>
      )}

      {/* Spacer when no section is active */}
      {!isQueryRoute && !isChatRoute && !isTopologyRoute && !isPerformanceRoute && <div className="flex-1" />}
      </div>

      {/* Footer */}
      <div className="px-3 py-3 border-t border-border/50 space-y-2 shrink-0">
        {updateAvailable && (
          <button
            onClick={reload}
            className="w-full flex items-center gap-2 px-3 py-2 text-sm text-blue-500 hover:text-blue-400 bg-blue-500/10 hover:bg-blue-500/20 rounded transition-colors"
            title="Click to reload and get the latest version"
          >
            <ArrowUpCircle className="h-4 w-4" />
            Update available
          </button>
        )}
        <div className="flex items-center gap-1">
          <button
            onClick={toggleTheme}
            className="flex-1 flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)] rounded transition-colors"
            title={`Switch to ${resolvedTheme === 'dark' ? 'light' : 'dark'} mode`}
          >
            {currentThemeIcon()}
            {resolvedTheme === 'dark' ? 'Dark' : 'Light'}
          </button>
          <Link
            to="/settings"
            className="flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)] rounded transition-colors"
            title="Settings"
          >
            <Settings className="h-4 w-4" />
          </Link>
        </div>
        <p className="text-xs text-grey-40 leading-snug">
          Early preview. Chat and query history is stored locally in your browser and may be cleared.
        </p>
      </div>

      <ConfirmDialog
        isOpen={!!deleteSession}
        title={deleteSession?.type === 'chat' ? 'Delete chat' : 'Delete session'}
        message={`Delete "${deleteSession?.title}"? This cannot be undone.`}
        onConfirm={() => {
          if (deleteSession) {
            if (deleteSession.type === 'chat') {
              onDeleteChatSession(deleteSession.id)
            } else {
              onDeleteQuerySession(deleteSession.id)
            }
          }
          setDeleteSession(null)
        }}
        onCancel={() => setDeleteSession(null)}
      />
    </div>
  )
}

interface SessionItemProps {
  title: string
  isActive: boolean
  url: string
  onClick: () => void
  onDelete: () => void
  onRename: (name: string) => void
  onGenerateTitle?: () => Promise<void>
}

function SessionItem({ title, isActive, url, onClick, onDelete, onRename, onGenerateTitle }: SessionItemProps) {
  const [showMenu, setShowMenu] = useState(false)
  const [isRenaming, setIsRenaming] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)

  const handleStartRename = () => {
    setRenameValue(title)
    setIsRenaming(true)
    setShowMenu(false)
  }

  const handleSaveRename = () => {
    const newName = renameValue.trim()
    if (newName && newName !== title) {
      onRename(newName)
    }
    setIsRenaming(false)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSaveRename()
    } else if (e.key === 'Escape') {
      setIsRenaming(false)
    }
  }

  const handleGenerateTitle = async () => {
    if (!onGenerateTitle || isGenerating) return
    setShowMenu(false)
    setIsGenerating(true)
    try {
      await onGenerateTitle()
    } finally {
      setIsGenerating(false)
    }
  }

  if (isRenaming) {
    return (
      <div className="px-3 py-1.5">
        <input
          type="text"
          value={renameValue}
          onChange={(e) => setRenameValue(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={handleSaveRename}
          autoFocus
          className="w-full text-sm bg-card border border-border px-2 py-1 focus:outline-none focus:border-foreground"
        />
      </div>
    )
  }

  return (
    <div
      className={cn(
        'group relative flex items-center gap-1 px-3 py-2 cursor-pointer transition-colors rounded',
        isActive
          ? 'bg-[var(--sidebar-active)] text-foreground'
          : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
      )}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey) {
          window.open(url, '_blank')
        } else {
          onClick()
        }
      }}
    >
      <div className={cn('flex-1 min-w-0 text-sm truncate', isActive && 'font-medium')}>
        {isGenerating ? (
          <span className="flex items-center gap-1">
            <RefreshCw className="h-3 w-3 animate-spin" />
            <span className="text-muted-foreground">Generating...</span>
          </span>
        ) : title}
      </div>
      <button
        onClick={(e) => {
          e.stopPropagation()
          setShowMenu(!showMenu)
        }}
        className="p-0.5 opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-foreground transition-all"
      >
        <MoreHorizontal className="h-3 w-3" />
      </button>

      {showMenu && (
        <>
          <div
            className="fixed inset-0 z-10"
            onClick={(e) => {
              e.stopPropagation()
              setShowMenu(false)
            }}
          />
          <div className="absolute right-0 top-full mt-1 z-20 bg-card border border-border shadow-md py-1 min-w-[120px]">
            <button
              onClick={(e) => {
                e.stopPropagation()
                handleStartRename()
              }}
              className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-foreground hover:bg-muted transition-colors"
            >
              <Pencil className="h-3 w-3" />
              Rename
            </button>
            {onGenerateTitle && (
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  handleGenerateTitle()
                }}
                className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-foreground hover:bg-muted transition-colors"
              >
                <RefreshCw className="h-3 w-3" />
                Generate Title
              </button>
            )}
            <button
              onClick={(e) => {
                e.stopPropagation()
                setShowMenu(false)
                onDelete()
              }}
              className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-destructive hover:bg-muted transition-colors"
            >
              <Trash2 className="h-3 w-3" />
              Delete
            </button>
          </div>
        </>
      )}
    </div>
  )
}
