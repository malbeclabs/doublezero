import { useState, useEffect } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
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
} from 'lucide-react'
import { cn, handleRowClick } from '@/lib/utils'
import { useTheme } from '@/hooks/use-theme'
import { useVersionCheck } from '@/hooks/use-version-check'
import {
  type QuerySession,
  type ChatSession,
  getSessionPreview,
  getChatSessionPreview,
} from '@/lib/sessions'

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
  const navigate = useNavigate()
  const location = useLocation()
  const { resolvedTheme, setTheme } = useTheme()
  const { updateAvailable, reload } = useVersionCheck()
  const isLandingPage = location.pathname === '/'
  const isTopologyPage = location.pathname === '/topology'
  const isStatusPage = location.pathname === '/status'
  const isDZRoute = location.pathname.startsWith('/dz/')
  const isSolanaRoute = location.pathname.startsWith('/solana/')

  const [isCollapsed, setIsCollapsed] = useState(() => {
    // Check localStorage for saved preference
    const saved = localStorage.getItem('sidebar-collapsed')
    if (saved !== null) return saved === 'true'
    // Default to collapsed on small screens, landing page, or topology page
    if (typeof window !== 'undefined' && window.innerWidth < 1024) return true
    // On landing page, topology page, status page, or entity pages, default to collapsed
    return window.location.pathname === '/' || window.location.pathname === '/topology' || window.location.pathname === '/status' || window.location.pathname.startsWith('/dz/') || window.location.pathname.startsWith('/solana/')
  })
  const [userCollapsed, setUserCollapsed] = useState<boolean | null>(() => {
    const saved = localStorage.getItem('sidebar-user-collapsed')
    return saved !== null ? saved === 'true' : null
  })

  // Auto-collapse/expand based on route and user preference
  useEffect(() => {
    // If user has explicit preference, respect it
    if (userCollapsed !== null) return

    const isSmall = typeof window !== 'undefined' && window.innerWidth < 1024
    if (isSmall) {
      setIsCollapsed(true)
    } else {
      // Landing page, topology page, status page, and entity pages default to collapsed
      setIsCollapsed(isLandingPage || isTopologyPage || isStatusPage || isDZRoute || isSolanaRoute)
    }
  }, [isLandingPage, isTopologyPage, isStatusPage, isDZRoute, isSolanaRoute, userCollapsed])

  // Auto-collapse/expand on resize based on user preference
  useEffect(() => {
    const checkWidth = () => {
      const isSmall = window.innerWidth < 1024
      if (isSmall) {
        // Always collapse on small screens
        setIsCollapsed(true)
      } else if (userCollapsed === null) {
        // No user preference - use route-based default
        setIsCollapsed(isLandingPage || isTopologyPage || isStatusPage || isDZRoute || isSolanaRoute)
      } else {
        // Respect user preference
        setIsCollapsed(userCollapsed)
      }
    }

    window.addEventListener('resize', checkWidth)
    return () => window.removeEventListener('resize', checkWidth)
  }, [userCollapsed, isLandingPage, isTopologyPage, isStatusPage, isDZRoute, isSolanaRoute])

  // Save collapsed state to localStorage
  useEffect(() => {
    localStorage.setItem('sidebar-collapsed', String(isCollapsed))
  }, [isCollapsed])

  const handleSetCollapsed = (collapsed: boolean) => {
    setIsCollapsed(collapsed)
    setUserCollapsed(collapsed)
    localStorage.setItem('sidebar-user-collapsed', String(collapsed))
  }

  const toggleTheme = () => {
    setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')
  }

  const isQueryRoute = location.pathname.startsWith('/query')
  const isChatRoute = location.pathname.startsWith('/chat')
  const isTopologyRoute = location.pathname === '/topology'
  const isStatusRoute = location.pathname === '/status'
  const isTimelineRoute = location.pathname === '/timeline'
  const isQuerySessions = location.pathname === '/query/sessions'
  const isChatSessions = location.pathname === '/chat/sessions'

  // Entity routes
  const isDevicesRoute = location.pathname === '/dz/devices'
  const isLinksRoute = location.pathname === '/dz/links'
  const isMetrosRoute = location.pathname === '/dz/metros'
  const isContributorsRoute = location.pathname === '/dz/contributors'
  const isUsersRoute = location.pathname === '/dz/users'
  const isValidatorsRoute = location.pathname === '/solana/validators'
  const isGossipNodesRoute = location.pathname === '/solana/gossip-nodes'

  // Sort sessions by updatedAt, most recent first, filter out empty sessions, and limit to 10
  const sortedQuerySessions = [...querySessions]
    .filter(s => s.history.length > 0)
    .sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime())
    .slice(0, 10)
  const sortedChatSessions = [...chatSessions]
    .filter(s => s.messages.length > 0)
    .sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime())
    .slice(0, 10)

  if (isCollapsed) {
    return (
      <div className="w-12 border-r bg-[var(--sidebar)] flex flex-col items-center relative z-10">
        {/* Logo icon - matches expanded header height */}
        <div className="w-full h-12 flex items-center justify-center border-b border-border/50">
          <button
            onClick={() => handleSetCollapsed(false)}
            className="group relative"
            title="Expand sidebar"
          >
            <img src={resolvedTheme === 'dark' ? '/logoDarkSm.svg' : '/logoLightSm.svg'} alt="Data" className="h-6 group-hover:opacity-0 transition-opacity" />
            <PanelLeftOpen className="h-5 w-5 absolute inset-0 m-auto opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground" />
          </button>
        </div>

        <div className="flex flex-col items-center gap-1 pt-4">
        {/* Home nav item */}
        <button
          onClick={(e) => handleRowClick(e, '/', navigate)}
          className={cn(
            'p-2 rounded transition-colors',
            isLandingPage
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Home"
        >
          <Home className="h-4 w-4" />
        </button>

        {/* Status nav item */}
        <button
          onClick={(e) => handleRowClick(e, '/status', navigate)}
          className={cn(
            'p-2 rounded transition-colors',
            isStatusRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Status"
        >
          <Activity className="h-4 w-4" />
        </button>

        {/* Timeline nav item */}
        <button
          onClick={(e) => handleRowClick(e, '/timeline', navigate)}
          className={cn(
            'p-2 rounded transition-colors',
            isTimelineRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Timeline"
        >
          <Clock className="h-4 w-4" />
        </button>

        {/* Chat nav item */}
        <button
          onClick={onNewChatSession}
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
          onClick={onNewQuerySession}
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
        <button
          onClick={(e) => handleRowClick(e, '/topology', navigate)}
          className={cn(
            'p-2 rounded transition-colors',
            isTopologyRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Topology"
        >
          <Globe className="h-4 w-4" />
        </button>

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

        {/* DZ nav item */}
        <button
          onClick={(e) => handleRowClick(e, '/dz/devices', navigate)}
          className={cn(
            'p-2 rounded transition-colors',
            isDZRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="DoubleZero"
        >
          <Server className="h-4 w-4" />
        </button>

        {/* Solana nav item */}
        <button
          onClick={(e) => handleRowClick(e, '/solana/validators', navigate)}
          className={cn(
            'p-2 rounded transition-colors',
            isSolanaRoute
              ? 'bg-[oklch(25%_.04_250)] text-white hover:bg-[oklch(30%_.05_250)]'
              : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
          )}
          title="Solana"
        >
          <Landmark className="h-4 w-4" />
        </button>
        </div>

        {/* Theme toggle and collapse toggle at bottom */}
        <div className="flex-1" />
        <div className="flex flex-col items-center gap-1 mb-3">
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
            title={resolvedTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          >
            {resolvedTheme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </button>
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
      <div className="px-3 h-12 flex items-center justify-between border-b border-border/50">
        <button
          onClick={(e) => handleRowClick(e, '/', navigate)}
          className="flex items-center gap-2 hover:opacity-80 transition-opacity"
        >
          <img src={resolvedTheme === 'dark' ? '/logoDark.svg' : '/logoLight.svg'} alt="DoubleZero" className="h-6" />
          <span className="wordmark text-lg">Data</span>
        </button>
        <button
          onClick={() => handleSetCollapsed(true)}
          className="p-1 text-muted-foreground hover:text-foreground transition-colors"
          title="Collapse sidebar"
        >
          <PanelLeftClose className="h-4 w-4 translate-y-0.5" />
        </button>
      </div>

      {/* Tools section */}
      <div className="px-3 pt-4">
        <div className="px-3 mb-2">
          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Tools</span>
        </div>
        <div className="space-y-1">
          <button
            onClick={(e) => handleRowClick(e, '/status', navigate)}
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isStatusRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Activity className="h-4 w-4" />
            Status
          </button>
          <button
            onClick={(e) => handleRowClick(e, '/timeline', navigate)}
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isTimelineRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Clock className="h-4 w-4" />
            Timeline
          </button>
          <button
            onClick={onNewChatSession}
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
            onClick={onNewQuerySession}
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
          <button
            onClick={(e) => handleRowClick(e, '/topology', navigate)}
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
              isTopologyRoute
                ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
            )}
          >
            <Globe className="h-4 w-4" />
            Topology
          </button>
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
      {!isChatRoute && !isQueryRoute && (
        <div className="px-3 pt-4">
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">DoubleZero</span>
          </div>
          <div className="space-y-1">
            <button
              onClick={(e) => handleRowClick(e, '/dz/devices', navigate)}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isDevicesRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Server className="h-4 w-4" />
              Devices
            </button>
            <button
              onClick={(e) => handleRowClick(e, '/dz/links', navigate)}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isLinksRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Link2 className="h-4 w-4" />
              Links
            </button>
            <button
              onClick={(e) => handleRowClick(e, '/dz/metros', navigate)}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isMetrosRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <MapPin className="h-4 w-4" />
              Metros
            </button>
            <button
              onClick={(e) => handleRowClick(e, '/dz/contributors', navigate)}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isContributorsRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Building2 className="h-4 w-4" />
              Contributors
            </button>
            <button
              onClick={(e) => handleRowClick(e, '/dz/users', navigate)}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isUsersRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Users className="h-4 w-4" />
              Users
            </button>
          </div>
        </div>
      )}

      {/* Solana section - hidden on tool pages */}
      {!isChatRoute && !isQueryRoute && (
        <div className="px-3 pt-4">
          <div className="px-3 mb-2">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Solana</span>
          </div>
          <div className="space-y-1">
            <button
              onClick={(e) => handleRowClick(e, '/solana/validators', navigate)}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isValidatorsRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Landmark className="h-4 w-4" />
              Validators
            </button>
            <button
              onClick={(e) => handleRowClick(e, '/solana/gossip-nodes', navigate)}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded transition-colors',
                isGossipNodesRoute
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              <Radio className="h-4 w-4" />
              Gossip Nodes
            </button>
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
                  onClick={onNewQuerySession}
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
            <button
              onClick={(e) => handleRowClick(e, '/query/sessions', navigate)}
              className={cn(
                'w-full text-left px-3 py-2 text-sm rounded transition-colors',
                isQuerySessions
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              History
            </button>
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
                  onClick={() => onSelectQuerySession(session)}
                  onDelete={() => {
                    if (window.confirm('Delete this session? This cannot be undone.')) {
                      onDeleteQuerySession(session.id)
                    }
                  }}
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
                  onClick={onNewChatSession}
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
            <button
              onClick={(e) => handleRowClick(e, '/chat/sessions', navigate)}
              className={cn(
                'w-full text-left px-3 py-2 text-sm rounded transition-colors',
                isChatSessions
                  ? 'bg-[var(--sidebar-active)] text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)]'
              )}
            >
              History
            </button>
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
                  onClick={() => onSelectChatSession(session)}
                  onDelete={() => {
                    if (window.confirm('Delete this chat? This cannot be undone.')) {
                      onDeleteChatSession(session.id)
                    }
                  }}
                  onRename={(name) => onRenameChatSession(session.id, name)}
                  onGenerateTitle={onGenerateTitleChatSession ? () => onGenerateTitleChatSession(session.id) : undefined}
                />
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Spacer when no section is active */}
      {!isQueryRoute && !isChatRoute && <div className="flex-1" />}

      {/* Theme toggle and development notice footer */}
      <div className="mt-auto px-3 py-3 border-t border-border/50 space-y-3">
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
        <button
          onClick={toggleTheme}
          className="w-full flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-[var(--sidebar-active)] rounded transition-colors"
        >
          {resolvedTheme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          {resolvedTheme === 'dark' ? 'Light mode' : 'Dark mode'}
        </button>
        <p className="text-xs text-grey-40 leading-snug">
          Early preview. Chat and query history is stored locally in your browser and may be cleared.
        </p>
      </div>
    </div>
  )
}

interface SessionItemProps {
  title: string
  isActive: boolean
  onClick: () => void
  onDelete: () => void
  onRename: (name: string) => void
  onGenerateTitle?: () => Promise<void>
}

function SessionItem({ title, isActive, onClick, onDelete, onRename, onGenerateTitle }: SessionItemProps) {
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
      onClick={onClick}
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
