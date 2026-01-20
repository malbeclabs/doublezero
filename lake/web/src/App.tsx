import { useState, useRef, useEffect, useCallback, createContext, useContext } from 'react'
import { Routes, Route, Navigate, useNavigate, useParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { format as formatSQL } from 'sql-formatter'
import { Catalog } from '@/components/catalog'
import { PromptInput } from '@/components/prompt-input'
import { QueryEditor, type QueryEditorHandle } from '@/components/query-editor'
import { ResultsView } from '@/components/results-view'
import { SessionHistory, type GenerationRecord } from '@/components/session-history'
import { SessionsPage } from '@/components/sessions-page'
import { ChatSessionsPage } from '@/components/chat-sessions-page'
import { Chat, ChatSkeleton } from '@/components/chat'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'
import type { ProcessingStep } from '@/lib/api'
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
import { generateSessionTitle, generateChatSessionTitle, sendChatMessageStream, recommendVisualization, fetchCatalog, acquireSessionLock, releaseSessionLock, watchSessionLock, getSession, generateMessageId, getRunningWorkflowForSession, reconnectToWorkflow, type SessionQueryInfo, type SessionLock } from '@/lib/api'
import type { TableInfo, QueryResponse, HistoryMessage, ChatMessage, QueryMode } from '@/lib/api'
import {
  type QuerySession,
  type ChatSession,
  loadSessions,
  saveSessions,
  createSession,
  createSessionWithId,
  loadChatSessions,
  saveChatSessions,
  createChatSession,
  createChatSessionWithId,
  BROWSER_ID,
} from '@/lib/sessions'
import {
  useQuerySessionSync,
  useChatSessionSync,
  useSessionDelete,
  migrateLocalSessions,
  findIncompleteMessage,
} from '@/lib/sync'
import type { ChatResponse } from '@/lib/api'

// Build processing steps from server response data (source of truth)
// Prefers unified steps array which preserves execution order
function buildProcessingSteps(data: ChatResponse): ProcessingStep[] {
  // If server provides unified steps, use them (preserves interleaved order)
  if (data.steps && data.steps.length > 0) {
    return data.steps.map(step => {
      if (step.type === 'thinking') {
        return { type: 'thinking' as const, content: step.content ?? '' }
      } else {
        return {
          type: 'query' as const,
          question: step.question ?? '',
          sql: step.sql ?? '',
          status: step.status ?? 'completed',
          columns: step.columns,
          data: step.rows,
          rows: step.count,
          error: step.error,
        }
      }
    })
  }

  // Fallback: build from legacy separate arrays (order not preserved)
  const steps: ProcessingStep[] = []

  if (data.thinking_steps) {
    for (const content of data.thinking_steps) {
      steps.push({ type: 'thinking', content })
    }
  }

  if (data.executedQueries) {
    for (const q of data.executedQueries) {
      steps.push({
        type: 'query',
        question: q.question,
        sql: q.sql,
        status: q.error ? 'error' : 'completed',
        columns: q.columns,
        data: q.rows,
        rows: q.count,
        error: q.error,
      })
    }
  }

  return steps
}

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

// Per-session pending state for concurrent chat requests
interface PendingChatState {
  processingSteps: ProcessingStep[]
  abortController: AbortController
  workflowId?: string // Workflow ID for durable persistence
}

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
  // Chat state
  chatSessions: ChatSession[]
  setChatSessions: React.Dispatch<React.SetStateAction<ChatSession[]>>
  currentChatSessionId: string
  setCurrentChatSessionId: React.Dispatch<React.SetStateAction<string>>
  chatSessionsLoaded: boolean
  chatServerSyncComplete: boolean
  // Chat mutation state (lifted to persist across navigation) - now per-session
  pendingChats: Map<string, PendingChatState>
  setPendingChats: React.Dispatch<React.SetStateAction<Map<string, PendingChatState>>>
  sendChatMessage: (sessionId: string, message: string, history: ChatMessage[], skipLock?: boolean) => void
  abortChatMessage: (sessionId: string) => void
  // External locks - locks held by other browsers
  externalLocks: Map<string, SessionLock>
  setExternalLocks: React.Dispatch<React.SetStateAction<Map<string, SessionLock>>>
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

// Stable component for syncing chat session from URL
function ChatSessionSync({ children }: { children: React.ReactNode }) {
  const { sessionId } = useParams()
  const {
    chatSessions,
    setChatSessions,
    chatSessionsLoaded,
    chatServerSyncComplete,
    setCurrentChatSessionId,
  } = useAppContext()

  useEffect(() => {
    // Wait for both localStorage load AND server sync before deciding to create new session
    if (!chatSessionsLoaded || !chatServerSyncComplete || !sessionId) return

    const session = chatSessions.find(s => s.id === sessionId)

    // If session doesn't exist locally, try to fetch it from server
    // This handles sessions created externally (e.g., from Slack)
    if (!session) {
      getSession<ChatMessage[]>(sessionId)
        .then(serverSession => {
          if (serverSession && serverSession.content) {
            // Session exists on server - add it to local state
            const newSession: ChatSession = {
              id: serverSession.id,
              name: serverSession.name ?? undefined,
              createdAt: new Date(serverSession.created_at),
              updatedAt: new Date(serverSession.updated_at),
              messages: serverSession.content,
            }
            setChatSessions(prev => [...prev, newSession])
          } else {
            // Session doesn't exist on server either - create empty one
            const newSession = createChatSessionWithId(sessionId)
            setChatSessions(prev => [...prev, newSession])
          }
        })
        .catch(() => {
          // Failed to fetch - create empty session as fallback
          const newSession = createChatSessionWithId(sessionId)
          setChatSessions(prev => [...prev, newSession])
        })
    }

    setCurrentChatSessionId(sessionId)
  }, [sessionId, chatSessionsLoaded, chatServerSyncComplete, chatSessions, setChatSessions, setCurrentChatSessionId])

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

function ChatRedirect() {
  const navigate = useNavigate()
  const { setChatSessions, chatSessionsLoaded } = useAppContext()

  useEffect(() => {
    if (!chatSessionsLoaded) return

    // Always create a new chat session
    const newSession = createChatSession()
    setChatSessions(prev => [...prev, newSession])
    navigate(`/chat/${newSession.id}`, { replace: true })
  }, [chatSessionsLoaded, setChatSessions, navigate])

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
    chatSessions,
    setChatSessions,
    sendChatMessage,
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

    // Find or create a chat session
    const emptySession = chatSessions.find(s => s.messages.length === 0)

    if (emptySession) {
      // Use existing empty session, add user message, then send
      setChatSessions(prev => prev.map(s =>
        s.id === emptySession.id
          ? { ...s, messages: [{ id: generateMessageId(), role: 'user' as const, content: question }], updatedAt: new Date() }
          : s
      ))
      sendChatMessage(emptySession.id, question, [])
      navigate(`/chat/${emptySession.id}`)
    } else {
      // Create a new session
      const newSession = createChatSession()
      setChatSessions(prev => [...prev, { ...newSession, messages: [{ id: generateMessageId(), role: 'user' as const, content: question }] }])
      sendChatMessage(newSession.id, question, [])
      navigate(`/chat/${newSession.id}`)
    }
  }, [query, results, chatSessions, setChatSessions, sendChatMessage, navigate])

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

// Chat View
function ChatView() {
  const navigate = useNavigate()
  const { sessionId: urlSessionId } = useParams()
  const {
    sessions,
    setSessions,
    chatSessions,
    setChatSessions,
    currentChatSessionId,
    chatSessionsLoaded,
    chatServerSyncComplete,
    pendingChats,
    setPendingChats,
    sendChatMessage,
    abortChatMessage,
    pendingQueryRef,
    externalLocks,
    setExternalLocks,
  } = useAppContext()

  const currentChatSession = chatSessions.find(s => s.id === currentChatSessionId)

  // Show skeleton while loading (delayed to avoid flash for fast loads)
  // Also consider loading if:
  // - Initial sync isn't complete
  // - URL has a session ID but it hasn't been synced to state yet (ChatSessionSync is still processing)
  // - Session ID is set but session hasn't been found/fetched yet
  const sessionNotReady = !!(urlSessionId && (urlSessionId !== currentChatSessionId || !currentChatSession))
  const isLoading = !chatSessionsLoaded || !chatServerSyncComplete || sessionNotReady
  const showSkeleton = useDelayedLoading(isLoading)
  const chatMessages = currentChatSession?.messages ?? []
  const pendingState = pendingChats.get(currentChatSessionId)
  const isPending = !!pendingState
  const currentProcessingSteps = pendingState?.processingSteps ?? []

  // Guard to prevent double-sends from rapid clicks (React state updates are async)
  const sendingRef = useRef(false)

  // Auto-resume incomplete streaming messages on page load
  // Wait for server sync to complete before checking - otherwise we might miss streaming messages
  const resumeAttempted = useRef<string | null>(null)
  useEffect(() => {
    // Debug logging
    console.log('[Chat] Auto-resume check:', {
      chatServerSyncComplete,
      hasSession: !!currentChatSession,
      isPending,
      resumeAttempted: resumeAttempted.current,
      currentChatSessionId,
      messageCount: chatMessages.length,
      hasStreaming: chatMessages.some(m => m.status === 'streaming'),
    })

    // Must wait for server sync to have the latest data
    if (!chatServerSyncComplete) return
    if (!currentChatSession || isPending) return
    if (resumeAttempted.current === currentChatSessionId) return

    // Check for incomplete streaming message
    const incomplete = findIncompleteMessage(chatMessages)
    console.log('[Chat] findIncompleteMessage result:', incomplete)

    if (incomplete) {
      resumeAttempted.current = currentChatSessionId
      console.log('[Chat] Resuming incomplete streaming message for session:', currentChatSessionId)

      // Query the server for the latest workflow for this session
      getRunningWorkflowForSession(currentChatSessionId).then(workflow => {
        if (!workflow) {
          // No workflow found at all - this shouldn't happen normally
          // Fall back to re-sending (legacy behavior)
          console.log('[Chat] No workflow found for session, falling back to re-send')
          const historyUpToIncomplete = chatMessages.slice(0, chatMessages.indexOf(incomplete.userMessage))
          sendChatMessage(currentChatSessionId, incomplete.userMessage.content, historyUpToIncomplete)
          return
        }

        console.log('[Chat] Found workflow on server:', workflow.id, 'status:', workflow.status)

        // Handle based on workflow status
        if (workflow.status === 'completed') {
          // Workflow already completed - use the result directly
          console.log('[Chat] Workflow already completed, using stored result')

          // Build processing steps from workflow data
          const processingSteps: ProcessingStep[] = []
          if (workflow.steps) {
            for (const step of workflow.steps as Array<{ type: string; content?: string; question?: string; sql?: string; error?: string; count?: number }>) {
              if (step.type === 'thinking' && step.content) {
                processingSteps.push({ type: 'thinking', content: step.content })
              } else if (step.type === 'query') {
                processingSteps.push({
                  type: 'query',
                  question: step.question || '',
                  sql: step.sql || '',
                  status: step.error ? 'error' : 'completed',
                  rows: step.count,
                  error: step.error || undefined,
                })
              }
            }
          }

          // Replace streaming message with complete message
          setChatSessions(prev => prev.map(s => {
            if (s.id !== currentChatSessionId) return s
            const newMessages = s.messages.filter(m => m.status !== 'streaming')
            newMessages.push({
              id: generateMessageId(),
              role: 'assistant',
              content: workflow.final_answer || '',
              workflowData: {
                dataQuestions: [],
                generatedQueries: [],
                executedQueries: [],
                followUpQuestions: undefined,
                processingSteps,
              },
              executedQueries: [],
              status: 'complete',
            })
            return { ...s, messages: newMessages, updatedAt: new Date() }
          }))
          return
        }

        if (workflow.status === 'failed' || workflow.status === 'cancelled') {
          // Workflow failed or was cancelled - show error
          console.log('[Chat] Workflow failed/cancelled:', workflow.error)
          setChatSessions(prev => prev.map(s => {
            if (s.id !== currentChatSessionId) return s
            const newMessages = s.messages.filter(m => m.status !== 'streaming')
            newMessages.push({
              id: generateMessageId(),
              role: 'assistant',
              content: workflow.error || 'Workflow failed',
              status: 'error',
            })
            return { ...s, messages: newMessages, updatedAt: new Date() }
          }))
          return
        }

        // Workflow is still running - reconnect to the stream
        console.log('[Chat] Workflow is running, reconnecting to stream')
        const abortController = new AbortController()
        setPendingChats(prev => {
          const next = new Map(prev)
          next.set(currentChatSessionId, {
            processingSteps: [],
            abortController,
            workflowId: workflow.id,
          })
          return next
        })

        // Reconnect to the workflow stream
        reconnectToWorkflow(
          workflow.id,
          {
            onThinking: (data) => {
              const step: ProcessingStep = { type: 'thinking', content: data.content }
              setPendingChats(prev => {
                const existing = prev.get(currentChatSessionId)
                if (!existing) return prev
                const next = new Map(prev)
                next.set(currentChatSessionId, {
                  ...existing,
                  processingSteps: [...existing.processingSteps, step],
                })
                return next
              })
            },
            onQueryDone: (data) => {
              const step: ProcessingStep = {
                type: 'query',
                question: data.question,
                sql: data.sql,
                status: data.error ? 'error' : 'completed',
                rows: data.rows,
                error: data.error || undefined,
              }
              setPendingChats(prev => {
                const existing = prev.get(currentChatSessionId)
                if (!existing) return prev
                const next = new Map(prev)
                next.set(currentChatSessionId, {
                  ...existing,
                  processingSteps: [...existing.processingSteps, step],
                })
                return next
              })
            },
            onDone: (data) => {
              console.log('[Chat] Workflow reconnect onDone', { workflowId: workflow.id })
              // Build processing steps from server data (source of truth)
              const processingSteps = buildProcessingSteps(data)
              console.log('[Chat] Workflow reconnect processingSteps from server:', processingSteps.length)

              // Replace streaming message with complete message
              setChatSessions(prev => prev.map(s => {
                if (s.id !== currentChatSessionId) return s
                const newMessages = s.messages.filter(m => m.status !== 'streaming')
                newMessages.push({
                  id: generateMessageId(),
                  role: 'assistant',
                  content: data.answer,
                  workflowData: {
                    dataQuestions: data.dataQuestions ?? [],
                    generatedQueries: data.generatedQueries ?? [],
                    executedQueries: data.executedQueries ?? [],
                    followUpQuestions: data.followUpQuestions,
                    processingSteps,
                  },
                  executedQueries: data.executedQueries?.map(q => q.sql) ?? [],
                  status: 'complete',
                })
                return { ...s, messages: newMessages, updatedAt: new Date() }
              }))
              setPendingChats(prev => {
                const next = new Map(prev)
                next.delete(currentChatSessionId)
                return next
              })
            },
            onError: (error) => {
              console.log('[Chat] Workflow reconnect error:', error)
              // Replace streaming message with error
              setChatSessions(prev => prev.map(s => {
                if (s.id !== currentChatSessionId) return s
                const newMessages = s.messages.filter(m => m.status !== 'streaming')
                newMessages.push({
                  id: generateMessageId(),
                  role: 'assistant',
                  content: error,
                  status: 'error',
                })
                return { ...s, messages: newMessages, updatedAt: new Date() }
              }))
              setPendingChats(prev => {
                const next = new Map(prev)
                next.delete(currentChatSessionId)
                return next
              })
            },
            onRetry: () => {
              // Workflow is running on another server - poll for completion
              console.log('[Chat] Workflow running on another server, polling for completion')
              const pollForCompletion = async () => {
                for (let i = 0; i < 60; i++) { // Poll for up to 2 minutes
                  await new Promise(r => setTimeout(r, 2000))
                  try {
                    const updatedWorkflow = await getRunningWorkflowForSession(currentChatSessionId)
                    if (!updatedWorkflow) continue
                    if (updatedWorkflow.status === 'completed' && updatedWorkflow.final_answer) {
                      console.log('[Chat] Workflow completed, updating UI')
                      setChatSessions(prev => prev.map(s => {
                        if (s.id !== currentChatSessionId) return s
                        const newMessages = s.messages.filter(m => m.status !== 'streaming')
                        newMessages.push({
                          id: generateMessageId(),
                          role: 'assistant',
                          content: updatedWorkflow.final_answer || '',
                          status: 'complete',
                        })
                        return { ...s, messages: newMessages, updatedAt: new Date() }
                      }))
                      setPendingChats(prev => {
                        const next = new Map(prev)
                        next.delete(currentChatSessionId)
                        return next
                      })
                      return
                    }
                    if (updatedWorkflow.status === 'failed') {
                      console.log('[Chat] Workflow failed')
                      setChatSessions(prev => prev.map(s => {
                        if (s.id !== currentChatSessionId) return s
                        const newMessages = s.messages.filter(m => m.status !== 'streaming')
                        newMessages.push({
                          id: generateMessageId(),
                          role: 'assistant',
                          content: updatedWorkflow.error || 'Workflow failed',
                          status: 'error',
                        })
                        return { ...s, messages: newMessages, updatedAt: new Date() }
                      }))
                      setPendingChats(prev => {
                        const next = new Map(prev)
                        next.delete(currentChatSessionId)
                        return next
                      })
                      return
                    }
                  } catch (e) {
                    console.log('[Chat] Poll error, continuing', e)
                  }
                }
                console.log('[Chat] Polling timed out')
              }
              pollForCompletion()
            },
          },
          abortController.signal
        )
      }).catch((err) => {
        console.log('[Chat] Auto-resume: failed to check for workflow', err)
        // Fall back to re-sending
        const historyUpToIncomplete = chatMessages.slice(0, chatMessages.indexOf(incomplete.userMessage))
        sendChatMessage(currentChatSessionId, incomplete.userMessage.content, historyUpToIncomplete)
      })
    }
  }, [chatServerSyncComplete, currentChatSession, currentChatSessionId, chatMessages, isPending, sendChatMessage, setExternalLocks, setChatSessions, setPendingChats])

  // Check for initial question from landing page
  const initialQuestionSent = useRef<string | null>(null)
  useEffect(() => {
    // Wait for server sync to complete - ChatSessionSync won't set currentChatSessionId until then
    if (!chatServerSyncComplete) return
    // Wait for session to be ready
    if (!currentChatSession) return
    // Don't send twice for the same session
    if (initialQuestionSent.current === currentChatSessionId) return

    const initialQuestion = sessionStorage.getItem('initialChatQuestion')
    if (initialQuestion && chatMessages.length === 0 && !isPending) {
      sessionStorage.removeItem('initialChatQuestion')
      initialQuestionSent.current = currentChatSessionId
      // Add user message immediately
      setChatSessions(prev => prev.map(session => {
        if (session.id === currentChatSessionId) {
          return {
            ...session,
            updatedAt: new Date(),
            messages: [...session.messages, { id: generateMessageId(), role: 'user' as const, content: initialQuestion }],
          }
        }
        return session
      }))
      // Send to API
      sendChatMessage(currentChatSessionId, initialQuestion, [])
    }
  }, [chatServerSyncComplete, currentChatSession, currentChatSessionId, chatMessages.length, isPending, setChatSessions, sendChatMessage])

  const handleSendMessage = useCallback(async (message: string) => {
    // Synchronous guard to prevent double-sends from rapid clicks
    if (sendingRef.current) {
      console.log('[Chat] handleSendMessage blocked - already sending')
      return
    }
    sendingRef.current = true

    try {
      console.log('[Chat] handleSendMessage called:', { message: message.slice(0, 50), currentChatSessionId })
      // Try to acquire lock FIRST before saving streaming message
      // This prevents race conditions where another browser sees the streaming message
      // but the lock hasn't been acquired yet
      const lockResult = await acquireSessionLock(currentChatSessionId, BROWSER_ID, 300, message).catch(() => null)
      if (lockResult && !lockResult.acquired) {
        // Another browser has the lock - show indicator and don't proceed
        console.log('[Lock] Cannot send - session locked by another browser:', lockResult.lock)
        if (lockResult.lock) {
          setExternalLocks(prev => {
            const next = new Map(prev)
            next.set(currentChatSessionId, lockResult.lock!)
            return next
          })
        }
        return
      }

      // Check if there's already a streaming message (safety check for double-sends)
      const currentSession = chatSessions.find(s => s.id === currentChatSessionId)
      if (currentSession?.messages.some(m => m.status === 'streaming')) {
        console.log('[Chat] handleSendMessage blocked - session already has streaming message')
        return
      }

      // Create updated session with streaming message
      const userMessageId = generateMessageId()
      const streamingMessageId = generateMessageId()
      const updatedSessions = chatSessions.map(session => {
        if (session.id === currentChatSessionId) {
          return {
            ...session,
            updatedAt: new Date(),
            messages: [
              ...session.messages,
              { id: userMessageId, role: 'user' as const, content: message },
              { id: streamingMessageId, role: 'assistant' as const, content: '', status: 'streaming' as const },
            ],
          }
        }
        return session
      })

      // Save to localStorage SYNCHRONOUSLY before React state update
      // This ensures the streaming message is persisted even if user reloads immediately
      saveChatSessions(updatedSessions.filter(s => s.messages.length > 0))

      // Update React state
      setChatSessions(updatedSessions)

      // Send to API - lock is already acquired, so pass skipLock flag
      sendChatMessage(currentChatSessionId, message, chatMessages, true)
    } finally {
      // Reset guard after a short delay to allow state to update
      // This prevents blocking legitimate new sends after the stream completes
      setTimeout(() => {
        sendingRef.current = false
      }, 100)
    }
  }, [currentChatSessionId, chatSessions, setChatSessions, sendChatMessage, chatMessages, setExternalLocks])

  const handleOpenInQueryEditor = useCallback((sql: string) => {
    // Format the SQL for better readability
    let formattedSQL = sql
    try {
      formattedSQL = formatSQL(sql, {
        language: 'sql',
        tabWidth: 2,
        keywordCase: 'upper',
      })
    } catch {
      // If formatting fails, use original
    }

    // Store the SQL to be loaded when the query editor syncs
    pendingQueryRef.current = formattedSQL

    // Find or create a query session to use
    const emptySession = sessions.find(s => s.history.length === 0)

    if (emptySession) {
      // Use existing empty session
      navigate(`/query/${emptySession.id}`)
    } else {
      // Create a new session
      const newSession = createSession()
      setSessions(prev => [...prev, newSession])
      navigate(`/query/${newSession.id}`)
    }
  }, [sessions, setSessions, pendingQueryRef, navigate])

  const handleAbort = useCallback(() => {
    abortChatMessage(currentChatSessionId)
  }, [abortChatMessage, currentChatSessionId])

  const handleRetry = useCallback(() => {
    // Find the last user message to retry
    const session = chatSessions.find(s => s.id === currentChatSessionId)
    if (!session) return

    // Find the last user message (should be right before the error message)
    const messages = session.messages
    let lastUserMessage: string | null = null
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'user') {
        lastUserMessage = messages[i].content
        break
      }
    }

    if (!lastUserMessage) return

    // Remove the error message and re-add streaming message
    const messagesWithoutError = messages.filter(m => m.status !== 'error')

    // Build history from messages before the last user message
    const history = messagesWithoutError
      .slice(0, -1) // Remove the last user message for history
      .filter(m => m.status !== 'streaming')

    // Update session state with streaming message
    setChatSessions(prev => prev.map(s => {
      if (s.id !== currentChatSessionId) return s
      return {
        ...s,
        messages: [
          ...messagesWithoutError,
          { id: generateMessageId(), role: 'assistant' as const, content: '', status: 'streaming' as const },
        ],
      }
    }))

    // Resend the message
    sendChatMessage(currentChatSessionId, lastUserMessage, history)
  }, [currentChatSessionId, chatSessions, setChatSessions, sendChatMessage])

  // Get external lock for this session (if any)
  const currentExternalLock = externalLocks.get(currentChatSessionId)

  // Show skeleton while loading (with delay to avoid flash)
  if (isLoading && showSkeleton) {
    return <ChatSkeleton />
  }
  // Still loading but delay hasn't passed - show nothing briefly
  if (isLoading) {
    return null
  }

  return (
    <Chat
      messages={chatMessages}
      isPending={isPending}
      processingSteps={currentProcessingSteps}
      externalLock={currentExternalLock ? { question: currentExternalLock.question } : null}
      onSendMessage={handleSendMessage}
      onAbort={handleAbort}
      onRetry={handleRetry}
      onOpenInQueryEditor={handleOpenInQueryEditor}
    />
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
  const { chatSessions, setChatSessions, currentChatSessionId } = useAppContext()

  const handleSelectChatSession = (session: ChatSession) => {
    navigate(`/chat/${session.id}`)
  }

  const handleDeleteChatSession = (sessionId: string) => {
    setChatSessions(prev => prev.filter(s => s.id !== sessionId))
    if (sessionId === currentChatSessionId && chatSessions.length > 1) {
      const remaining = chatSessions.filter(s => s.id !== sessionId)
      if (remaining.length > 0) {
        navigate(`/chat/${remaining[0].id}`)
      }
    }
  }

  const handleUpdateTitle = (sessionId: string, title: string) => {
    setChatSessions(prev => prev.map(s =>
      s.id === sessionId ? { ...s, name: title, updatedAt: new Date() } : s
    ))
  }

  const handleGenerateTitle = async (sessionId: string) => {
    const session = chatSessions.find(s => s.id === sessionId)
    if (!session || session.messages.length === 0) return

    const result = await generateChatSessionTitle(session.messages)
    if (result.title) {
      setChatSessions(prev => prev.map(s =>
        s.id === sessionId ? { ...s, name: result.title, updatedAt: new Date() } : s
      ))
    }
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

  const [query, setQuery] = useState('')
  const [results, setResults] = useState<QueryResponse | null>(null)
  const [autoRun, setAutoRun] = useState(true)
  const [sessions, setSessions] = useState<QuerySession[]>([])
  const [currentSessionId, setCurrentSessionId] = useState<string>('')
  const [chatSessions, setChatSessions] = useState<ChatSession[]>([])
  const [currentChatSessionId, setCurrentChatSessionId] = useState<string>('')
  const [sessionsLoaded, setSessionsLoaded] = useState(false)
  const [chatSessionsLoaded, setChatSessionsLoaded] = useState(false)
  const queryEditorRef = useRef<QueryEditorHandle>(null)
  const pendingQueryRef = useRef<string | null>(null)

  // Chat mutation state (lifted to persist across navigation) - now per-session
  const [pendingChats, setPendingChats] = useState<Map<string, PendingChatState>>(new Map())


  // External locks - tracks locks held by other browsers on sessions
  const [externalLocks, setExternalLocks] = useState<Map<string, SessionLock>>(new Map())

  // Watch for lock status changes via SSE on the current chat session
  useEffect(() => {
    if (!currentChatSessionId) return
    // Don't watch if we're the ones processing
    if (pendingChats.has(currentChatSessionId)) return

    const cleanup = watchSessionLock(
      currentChatSessionId,
      BROWSER_ID,
      {
        onLocked: (lock) => {
          console.log('[Lock] SSE: Session locked by another browser:', lock)
          setExternalLocks(prev => {
            const next = new Map(prev)
            next.set(currentChatSessionId, lock)
            return next
          })
        },
        onUnlocked: async () => {
          console.log('[Lock] SSE: Session unlocked')
          setExternalLocks(prev => {
            const next = new Map(prev)
            next.delete(currentChatSessionId)
            return next
          })

          // Fetch the latest session data from server to see new messages
          try {
            const serverSession = await getSession<ChatMessage[]>(currentChatSessionId)
            if (serverSession && serverSession.content) {
              // Filter out any streaming messages - the other browser completed, so any
              // streaming message in server data is stale (race condition with save)
              const completedMessages = serverSession.content.filter(m => m.status !== 'streaming')
              setChatSessions(prev => prev.map(s =>
                s.id === currentChatSessionId
                  ? { ...s, messages: completedMessages, name: serverSession.name || s.name, updatedAt: new Date(serverSession.updated_at) }
                  : s
              ))
              console.log('[Lock] SSE: Updated session with', completedMessages.length, 'messages (filtered from', serverSession.content.length, ')')
            }
          } catch (err) {
            console.log('[Lock] SSE: Failed to fetch updated session:', err)
          }
        },
        onError: (err) => {
          console.log('[Lock] SSE error:', err.message)
        },
      }
    )

    return cleanup
  }, [currentChatSessionId, pendingChats])

  // Load query sessions from localStorage on mount
  useEffect(() => {
    const savedSessions = loadSessions()
    if (savedSessions.length > 0) {
      setSessions(savedSessions)
      // Don't set currentSessionId here - let the route sync handle it
    } else {
      const newSession = createSession()
      setSessions([newSession])
    }
    setSessionsLoaded(true)
  }, [])

  // Load chat sessions from localStorage on mount
  useEffect(() => {
    const savedChatSessions = loadChatSessions()
    if (savedChatSessions.length > 0) {
      setChatSessions(savedChatSessions)
      // Don't set currentChatSessionId here - let the route sync handle it
    } else {
      const newChatSession = createChatSession()
      setChatSessions([newChatSession])
    }
    setChatSessionsLoaded(true)
  }, [])

  // Sync hooks for server persistence
  // Note: sync functions are called only when sessions are actually modified,
  // not on every state change, to avoid updating timestamps for unchanged sessions
  const [_syncQuerySession, queryServerSyncComplete] = useQuerySessionSync(
    setSessions,
    sessionsLoaded
  )
  const [chatSyncFns, chatServerSyncComplete] = useChatSessionSync(
    setChatSessions,
    chatSessionsLoaded
  )
  const deleteSessionFromServer = useSessionDelete()

  // Run one-time migration of localStorage sessions to server
  useEffect(() => {
    if (sessionsLoaded && chatSessionsLoaded) {
      migrateLocalSessions(sessions, chatSessions).catch(console.error)
    }
  }, [sessionsLoaded, chatSessionsLoaded]) // Only run once when both are loaded

  // Save sessions to localStorage when they change
  // Note: Server sync is handled separately when sessions are explicitly modified
  // to avoid updating all sessions' updated_at timestamps on every state change
  useEffect(() => {
    if (sessionsLoaded) {
      const nonEmptySessions = sessions.filter(s => s.history.length > 0)
      saveSessions(nonEmptySessions)
    }
  }, [sessions, sessionsLoaded])

  // Save chat sessions to localStorage when they change
  // Note: Server sync is handled separately when sessions are explicitly modified
  // to avoid updating all sessions' updated_at timestamps on every state change
  useEffect(() => {
    if (chatSessionsLoaded) {
      const nonEmptyChatSessions = chatSessions.filter(s => s.messages.length > 0)
      saveChatSessions(nonEmptyChatSessions)
    }
  }, [chatSessions, chatSessionsLoaded])

  // Helper to add a processing step to a session's pending state
  const addProcessingStep = useCallback((sessionId: string, step: ProcessingStep) => {
    setPendingChats(prev => {
      const existing = prev.get(sessionId)
      if (!existing) return prev
      const next = new Map(prev)
      next.set(sessionId, {
        ...existing,
        processingSteps: [...existing.processingSteps, step],
      })
      return next
    })
  }, [])

  // Helper to update a query step (e.g., mark as completed)
  // Only updates the first matching query with status 'running' to avoid
  // overwriting already-completed or errored queries with the same question text
  const updateQueryStep = useCallback((
    sessionId: string,
    question: string,
    update: { status: 'completed' | 'error'; rows?: number; error?: string }
  ) => {
    setPendingChats(prev => {
      const existing = prev.get(sessionId)
      if (!existing) return prev
      const next = new Map(prev)
      // Find the index of the first running query with matching question
      const targetIndex = existing.processingSteps.findIndex(
        step => step.type === 'query' && step.question === question && step.status === 'running'
      )
      if (targetIndex === -1) return prev // No running query found with this question
      const updatedSteps = existing.processingSteps.map((step, index) => {
        if (index === targetIndex) {
          return { ...step, ...update }
        }
        return step
      })
      next.set(sessionId, { ...existing, processingSteps: updatedSteps })
      return next
    })
  }, [])

  // Helper to remove a session from pending
  const removePending = useCallback((sessionId: string) => {
    setPendingChats(prev => {
      if (!prev.has(sessionId)) return prev
      const next = new Map(prev)
      next.delete(sessionId)
      return next
    })
  }, [])

  // Chat message handler (lifted to persist across navigation)
  const sendChatMessage = useCallback(async (sessionId: string, message: string, history: ChatMessage[], skipLock = false) => {
    console.log('[Chat] sendChatMessage called:', { sessionId, message: message.slice(0, 50), historyLen: history.length, skipLock })
    console.trace('[Chat] sendChatMessage stack trace')
    const abortController = new AbortController()

    // Try to acquire lock before starting (unless caller already acquired it)
    if (!skipLock) {
      console.log('[Chat] sendChatMessage: acquiring lock')
      const lockResult = await acquireSessionLock(sessionId, BROWSER_ID, 300, message).catch((err) => {
        console.log('[Chat] sendChatMessage: lock acquisition failed', err)
        return null
      })
      console.log('[Chat] sendChatMessage: lock result', lockResult)
      if (lockResult && !lockResult.acquired) {
        // Another browser has the lock - store it and don't proceed
        console.log('[Lock] Session locked by another browser:', sessionId, lockResult.lock)
        if (lockResult.lock) {
          setExternalLocks(prev => {
            const next = new Map(prev)
            next.set(sessionId, lockResult.lock!)
            return next
          })
        }
        return // Don't start the request
      }
    }

    // Add this session to pending with initial state
    console.log('[Chat] sendChatMessage: adding to pendingChats, starting stream')
    setPendingChats(prev => {
      const next = new Map(prev)
      next.set(sessionId, {
        processingSteps: [], // Start empty - addProcessingStep will update for display
        abortController,
      })
      return next
    })

    try {
      console.log('[Chat] sendChatMessage: calling sendChatMessageStream')
      await sendChatMessageStream(
        message,
        history,
        {
          onThinking: (data) => {
            const step: ProcessingStep = { type: 'thinking', content: data.content }
            addProcessingStep(sessionId, step)
          },
          onQueryStarted: (data) => {
            const step: ProcessingStep = { type: 'query', question: data.question, sql: data.sql, status: 'running' }
            addProcessingStep(sessionId, step)
          },
          onQueryDone: (data) => {
            updateQueryStep(sessionId, data.question, {
              status: data.error ? 'error' : 'completed',
              rows: data.rows,
              error: data.error || undefined,
            })
          },
          onDone: (data) => {
            console.log('[Chat] onDone called', { sessionId, hasAnswer: !!data.answer, answerLen: data.answer?.length, error: data.error })
            // Build processing steps from server data (source of truth)
            const processingSteps = buildProcessingSteps(data)
            console.log('[Chat] onDone processingSteps from server:', processingSteps.length)

            // Update the session - replace streaming message with complete message
            setChatSessions(prev => {
              const session = prev.find(s => s.id === sessionId)
              if (!session) {
                console.log('[Chat] onDone: session not found!', { sessionId })
                return prev
              }

              // Safety check: if there's already a complete assistant message for this turn, don't add another
              const lastUserIdx = session.messages.map(m => m.role).lastIndexOf('user')
              const hasCompleteAfterLastUser = session.messages.slice(lastUserIdx + 1).some(
                m => m.role === 'assistant' && m.status === 'complete'
              )
              if (hasCompleteAfterLastUser) {
                console.log('[Chat] onDone: already has complete message, skipping')
                return prev
              }

              const assistantMessage: ChatMessage = data.error
                ? { id: generateMessageId(), role: 'assistant', content: data.error, status: 'error' }
                : {
                  id: generateMessageId(),
                  role: 'assistant',
                  content: data.answer,
                  workflowData: {
                    dataQuestions: data.dataQuestions ?? [],
                    generatedQueries: data.generatedQueries ?? [],
                    executedQueries: data.executedQueries ?? [],
                    followUpQuestions: data.followUpQuestions,
                    processingSteps,
                  },
                  executedQueries: data.executedQueries?.map(q => q.sql) ?? [],
                  status: 'complete',
                }

              console.log('[Chat] onDone assistantMessage', { hasWorkflowData: !!assistantMessage.workflowData, stepsCount: assistantMessage.workflowData?.processingSteps?.length })

              // Replace the streaming message with the complete one
              const newMessages = session.messages.filter(m => m.status !== 'streaming')
              newMessages.push(assistantMessage)

              return prev.map(s =>
                s.id === sessionId ? { ...s, messages: newMessages, updatedAt: new Date() } : s
              )
            })

            removePending(sessionId)
            // Release lock
            releaseSessionLock(sessionId, BROWSER_ID).catch(() => {})

            // Sync the completed session to server (after state is updated)
            setTimeout(() => {
              setChatSessions(prev => {
                const session = prev.find(s => s.id === sessionId)
                if (session) {
                  chatSyncFns.syncImmediate(session)
                }

                // Auto-generate title for new sessions
                if (!data.error && session && !session.name && session.messages.length <= 2) {
                  generateChatSessionTitle(session.messages).then(result => {
                    if (result.title) {
                      setChatSessions(p => p.map(s =>
                        s.id === sessionId ? { ...s, name: result.title, updatedAt: new Date() } : s
                      ))
                      // Sync again after title update
                      setChatSessions(p => {
                        const updated = p.find(s => s.id === sessionId)
                        if (updated) chatSyncFns.syncImmediate(updated)
                        return p
                      })
                    }
                  }).catch(() => { /* ignore */ })
                }
                return prev
              })
            }, 100)
          },
          onError: (error) => {
            // Update session - replace streaming message with error
            setChatSessions(prev => prev.map(s => {
              if (s.id !== sessionId) return s
              const newMessages = s.messages.filter(m => m.status !== 'streaming')
              newMessages.push({
                id: generateMessageId(),
                role: 'assistant',
                content: error,
                status: 'error',
              })
              return { ...s, messages: newMessages, updatedAt: new Date() }
            }))
            removePending(sessionId)
            // Release lock
            releaseSessionLock(sessionId, BROWSER_ID).catch(() => {})
            // Sync the errored session to server
            setTimeout(() => {
              setChatSessions(prev => {
                const session = prev.find(s => s.id === sessionId)
                if (session) chatSyncFns.syncImmediate(session)
                return prev
              })
            }, 100)
          },
          onWorkflowStarted: (data) => {
            console.log('[Chat] onWorkflowStarted', { sessionId, workflowId: data.workflow_id })
            // Store workflow ID in pending state for potential reconnection
            setPendingChats(prev => {
              const existing = prev.get(sessionId)
              if (!existing) return prev
              const next = new Map(prev)
              next.set(sessionId, { ...existing, workflowId: data.workflow_id })
              return next
            })
            // Also store workflow_id in the streaming message for persistence across page refresh
            setChatSessions(prev => prev.map(s => {
              if (s.id !== sessionId) return s
              return {
                ...s,
                messages: s.messages.map(m =>
                  m.status === 'streaming' ? { ...m, workflowId: data.workflow_id } : m
                ),
              }
            }))
          },
          onRetrying: (attempt, maxAttempts) => {
            console.log('[Chat] onRetrying', { sessionId, attempt, maxAttempts })
            // Retry is handled automatically, just log for debugging
          },
        },
        abortController.signal,
        sessionId // Pass session_id for workflow persistence
      )
      console.log('[Chat] sendChatMessage: sendChatMessageStream completed')
    } catch (err) {
      console.log('[Chat] sendChatMessage: caught error', err)
      // Don't show error if aborted
      if (err instanceof Error && err.name === 'AbortError') {
        // Remove streaming message on abort
        setChatSessions(prev => prev.map(s => {
          if (s.id !== sessionId) return s
          const newMessages = s.messages.filter(m => m.status !== 'streaming')
          return { ...s, messages: newMessages, updatedAt: new Date() }
        }))
        removePending(sessionId)
        // Release lock
        releaseSessionLock(sessionId, BROWSER_ID).catch(() => {})
        return
      }
      // Update session - replace streaming message with error
      setChatSessions(prev => prev.map(s => {
        if (s.id !== sessionId) return s
        const newMessages = s.messages.filter(m => m.status !== 'streaming')
        newMessages.push({
          id: generateMessageId(),
          role: 'assistant',
          content: err instanceof Error ? err.message : 'Something went wrong. Please try again.',
          status: 'error',
        })
        return { ...s, messages: newMessages, updatedAt: new Date() }
      }))
      removePending(sessionId)
      // Release lock
      releaseSessionLock(sessionId, BROWSER_ID).catch(() => {})
    }
  }, [addProcessingStep, updateQueryStep, removePending, pendingChats])

  const abortChatMessage = useCallback((sessionId: string) => {
    const pending = pendingChats.get(sessionId)
    if (pending) {
      pending.abortController.abort()
      removePending(sessionId)
    }
  }, [pendingChats, removePending])

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
    chatSessions,
    setChatSessions,
    currentChatSessionId,
    setCurrentChatSessionId,
    chatSessionsLoaded,
    chatServerSyncComplete,
    pendingChats,
    setPendingChats,
    sendChatMessage,
    abortChatMessage,
    externalLocks,
    setExternalLocks,
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

  const handleNewChatSession = () => {
    const newSession = createChatSession()
    setChatSessions(prev => [...prev, newSession])
    navigate(`/chat/${newSession.id}`)
  }

  const handleSelectChatSession = (session: ChatSession) => {
    navigate(`/chat/${session.id}`)
  }

  const handleDeleteChatSession = (sessionId: string) => {
    setChatSessions(prev => prev.filter(s => s.id !== sessionId))
    deleteSessionFromServer(sessionId) // Also delete from server
    if (sessionId === currentChatSessionId && chatSessions.length > 1) {
      const remaining = chatSessions.filter(s => s.id !== sessionId)
      if (remaining.length > 0) {
        navigate(`/chat/${remaining[0].id}`)
      }
    }
  }

  const handleRenameQuerySession = (sessionId: string, name: string) => {
    setSessions(prev => prev.map(s =>
      s.id === sessionId ? { ...s, name, updatedAt: new Date() } : s
    ))
  }

  const handleRenameChatSession = (sessionId: string, name: string) => {
    setChatSessions(prev => prev.map(s =>
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

  const handleGenerateTitleChatSession = async (sessionId: string) => {
    const session = chatSessions.find(s => s.id === sessionId)
    if (!session || session.messages.length === 0) return

    const result = await generateChatSessionTitle(session.messages)
    if (result.title) {
      setChatSessions(prev => prev.map(s =>
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
    const handleNewChat = () => handleNewChatSession()
    window.addEventListener('keydown', handleKeyDown)
    window.addEventListener('open-search', handleOpenSearch)
    window.addEventListener('new-chat-session', handleNewChat)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('open-search', handleOpenSearch)
      window.removeEventListener('new-chat-session', handleNewChat)
    }
  }, [handleNewChatSession])

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
          chatSessions={chatSessions}
          currentChatSessionId={currentChatSessionId}
          onNewChatSession={handleNewChatSession}
          onSelectChatSession={handleSelectChatSession}
          onDeleteChatSession={handleDeleteChatSession}
          onRenameChatSession={handleRenameChatSession}
          onGenerateTitleChatSession={handleGenerateTitleChatSession}
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
            <Route path="/chat" element={<ChatRedirect />} />
            <Route path="/chat/:sessionId" element={
              <ChatSessionSync>
                <ChatView />
              </ChatSessionSync>
            } />
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

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AppContent />
    </QueryClientProvider>
  )
}
