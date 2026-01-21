import { useState, useRef, useEffect, useCallback, createContext, useContext } from 'react'
import { Routes, Route, Navigate, useNavigate, useParams, useLocation } from 'react-router-dom'
import { useChatSessions, useDeleteChatSession, useRenameChatSession, useGenerateChatTitle } from '@/hooks/use-chat'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { WalletProviderWrapper } from '@/components/auth/WalletProviderWrapper'
import { AuthProvider, useAuth } from '@/contexts/AuthContext'
import { Catalog } from '@/components/catalog'
import { PromptInput } from '@/components/prompt-input'
import { QueryEditor, type QueryEditorHandle } from '@/components/query-editor'
import { ResultsView } from '@/components/results-view'
import { SessionHistory, type GenerationRecord } from '@/components/session-history'
import { SessionsPage } from '@/components/sessions-page'
import { ChatSessionsPage } from '@/components/chat-sessions-page'
import { SimplifiedChatView } from '@/components/chat-view'
import { Landing } from '@/components/landing'
import { Sidebar } from '@/components/sidebar'
import { SearchSpotlight } from '@/components/search-spotlight'
import { TopologyPage } from '@/components/topology-page'
import { PathCalculatorPage } from '@/components/path-calculator-page'
import { RedundancyReportPage } from '@/components/redundancy-report-page'
import { MetroConnectivityPage } from '@/components/metro-connectivity-page'
import { DzVsInternetPage } from '@/components/dz-vs-internet-page'
import { PathLatencyPage } from '@/components/path-latency-page'
import { MaintenancePlannerPage } from '@/components/maintenance-planner-page'
import { StatusPage } from '@/components/status-page'
import { TimelinePage } from '@/components/timeline-page'
import { OutagesPage } from '@/components/outages-page'
import { StatusAppendix } from '@/components/status-appendix'
import { DevicesPage } from '@/components/devices-page'
import { LinksPage } from '@/components/links-page'
import { MetrosPage } from '@/components/metros-page'
import { ContributorsPage } from '@/components/contributors-page'
import { UsersPage } from '@/components/users-page'
import { ValidatorsPage } from '@/components/validators-page'
import { GossipNodesPage } from '@/components/gossip-nodes-page'
import { DeviceDetailPage } from '@/components/device-detail-page'
import { LinkDetailPage } from '@/components/link-detail-page'
import { MetroDetailPage } from '@/components/metro-detail-page'
import { ContributorDetailPage } from '@/components/contributor-detail-page'
import { UserDetailPage } from '@/components/user-detail-page'
import { ValidatorDetailPage } from '@/components/validator-detail-page'
import { GossipNodeDetailPage } from '@/components/gossip-node-detail-page'
import { StakePage } from '@/components/stake-page'
import { SettingsPage } from '@/components/settings-page'
import { ConnectionError } from '@/components/ConnectionError'
import { generateSessionTitle, recommendVisualization, fetchCatalog, type SessionQueryInfo } from '@/lib/api'
import type { TableInfo, QueryResponse, HistoryMessage, QueryMode } from '@/lib/api'
import {
  type QuerySession,
  type ChatSession,
  createSession,
  createSessionWithId,
} from '@/lib/sessions'
import {
  useQuerySessionSync,
  useSessionDelete,
} from '@/lib/sync'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Retry 3 times with exponential backoff on network errors
      retry: (failureCount, error) => {
        // Don't retry on 4xx errors
        if (error instanceof Error && error.message.includes('4')) return false
        return failureCount < 3
      },
      retryDelay: (attemptIndex) => Math.min(500 * 2 ** attemptIndex, 5000),
      // Keep data fresh for 30 seconds
      staleTime: 30 * 1000,
      // Refetch on window focus after being away
      refetchOnWindowFocus: true,
    },
  },
})

// Context for app state
interface AppContextType {
  // Query state
  sessions: QuerySession[]
  setSessions: React.Dispatch<React.SetStateAction<QuerySession[]>>
  currentSessionId: string
  setCurrentSessionId: React.Dispatch<React.SetStateAction<string>>
  sessionsLoaded: boolean
  queryServerSyncComplete: boolean
  query: string
  setQuery: React.Dispatch<React.SetStateAction<string>>
  results: QueryResponse | null
  setResults: React.Dispatch<React.SetStateAction<QueryResponse | null>>
  autoRun: boolean
  setAutoRun: React.Dispatch<React.SetStateAction<boolean>>
  queryEditorRef: React.RefObject<QueryEditorHandle | null>
  // Pending query to load (used when navigating from chat with SQL)
  pendingQueryRef: React.MutableRefObject<string | null>
}

const AppContext = createContext<AppContextType | null>(null)

function useAppContext() {
  const ctx = useContext(AppContext)
  if (!ctx) throw new Error('useAppContext must be used within AppProvider')
  return ctx
}

// Stable component for syncing query session from URL
function QuerySessionSync({ children }: { children: React.ReactNode }) {
  const { sessionId } = useParams()
  const loadedSessionRef = useRef<string | null>(null)
  const {
    sessions,
    setSessions,
    sessionsLoaded,
    queryServerSyncComplete,
    setCurrentSessionId,
    setResults,
    setQuery,
    queryEditorRef,
    pendingQueryRef,
  } = useAppContext()

  useEffect(() => {
    // Wait for both localStorage load AND server sync before deciding to create new session
    if (!sessionsLoaded || !queryServerSyncComplete || !sessionId) return

    let session = sessions.find(s => s.id === sessionId)

    // If session doesn't exist (e.g., page refresh on a new empty session),
    // create it with the URL's ID to preserve the URL
    if (!session) {
      const newSession = createSessionWithId(sessionId)
      setSessions(prev => [...prev, newSession])
      session = newSession
    }

    // Always update currentSessionId
    setCurrentSessionId(sessionId)

    // Only load session data if we haven't loaded this session yet
    if (loadedSessionRef.current !== sessionId) {
      loadedSessionRef.current = sessionId
      setResults(null)

      // Check if there's a pending query from navigation (e.g., from chat "Edit" button)
      if (pendingQueryRef.current !== null) {
        const pendingQuery = pendingQueryRef.current
        setQuery(pendingQuery)
        pendingQueryRef.current = null
        // Auto-run the query
        setTimeout(() => {
          queryEditorRef.current?.run(pendingQuery)
        }, 100)
      } else if (session.history.length > 0) {
        const latestQuery = session.history[0].sql
        setQuery(latestQuery)
        setTimeout(() => {
          queryEditorRef.current?.run(latestQuery)
        }, 100)
      } else {
        setQuery('')
      }
    }
  }, [sessionId, sessionsLoaded, queryServerSyncComplete, sessions, setSessions, setCurrentSessionId, setResults, setQuery, queryEditorRef, pendingQueryRef])

  return <>{children}</>
}

// Redirect components
function QueryRedirect() {
  const navigate = useNavigate()
  const { sessions, setSessions, sessionsLoaded } = useAppContext()

  useEffect(() => {
    if (!sessionsLoaded) return

    // Find the most recent session
    const mostRecent = [...sessions].sort(
      (a, b) => b.updatedAt.getTime() - a.updatedAt.getTime()
    )[0]

    // If most recent session is empty, use it; otherwise create a new one
    if (mostRecent && mostRecent.history.length === 0) {
      navigate(`/query/${mostRecent.id}`, { replace: true })
    } else {
      const newSession = createSession()
      setSessions(prev => [...prev, newSession])
      navigate(`/query/${newSession.id}`, { replace: true })
    }
  }, [sessionsLoaded, sessions, setSessions, navigate])

  return null
}

// Query Editor View
function QueryEditorView() {
  const navigate = useNavigate()
  const {
    sessions,
    setSessions,
    currentSessionId,
    query,
    setQuery,
    results,
    setResults,
    autoRun,
    setAutoRun,
    queryEditorRef,
  } = useAppContext()

  // Fetch catalog for SQL autocomplete (shares cache with Catalog component)
  const { data: catalogData } = useQuery({
    queryKey: ['catalog'],
    queryFn: fetchCatalog,
  })

  // Query mode state - default to 'auto' for new sessions
  const [mode, setMode] = useState<QueryMode>('auto')
  const [activeMode, setActiveMode] = useState<'sql' | 'cypher'>('sql')

  // Visualization recommendation state
  const [isRecommending, setIsRecommending] = useState(false)
  const [recommendedConfig, setRecommendedConfig] = useState<{
    chartType: 'bar' | 'line' | 'pie' | 'area' | 'scatter'
    xAxis: string
    yAxis: string[]
  } | null>(null)

  const currentSession = sessions.find(s => s.id === currentSessionId)
  const generationHistory = currentSession?.history ?? []

  // Detect mode from session's most recent query on mount/session change
  const latestHistoryEntry = generationHistory[0]
  useEffect(() => {
    if (!latestHistoryEntry) {
      // New session with no history - default to auto
      setMode('auto')
      setActiveMode('sql')
      return
    }
    // Use saved queryType if available, otherwise detect from content
    let detectedType = latestHistoryEntry.queryType
    if (!detectedType) {
      const upper = latestHistoryEntry.sql.toUpperCase().trim()
      if (upper.startsWith('MATCH') || upper.includes('MATCH (') || upper.includes('MATCH(')) {
        detectedType = 'cypher'
      } else {
        detectedType = 'sql'
      }
    }
    setMode(detectedType)
    setActiveMode(detectedType)
  }, [currentSessionId, latestHistoryEntry?.sql]) // Run when session changes or first query loads

  const handleUpdateTitle = useCallback((title: string) => {
    setSessions(prev => prev.map(session => {
      if (session.id === currentSessionId) {
        return {
          ...session,
          name: title || undefined,
          updatedAt: new Date(),
        }
      }
      return session
    }))
  }, [currentSessionId, setSessions])

  const conversationHistory: HistoryMessage[] = generationHistory
    .filter(r => r.type === 'generation' && r.prompt && r.thinking)
    .slice(0, 5)
    .reverse()
    .flatMap(r => [
      { role: 'user' as const, content: r.prompt! },
      { role: 'assistant' as const, content: r.thinking! },
    ])

  const addToHistory = useCallback((record: GenerationRecord) => {
    setSessions(prev => prev.map(session => {
      if (session.id === currentSessionId) {
        return {
          ...session,
          updatedAt: new Date(),
          history: [record, ...session.history],
        }
      }
      return session
    }))
  }, [currentSessionId, setSessions])

  const handleSelectTable = (table: TableInfo) => {
    setQuery(`SELECT * FROM ${table.name} LIMIT 100`)
  }

  const handleClear = () => {
    setQuery('')
    setResults(null)
    setRecommendedConfig(null)
  }

  // Handle query results (no auto-visualization)
  const handleResults = useCallback((data: QueryResponse) => {
    setResults(data)
    setRecommendedConfig(null)
  }, [setResults])

  // Manual visualization request
  const handleRequestVisualization = useCallback(async () => {
    if (!results) return

    // Skip recommendation for large datasets or empty results
    const shouldSkip = results.columns.length > 20 || results.row_count > 1000 || results.rows.length === 0
    if (shouldSkip) {
      return
    }

    // Request LLM recommendation
    setIsRecommending(true)
    try {
      const rec = await recommendVisualization({
        columns: results.columns,
        sampleRows: results.rows.slice(0, 20),
        rowCount: results.row_count,
        query: query,
      })

      if (rec.recommended && rec.chartType && rec.xAxis && rec.yAxis) {
        setRecommendedConfig({
          chartType: rec.chartType,
          xAxis: rec.xAxis,
          yAxis: rec.yAxis,
        })
      }
    } catch {
      // Silently fail - recommendation is not critical
    } finally {
      setIsRecommending(false)
    }
  }, [results, query])

  const handleGenerated = (sql: string, shouldRun: boolean) => {
    setQuery(sql)
    if (shouldRun) {
      queryEditorRef.current?.run(sql)
    }
  }

  const handleGenerationComplete = async (record: GenerationRecord) => {
    addToHistory(record)

    // Auto-generate title on first generated query if session has no name
    if (currentSession && !currentSession.name && record.type === 'generation' && record.prompt) {
      // Check if this is the first generation (current history has no generations)
      const hasExistingGenerations = currentSession.history.some(h => h.type === 'generation')
      if (!hasExistingGenerations) {
        try {
          const result = await generateSessionTitle([{ prompt: record.prompt, sql: record.sql }])
          if (result.title) {
            handleUpdateTitle(result.title)
          }
        } catch {
          // Silently fail - title generation is not critical
        }
      }
    }
  }

  const handleManualRun = (record: GenerationRecord) => {
    addToHistory(record)
  }

  const handleRestoreQuery = (sql: string, queryType?: 'sql' | 'cypher') => {
    setQuery(sql)
    // Restore the query type/mode if provided, otherwise detect from content
    let detectedType = queryType
    if (!detectedType) {
      // Heuristic: Cypher uses MATCH, SQL uses SELECT
      const upper = sql.toUpperCase().trim()
      if (upper.startsWith('MATCH') || upper.includes('MATCH (') || upper.includes('MATCH(')) {
        detectedType = 'cypher'
      } else {
        detectedType = 'sql'
      }
    }
    setMode(detectedType)
    setActiveMode(detectedType)
  }

  const handleAskAboutResults = useCallback(() => {
    if (!query || !results) return

    // Build a summary of the results for context
    const resultSummary = results.rows.length > 0
      ? `The query returned ${results.row_count} rows with columns: ${results.columns.join(', ')}.`
      : 'The query returned no results.'

    // Create the question
    const question = `I ran this SQL query:\n\n\`\`\`sql\n${query}\n\`\`\`\n\n${resultSummary}\n\nCan you help me understand these results?`

    // Navigate to chat with the question - SimplifiedChatView handles the rest
    navigate(`/chat?q=${encodeURIComponent(question)}`)
  }, [query, results, navigate])

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      <div className="flex-shrink-0 px-8 pt-6 pb-4">
        <PromptInput
          currentQuery={query}
          conversationHistory={conversationHistory}
          onGenerated={handleGenerated}
          onGenerationComplete={handleGenerationComplete}
          autoRun={autoRun}
          onAutoRunChange={setAutoRun}
          mode={mode}
          onModeDetected={setActiveMode}
        />
      </div>
      <div className="flex-1 overflow-auto px-8 pb-8">
        <div className="flex flex-col gap-5">
          <SessionHistory
            history={generationHistory}
            onRestoreQuery={handleRestoreQuery}
          />
          <Catalog onSelectTable={handleSelectTable} />
          <QueryEditor
            ref={queryEditorRef}
            query={query}
            onQueryChange={setQuery}
            onResults={handleResults}
            onClear={handleClear}
            onManualRun={handleManualRun}
            schema={catalogData?.tables}
            mode={mode}
            onModeChange={setMode}
            activeMode={activeMode}
            onActiveModeChange={setActiveMode}
          />
          <ResultsView
            results={results}
            isRecommending={isRecommending}
            recommendedConfig={recommendedConfig}
            onAskAboutResults={handleAskAboutResults}
            onRequestVisualization={handleRequestVisualization}
          />
        </div>
      </div>
    </div>
  )
}

// Sessions Page Views
function QuerySessionsView() {
  const navigate = useNavigate()
  const { sessions, setSessions, currentSessionId } = useAppContext()

  const handleSelectSession = (session: QuerySession) => {
    navigate(`/query/${session.id}`)
  }

  const handleDeleteSession = (sessionId: string) => {
    setSessions(prev => prev.filter(s => s.id !== sessionId))
    if (sessionId === currentSessionId && sessions.length > 1) {
      const remaining = sessions.filter(s => s.id !== sessionId)
      if (remaining.length > 0) {
        navigate(`/query/${remaining[0].id}`)
      }
    }
  }

  const handleUpdateTitle = (sessionId: string, title: string) => {
    setSessions(prev => prev.map(s =>
      s.id === sessionId ? { ...s, name: title, updatedAt: new Date() } : s
    ))
  }

  const handleGenerateTitle = async (sessionId: string) => {
    const session = sessions.find(s => s.id === sessionId)
    if (!session) return

    // Include both generated queries (with prompts) and manual queries (SQL only)
    const queries: SessionQueryInfo[] = session.history
      .filter(h => h.sql) // Must have SQL
      .map(h => ({ prompt: h.prompt || '', sql: h.sql }))
      .slice(0, 3)

    if (queries.length === 0) return

    const result = await generateSessionTitle(queries)
    if (result.title) {
      setSessions(prev => prev.map(s =>
        s.id === sessionId ? { ...s, name: result.title, updatedAt: new Date() } : s
      ))
    }
  }

  return (
    <SessionsPage
      sessions={sessions}
      currentSessionId={currentSessionId}
      onSelectSession={handleSelectSession}
      onDeleteSession={handleDeleteSession}
      onUpdateSessionTitle={handleUpdateTitle}
      onGenerateTitle={handleGenerateTitle}
    />
  )
}

function ChatSessionsView() {
  const navigate = useNavigate()
  const location = useLocation()

  // Get chat sessions and mutations from React Query
  const { data: chatSessions = [] } = useChatSessions()
  const deleteChatSession = useDeleteChatSession()
  const renameChatSession = useRenameChatSession()
  const generateChatTitle = useGenerateChatTitle()

  // Extract current chat session ID from URL
  const chatMatch = location.pathname.match(/^\/chat\/([^/]+)/)
  const currentChatSessionId = chatMatch?.[1] ?? ''

  const handleSelectChatSession = (session: ChatSession) => {
    navigate(`/chat/${session.id}`)
  }

  const handleDeleteChatSession = (sessionId: string) => {
    deleteChatSession.mutate(sessionId)
  }

  const handleUpdateTitle = (sessionId: string, title: string) => {
    renameChatSession.mutate({ sessionId, name: title })
  }

  const handleGenerateTitle = async (sessionId: string) => {
    await generateChatTitle.mutateAsync(sessionId)
  }

  return (
    <ChatSessionsPage
      sessions={chatSessions}
      currentSessionId={currentChatSessionId}
      onSelectSession={handleSelectChatSession}
      onDeleteSession={handleDeleteChatSession}
      onUpdateSessionTitle={handleUpdateTitle}
      onGenerateTitle={handleGenerateTitle}
    />
  )
}

function AppContent() {
  const navigate = useNavigate()
  const { isAuthenticated, connectionError, retryConnection, isLoading: authLoading } = useAuth()

  // Track previous auth state to detect login
  const wasAuthenticatedRef = useRef(isAuthenticated)

  const [query, setQuery] = useState('')
  const [results, setResults] = useState<QueryResponse | null>(null)
  const [autoRun, setAutoRun] = useState(true)
  const [sessions, setSessions] = useState<QuerySession[]>([])
  const [currentSessionId, setCurrentSessionId] = useState<string>('')
  const [sessionsLoaded, setSessionsLoaded] = useState(false)
  const queryEditorRef = useRef<QueryEditorHandle>(null)
  const pendingQueryRef = useRef<string | null>(null)

  // Mark sessions as ready to load immediately - sync hooks will fetch from server
  useEffect(() => {
    setSessionsLoaded(true)
  }, [])

  // Sync hooks - load sessions from server and provide sync functions
  const [querySyncFns, queryServerSyncComplete] = useQuerySessionSync(
    setSessions,
    sessionsLoaded
  )
  const deleteSessionFromServer = useSessionDelete()

  // Refresh sessions from server when user logs in
  useEffect(() => {
    if (isAuthenticated && !wasAuthenticatedRef.current) {
      // User just logged in - refresh sessions to get their history
      console.log('[Auth] User logged in, refreshing sessions')
      querySyncFns.refreshFromServer()
    }
    wasAuthenticatedRef.current = isAuthenticated
  }, [isAuthenticated, querySyncFns])

  const contextValue: AppContextType = {
    sessions,
    setSessions,
    currentSessionId,
    setCurrentSessionId,
    sessionsLoaded,
    queryServerSyncComplete,
    query,
    setQuery,
    results,
    setResults,
    autoRun,
    setAutoRun,
    queryEditorRef,
    pendingQueryRef,
  }

  // Search spotlight state
  const [isSearchOpen, setIsSearchOpen] = useState(false)

  // Sidebar handlers
  const handleNewQuerySession = () => {
    const newSession = createSession()
    setSessions(prev => [...prev, newSession])
    navigate(`/query/${newSession.id}`)
  }

  const handleSelectQuerySession = (session: QuerySession) => {
    navigate(`/query/${session.id}`)
  }

  const handleDeleteQuerySession = (sessionId: string) => {
    setSessions(prev => prev.filter(s => s.id !== sessionId))
    deleteSessionFromServer(sessionId) // Also delete from server
    if (sessionId === currentSessionId && sessions.length > 1) {
      const remaining = sessions.filter(s => s.id !== sessionId)
      if (remaining.length > 0) {
        navigate(`/query/${remaining[0].id}`)
      }
    }
  }

  const handleNewChatSession = (question?: string) => {
    const queryString = question ? `?q=${encodeURIComponent(question)}` : ''
    navigate(`/chat${queryString}`)
  }

  const handleRenameQuerySession = (sessionId: string, name: string) => {
    setSessions(prev => prev.map(s =>
      s.id === sessionId ? { ...s, name, updatedAt: new Date() } : s
    ))
  }

  const handleGenerateTitleQuerySession = async (sessionId: string) => {
    const session = sessions.find(s => s.id === sessionId)
    if (!session) return

    // Include both generated queries (with prompts) and manual queries (SQL only)
    const queries: SessionQueryInfo[] = session.history
      .filter(h => h.sql) // Must have SQL
      .map(h => ({ prompt: h.prompt || '', sql: h.sql }))
      .slice(0, 3)

    if (queries.length === 0) return

    const result = await generateSessionTitle(queries)
    if (result.title) {
      setSessions(prev => prev.map(s =>
        s.id === sessionId ? { ...s, name: result.title, updatedAt: new Date() } : s
      ))
    }
  }

  // Global keyboard shortcuts and search event
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Only handle if Cmd/Ctrl is pressed
      if (!e.metaKey && !e.ctrlKey) return

      // Cmd+K should always work, even in inputs
      if (e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setIsSearchOpen(true)
        return
      }

      // Ignore other shortcuts if user is typing in an input
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
    }
    const handleOpenSearch = () => setIsSearchOpen(true)
    const handleNewChat = (e: Event) => {
      const question = (e as CustomEvent<{ question?: string }>).detail?.question
      handleNewChatSession(question)
    }
    window.addEventListener('keydown', handleKeyDown)
    window.addEventListener('open-search', handleOpenSearch)
    window.addEventListener('new-chat-session', handleNewChat)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('open-search', handleOpenSearch)
      window.removeEventListener('new-chat-session', handleNewChat)
    }
  }, [handleNewChatSession])

  // Show connection error page if server is unreachable
  // Must be after all hooks to satisfy React's rules of hooks
  if (connectionError) {
    return <ConnectionError onRetry={retryConnection} isRetrying={authLoading} />
  }

  return (
    <AppContext.Provider value={contextValue}>
      <div className="h-dvh flex">
        {/* Sidebar */}
        <Sidebar
          querySessions={sessions}
          currentQuerySessionId={currentSessionId}
          onNewQuerySession={handleNewQuerySession}
          onSelectQuerySession={handleSelectQuerySession}
          onDeleteQuerySession={handleDeleteQuerySession}
          onRenameQuerySession={handleRenameQuerySession}
          onGenerateTitleQuerySession={handleGenerateTitleQuerySession}
        />

        {/* Main content */}
        <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
          <Routes>
            {/* Landing page */}
            <Route path="/" element={<Landing />} />

            {/* Query routes */}
            <Route path="/query" element={<QueryRedirect />} />
            <Route path="/query/:sessionId" element={
              <QuerySessionSync>
                <QueryEditorView />
              </QuerySessionSync>
            } />
            <Route path="/query/sessions" element={<QuerySessionsView />} />

            {/* Chat routes */}
            <Route path="/chat" element={<SimplifiedChatView />} />
            <Route path="/chat/:sessionId" element={<SimplifiedChatView />} />
            <Route path="/chat/sessions" element={<ChatSessionsView />} />

            {/* Topology routes */}
            <Route path="/topology" element={<Navigate to="/topology/map" replace />} />
            <Route path="/topology/map" element={<TopologyPage view="map" />} />
            <Route path="/topology/graph" element={<TopologyPage view="graph" />} />
            <Route path="/topology/path-calculator" element={<PathCalculatorPage />} />
            <Route path="/topology/redundancy" element={<RedundancyReportPage />} />
            <Route path="/topology/metro-connectivity" element={<MetroConnectivityPage />} />
            <Route path="/topology/maintenance" element={<MaintenancePlannerPage />} />

            {/* Performance routes */}
            <Route path="/performance" element={<Navigate to="/performance/dz-vs-internet" replace />} />
            <Route path="/performance/dz-vs-internet" element={<DzVsInternetPage />} />
            <Route path="/performance/path-latency" element={<PathLatencyPage />} />

            {/* Status routes */}
            <Route path="/status" element={<StatusPage />} />
            <Route path="/status/links" element={<StatusPage />} />
            <Route path="/status/devices" element={<StatusPage />} />
            <Route path="/status/metros" element={<StatusPage />} />
            <Route path="/status/methodology" element={<StatusAppendix />} />

            {/* Timeline route */}
            <Route path="/timeline" element={<TimelinePage />} />

            {/* Outages route */}
            <Route path="/outages" element={<OutagesPage />} />

            {/* Stake analytics route */}
            <Route path="/stake" element={<StakePage />} />

            {/* Settings */}
            <Route path="/settings" element={<SettingsPage />} />

            {/* DZ entity routes */}
            <Route path="/dz/devices" element={<DevicesPage />} />
            <Route path="/dz/devices/:pk" element={<DeviceDetailPage />} />
            <Route path="/dz/links" element={<LinksPage />} />
            <Route path="/dz/links/:pk" element={<LinkDetailPage />} />
            <Route path="/dz/metros" element={<MetrosPage />} />
            <Route path="/dz/metros/:pk" element={<MetroDetailPage />} />
            <Route path="/dz/contributors" element={<ContributorsPage />} />
            <Route path="/dz/contributors/:pk" element={<ContributorDetailPage />} />
            <Route path="/dz/users" element={<UsersPage />} />
            <Route path="/dz/users/:pk" element={<UserDetailPage />} />

            {/* Solana entity routes */}
            <Route path="/solana/validators" element={<ValidatorsPage />} />
            <Route path="/solana/validators/:vote_pubkey" element={<ValidatorDetailPage />} />
            <Route path="/solana/gossip-nodes" element={<GossipNodesPage />} />
            <Route path="/solana/gossip-nodes/:pubkey" element={<GossipNodeDetailPage />} />

            {/* Default redirect */}
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>

        {/* Search spotlight */}
        <SearchSpotlight isOpen={isSearchOpen} onClose={() => setIsSearchOpen(false)} />
      </div>
    </AppContext.Provider>
  )
}

// Google Client ID from environment
const GOOGLE_CLIENT_ID = import.meta.env.VITE_GOOGLE_CLIENT_ID as string | undefined

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <WalletProviderWrapper>
        <AuthProvider googleClientId={GOOGLE_CLIENT_ID}>
          <AppContent />
        </AuthProvider>
      </WalletProviderWrapper>
    </QueryClientProvider>
  )
}
