import { useState, useRef, useEffect, useCallback, createContext, useContext } from 'react'
import { Routes, Route, Navigate, useNavigate, useParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { Catalog } from '@/components/catalog'
import { PromptInput } from '@/components/prompt-input'
import { QueryEditor, type QueryEditorHandle } from '@/components/query-editor'
import { ResultsView } from '@/components/results-view'
import { SessionHistory, type GenerationRecord } from '@/components/session-history'
import { SessionsPage } from '@/components/sessions-page'
import { ChatSessionsPage } from '@/components/chat-sessions-page'
import { Chat, type ChatProgress, type QueryProgressItem, type PipelineStep } from '@/components/chat'
import { Landing } from '@/components/landing'
import { Sidebar } from '@/components/sidebar'
import { generateSessionTitle, generateChatSessionTitle, sendChatMessageStream, recommendVisualization, fetchCatalog, type SessionQueryInfo } from '@/lib/api'
import type { TableInfo, QueryResponse, HistoryMessage, ChatMessage } from '@/lib/api'
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
} from '@/lib/sessions'

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
  progress: ChatProgress
  abortController: AbortController
}

// Context for app state
interface AppContextType {
  // Query state
  sessions: QuerySession[]
  setSessions: React.Dispatch<React.SetStateAction<QuerySession[]>>
  currentSessionId: string
  setCurrentSessionId: React.Dispatch<React.SetStateAction<string>>
  sessionsLoaded: boolean
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
  // Chat mutation state (lifted to persist across navigation) - now per-session
  pendingChats: Map<string, PendingChatState>
  sendChatMessage: (sessionId: string, message: string, history: ChatMessage[]) => void
  abortChatMessage: (sessionId: string) => void
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
    setCurrentSessionId,
    setResults,
    setQuery,
    queryEditorRef,
    pendingQueryRef,
  } = useAppContext()

  useEffect(() => {
    if (!sessionsLoaded || !sessionId) return

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
  }, [sessionId, sessionsLoaded, sessions, setSessions, setCurrentSessionId, setResults, setQuery, queryEditorRef, pendingQueryRef])

  return <>{children}</>
}

// Stable component for syncing chat session from URL
function ChatSessionSync({ children }: { children: React.ReactNode }) {
  const { sessionId } = useParams()
  const {
    chatSessions,
    setChatSessions,
    chatSessionsLoaded,
    setCurrentChatSessionId,
  } = useAppContext()

  useEffect(() => {
    if (!chatSessionsLoaded || !sessionId) return

    const session = chatSessions.find(s => s.id === sessionId)

    // If session doesn't exist (e.g., page refresh on a new empty session),
    // create it with the URL's ID to preserve the URL
    if (!session) {
      const newSession = createChatSessionWithId(sessionId)
      setChatSessions(prev => [...prev, newSession])
    }

    setCurrentChatSessionId(sessionId)
  }, [sessionId, chatSessionsLoaded, chatSessions, setChatSessions, setCurrentChatSessionId])

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

  // Visualization recommendation state
  const [isRecommending, setIsRecommending] = useState(false)
  const [recommendedConfig, setRecommendedConfig] = useState<{
    chartType: 'bar' | 'line' | 'pie' | 'area' | 'scatter'
    xAxis: string
    yAxis: string[]
  } | null>(null)

  const currentSession = sessions.find(s => s.id === currentSessionId)
  const generationHistory = currentSession?.history ?? []

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

  const handleRestoreQuery = (sql: string) => {
    setQuery(sql)
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
          ? { ...s, messages: [{ role: 'user' as const, content: question }], updatedAt: new Date() }
          : s
      ))
      sendChatMessage(emptySession.id, question, [])
      navigate(`/chat/${emptySession.id}`)
    } else {
      // Create a new session
      const newSession = createChatSession()
      setChatSessions(prev => [...prev, { ...newSession, messages: [{ role: 'user' as const, content: question }] }])
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
  const {
    sessions,
    setSessions,
    chatSessions,
    setChatSessions,
    currentChatSessionId,
    pendingChats,
    sendChatMessage,
    abortChatMessage,
    pendingQueryRef,
  } = useAppContext()

  const currentChatSession = chatSessions.find(s => s.id === currentChatSessionId)
  const chatMessages = currentChatSession?.messages ?? []
  const pendingState = pendingChats.get(currentChatSessionId)
  const isPending = !!pendingState
  const currentProgress = pendingState?.progress ?? null

  // Check for initial question from landing page
  const initialQuestionSent = useRef<string | null>(null)
  useEffect(() => {
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
            messages: [...session.messages, { role: 'user' as const, content: initialQuestion }],
          }
        }
        return session
      }))
      // Send to API
      sendChatMessage(currentChatSessionId, initialQuestion, [])
    }
  }, [currentChatSession, currentChatSessionId, chatMessages.length, isPending, setChatSessions, sendChatMessage])

  const handleSendMessage = useCallback((message: string) => {
    // Add user message immediately
    setChatSessions(prev => prev.map(session => {
      if (session.id === currentChatSessionId) {
        return {
          ...session,
          updatedAt: new Date(),
          messages: [...session.messages, { role: 'user' as const, content: message }],
        }
      }
      return session
    }))

    // Send to API (this persists across navigation)
    sendChatMessage(currentChatSessionId, message, chatMessages)
  }, [currentChatSessionId, setChatSessions, sendChatMessage, chatMessages])

  const handleOpenInQueryEditor = useCallback((sql: string) => {
    // Store the SQL to be loaded when the query editor syncs
    pendingQueryRef.current = sql

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

  return (
    <Chat
      messages={chatMessages}
      isPending={isPending}
      progress={currentProgress ?? undefined}
      onSendMessage={handleSendMessage}
      onAbort={handleAbort}
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

  // Save sessions to localStorage when they change (only non-empty sessions)
  useEffect(() => {
    if (sessionsLoaded) {
      const nonEmptySessions = sessions.filter(s => s.history.length > 0)
      saveSessions(nonEmptySessions)
    }
  }, [sessions, sessionsLoaded])

  // Save chat sessions to localStorage when they change (only non-empty sessions)
  useEffect(() => {
    if (chatSessionsLoaded) {
      const nonEmptyChatSessions = chatSessions.filter(s => s.messages.length > 0)
      saveChatSessions(nonEmptyChatSessions)
    }
  }, [chatSessions, chatSessionsLoaded])

  // Helper to update progress for a specific session
  // Accepts either a progress object or a function that receives the previous progress
  const updatePendingProgress = useCallback((
    sessionId: string,
    progressOrFn: ChatProgress | ((prev: ChatProgress) => ChatProgress)
  ) => {
    setPendingChats(prev => {
      const existing = prev.get(sessionId)
      if (!existing) return prev
      const next = new Map(prev)
      const newProgress = typeof progressOrFn === 'function'
        ? progressOrFn(existing.progress)
        : progressOrFn
      next.set(sessionId, { ...existing, progress: newProgress })
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
  const sendChatMessage = useCallback(async (sessionId: string, message: string, history: ChatMessage[]) => {
    const abortController = new AbortController()

    // Add this session to pending with initial state
    setPendingChats(prev => {
      const next = new Map(prev)
      next.set(sessionId, {
        progress: { status: 'Starting...' },
        abortController,
      })
      return next
    })

    try {
      await sendChatMessageStream(
        message,
        history,
        {
          onStatus: (status) => {
            updatePendingProgress(sessionId, (prev) => {
              // Track completed steps - when we move to a new step, previous ones are done
              const stepOrder: PipelineStep[] = ['classifying', 'decomposing', 'executing', 'synthesizing']
              const currentStepIndex = stepOrder.indexOf(status.step as PipelineStep)
              const completedSteps = currentStepIndex > 0
                ? stepOrder.slice(0, currentStepIndex)
                : []
              return {
                ...prev,
                status: status.message,
                step: status.step,
                completedSteps,
              }
            })
          },
          onDecomposed: (data) => {
            // Initialize all queries as pending, first one as running
            const queries: QueryProgressItem[] = data.questions.map((q, i) => ({
              question: q.question,
              status: i === 0 ? 'running' : 'pending',
            }))
            updatePendingProgress(sessionId, (prev) => ({
              ...prev,
              status: `Running ${data.count} queries...`,
              step: 'executing',
              questionsCount: data.count,
              queriesCompleted: 0,
              queriesTotal: data.count,
              queries,
              completedSteps: ['classifying', 'decomposing'],
            }))
          },
          onQueryProgress: (data) => {
            updatePendingProgress(sessionId, (prev) => {
              // Update queries list: mark completed query, set next to running
              const queries = prev.queries?.map((q, i) => {
                if (q.question === data.question) {
                  return {
                    ...q,
                    status: data.success ? 'completed' : 'error',
                    rows: data.rows,
                  } as QueryProgressItem
                }
                // Set the next pending query to running
                if (q.status === 'pending' && i === data.completed) {
                  return { ...q, status: 'running' } as QueryProgressItem
                }
                return q
              })
              return {
                ...prev,
                status: `Running queries...`,
                step: 'executing',
                queriesCompleted: data.completed,
                queriesTotal: data.total,
                lastQuery: data.question,
                queries,
              }
            })
          },
          onDone: (data) => {
            // Update the session with the response
            setChatSessions(prev => {
              const session = prev.find(s => s.id === sessionId)
              if (!session) return prev

              const assistantMessage: ChatMessage = data.error
                ? { role: 'assistant', content: data.error }
                : {
                  role: 'assistant',
                  content: data.answer,
                  pipelineData: {
                    dataQuestions: data.dataQuestions ?? [],
                    generatedQueries: data.generatedQueries ?? [],
                    executedQueries: data.executedQueries ?? [],
                    followUpQuestions: data.followUpQuestions,
                  },
                  executedQueries: data.executedQueries?.map(q => q.sql) ?? [],
                }

              const newMessages: ChatMessage[] = [...session.messages, assistantMessage]

              return prev.map(s =>
                s.id === sessionId ? { ...s, messages: newMessages, updatedAt: new Date() } : s
              )
            })

            // Auto-generate title for new sessions
            if (!data.error) {
              setChatSessions(prev => {
                const session = prev.find(s => s.id === sessionId)
                if (session && !session.name && session.messages.length <= 2) {
                  // Generate title async (don't await)
                  generateChatSessionTitle(session.messages).then(result => {
                    if (result.title) {
                      setChatSessions(p => p.map(s =>
                        s.id === sessionId ? { ...s, name: result.title, updatedAt: new Date() } : s
                      ))
                    }
                  }).catch(() => { /* ignore */ })
                }
                return prev
              })
            }

            removePending(sessionId)
          },
          onError: (error) => {
            // Update session with error
            setChatSessions(prev => prev.map(s =>
              s.id === sessionId
                ? {
                  ...s,
                  messages: [...s.messages, {
                    role: 'assistant',
                    content: error
                  }],
                  updatedAt: new Date()
                }
                : s
            ))
            removePending(sessionId)
          },
        },
        abortController.signal
      )
    } catch (err) {
      // Don't show error if aborted
      if (err instanceof Error && err.name === 'AbortError') {
        removePending(sessionId)
        return
      }
      // Update session with error
      setChatSessions(prev => prev.map(s =>
        s.id === sessionId
          ? {
            ...s,
            messages: [...s.messages, {
              role: 'assistant',
              content: err instanceof Error ? err.message : 'Something went wrong. Please try again.'
            }],
            updatedAt: new Date()
          }
          : s
      ))
      removePending(sessionId)
    }
  }, [updatePendingProgress, removePending])

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
    pendingChats,
    sendChatMessage,
    abortChatMessage,
  }

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

            {/* Default redirect */}
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>
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
