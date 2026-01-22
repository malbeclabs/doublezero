import { useState, useRef, useEffect, useCallback } from 'react'
import { Routes, Route, Navigate, useNavigate, useParams, useLocation, useSearchParams } from 'react-router-dom'
import { useChatSessions, useDeleteChatSession, useRenameChatSession, useGenerateChatTitle } from '@/hooks/use-chat'
import {
  useQuerySessions,
  useQuerySession,
  useDeleteQuerySession,
  useRenameQuerySession,
  useGenerateQueryTitle,
  useCreateQuerySession,
  useAddQueryHistory,
  useUpdateQueryTitle,
} from '@/hooks/use-query-sessions'
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from '@tanstack/react-query'
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
import { generateSessionTitle, recommendVisualization, fetchCatalog } from '@/lib/api'
import type { TableInfo, QueryResponse, HistoryMessage, QueryMode } from '@/lib/api'
import { type QuerySession, type ChatSession } from '@/lib/sessions'
import { formatQueryByType, formatQuery } from '@/lib/format-query'

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

// Redirect to latest or new query session
function QueryRedirect() {
  const navigate = useNavigate()
  const { data: sessions, isLoading, isError } = useQuerySessions()
  const hasNavigatedRef = useRef(false)

  useEffect(() => {
    if (hasNavigatedRef.current) return

    // On error or after loading completes, navigate to appropriate session
    if (isError) {
      // If we can't load sessions, just create a new one
      hasNavigatedRef.current = true
      navigate(`/query/${crypto.randomUUID()}`, { replace: true })
      return
    }

    if (isLoading || !sessions) return
    hasNavigatedRef.current = true

    // Find the most recent session
    const mostRecent = [...sessions].sort(
      (a, b) => b.updatedAt.getTime() - a.updatedAt.getTime()
    )[0]

    // If most recent session is empty, use it; otherwise navigate to a new session ID.
    // QueryEditorView will create the session if it doesn't exist.
    if (mostRecent && mostRecent.history.length === 0) {
      navigate(`/query/${mostRecent.id}`, { replace: true })
    } else {
      navigate(`/query/${crypto.randomUUID()}`, { replace: true })
    }
  }, [isLoading, isError, sessions, navigate])

  return null
}

// Query Editor View
function QueryEditorView() {
  const navigate = useNavigate()
  const { sessionId } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const queryEditorRef = useRef<QueryEditorHandle>(null)

  // Fetch session from React Query
  const { data: session, isLoading: sessionLoading } = useQuerySession(sessionId)
  const createSessionMutation = useCreateQuerySession()
  const addHistoryMutation = useAddQueryHistory()
  const updateTitleMutation = useUpdateQueryTitle()

  // Local editor state
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<QueryResponse | null>(null)
  const [autoRun, setAutoRun] = useState(true)

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

  // Create session if it doesn't exist (e.g., direct URL navigation)
  const sessionCreatedRef = useRef<string | null>(null)
  useEffect(() => {
    if (!sessionId || sessionLoading) return
    if (!session && sessionCreatedRef.current !== sessionId) {
      sessionCreatedRef.current = sessionId
      createSessionMutation.mutate(sessionId)
    }
  }, [sessionId, session, sessionLoading, createSessionMutation])

  // Track pending query to run (from URL param or session history)
  const [pendingRun, setPendingRun] = useState<string | null>(null)

  // Handle URL param immediately on mount/navigation (don't wait for session)
  const urlParamHandledRef = useRef<string | null>(null)
  useEffect(() => {
    if (!sessionId || urlParamHandledRef.current === sessionId) return
    const pendingSql = searchParams.get('sql')
    const pendingCypher = searchParams.get('cypher')
    if (pendingSql) {
      urlParamHandledRef.current = sessionId
      const formatted = formatQueryByType(pendingSql, 'sql')
      setQuery(formatted)
      setResults(null)
      setPendingRun(formatted)
      setMode('sql')
      setActiveMode('sql')
      setSearchParams({}, { replace: true })
    } else if (pendingCypher) {
      urlParamHandledRef.current = sessionId
      const formatted = formatQueryByType(pendingCypher, 'cypher')
      setQuery(formatted)
      setResults(null)
      setPendingRun(formatted)
      setMode('cypher')
      setActiveMode('cypher')
      setSearchParams({}, { replace: true })
    }
  }, [sessionId, searchParams, setSearchParams])

  // Initialize editor from session history (only if no URL param)
  const historyLoadedRef = useRef<string | null>(null)
  useEffect(() => {
    if (!sessionId || sessionLoading || historyLoadedRef.current === sessionId) return
    // Skip if we already handled a URL param for this session
    if (urlParamHandledRef.current === sessionId) return
    historyLoadedRef.current = sessionId
    setResults(null)

    if (session?.history.length) {
      const latestEntry = session.history[0]
      const queryType = latestEntry.queryType ?? 'sql'
      const formatted = formatQueryByType(latestEntry.sql, queryType)
      setQuery(formatted)
      setPendingRun(formatted)
    } else {
      setQuery('')
    }
  }, [sessionId, session, sessionLoading])

  // Run pending query once editor ref is available
  // Use a small interval to check for ref availability since refs don't trigger re-renders
  useEffect(() => {
    if (!pendingRun) return

    const tryRun = () => {
      if (queryEditorRef.current) {
        queryEditorRef.current.run(pendingRun)
        setPendingRun(null)
        return true
      }
      return false
    }

    // Try immediately
    if (tryRun()) return

    // If ref not ready, poll briefly
    const interval = setInterval(() => {
      if (tryRun()) {
        clearInterval(interval)
      }
    }, 50)

    // Clean up after 1 second (editor should definitely be mounted by then)
    const timeout = setTimeout(() => clearInterval(interval), 1000)

    return () => {
      clearInterval(interval)
      clearTimeout(timeout)
    }
  }, [pendingRun])

  const generationHistory = session?.history ?? []

  // Detect mode from session's most recent query on mount/session change
  const latestHistoryEntry = generationHistory[0]
  useEffect(() => {
    // Skip if we already handled a URL param for this session
    if (urlParamHandledRef.current === sessionId) return

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
  }, [sessionId, latestHistoryEntry?.sql])

  const conversationHistory: HistoryMessage[] = generationHistory
    .filter(r => r.type === 'generation' && r.prompt && r.thinking)
    .slice(0, 5)
    .reverse()
    .flatMap(r => [
      { role: 'user' as const, content: r.prompt! },
      { role: 'assistant' as const, content: r.thinking! },
    ])

  const addToHistory = useCallback((record: GenerationRecord) => {
    if (!sessionId) return
    addHistoryMutation.mutate({ sessionId, record })
  }, [sessionId, addHistoryMutation])

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
  }, [])

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
    if (sessionId && session && !session.name && record.type === 'generation' && record.prompt) {
      // Check if this is the first generation (current history has no generations)
      const hasExistingGenerations = session.history.some(h => h.type === 'generation')
      if (!hasExistingGenerations) {
        try {
          const result = await generateSessionTitle([{ prompt: record.prompt, sql: record.sql }])
          if (result.title) {
            updateTitleMutation.mutate({ sessionId, title: result.title })
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
    // Format the query and detect type if not provided
    if (queryType) {
      setQuery(formatQueryByType(sql, queryType))
      setMode(queryType)
      setActiveMode(queryType)
    } else {
      const { formatted, language } = formatQuery(sql)
      setQuery(formatted)
      setMode(language)
      setActiveMode(language)
    }
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
  const location = useLocation()

  // Get query sessions and mutations from React Query
  const { data: sessions = [] } = useQuerySessions()
  const deleteQuerySession = useDeleteQuerySession()
  const renameQuerySession = useRenameQuerySession()
  const generateQueryTitle = useGenerateQueryTitle()

  // Extract current session ID from URL
  const queryMatch = location.pathname.match(/^\/query\/([^/]+)/)
  const currentSessionId = queryMatch?.[1] ?? ''

  const handleSelectSession = (session: QuerySession) => {
    navigate(`/query/${session.id}`)
  }

  const handleDeleteSession = (sessionId: string) => {
    deleteQuerySession.mutate(sessionId)
  }

  const handleUpdateTitle = (sessionId: string, title: string) => {
    renameQuerySession.mutate({ sessionId, name: title })
  }

  const handleGenerateTitle = async (sessionId: string) => {
    await generateQueryTitle.mutateAsync(sessionId)
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
  const { connectionError, retryConnection, isLoading: authLoading } = useAuth()

  // Search spotlight state
  const [isSearchOpen, setIsSearchOpen] = useState(false)

  const handleNewChatSession = useCallback((question?: string) => {
    const queryString = question ? `?q=${encodeURIComponent(question)}` : ''
    navigate(`/chat${queryString}`)
  }, [navigate])

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
    <div className="h-dvh flex">
      {/* Sidebar */}
      <Sidebar />

      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        <Routes>
          {/* Landing page */}
          <Route path="/" element={<Landing />} />

          {/* Query routes */}
          <Route path="/query" element={<QueryRedirect />} />
          <Route path="/query/:sessionId" element={<QueryEditorView />} />
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
  )
}

// Google Client ID from environment
const GOOGLE_CLIENT_ID = import.meta.env.VITE_GOOGLE_CLIENT_ID as string | undefined

// Wrapper that provides auth callbacks with access to navigation and query client
function AuthWrapper({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const qc = useQueryClient()

  const handleLoginSuccess = useCallback(() => {
    // Invalidate all queries to refetch with new user's credentials
    qc.invalidateQueries()
    // If on a session page, navigate away (user may have been on an anonymous session)
    if (/^\/(chat|query)\/[^/]+/.test(location.pathname)) {
      const base = location.pathname.startsWith('/query') ? '/query' : '/chat'
      navigate(base, { replace: true })
    }
  }, [qc, navigate, location.pathname])

  const handleLogoutSuccess = useCallback(() => {
    // Clear all cached data (chat sessions, query sessions, etc.)
    qc.clear()
    // If on a session page, navigate away
    if (/^\/(chat|query)\/[^/]+/.test(location.pathname)) {
      const base = location.pathname.startsWith('/query') ? '/query' : '/chat'
      navigate(base, { replace: true })
    }
  }, [navigate, qc, location.pathname])

  return (
    <AuthProvider
      googleClientId={GOOGLE_CLIENT_ID}
      onLoginSuccess={handleLoginSuccess}
      onLogoutSuccess={handleLogoutSuccess}
    >
      {children}
    </AuthProvider>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <WalletProviderWrapper>
        <AuthWrapper>
          <AppContent />
        </AuthWrapper>
      </WalletProviderWrapper>
    </QueryClientProvider>
  )
}
