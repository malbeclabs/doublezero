import { useState, useRef, useEffect, useCallback, createContext, useContext } from 'react'
import { Routes, Route, Navigate, useNavigate, useParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Catalog } from '@/components/catalog'
import { PromptInput } from '@/components/prompt-input'
import { QueryEditor, type QueryEditorHandle } from '@/components/query-editor'
import { ResultsTable } from '@/components/results-table'
import { SessionHistory, type GenerationRecord } from '@/components/session-history'
import { SessionsPage } from '@/components/sessions-page'
import { ChatSessionsPage } from '@/components/chat-sessions-page'
import { Chat } from '@/components/chat'
import { Sidebar } from '@/components/sidebar'
import { generateSessionTitle, generateChatSessionTitle, type SessionQueryInfo } from '@/lib/api'
import type { TableInfo, QueryResponse, HistoryMessage, ChatMessage } from '@/lib/api'
import {
  type QuerySession,
  type ChatSession,
  loadSessions,
  saveSessions,
  createSession,
  loadChatSessions,
  saveChatSessions,
  createChatSession,
} from '@/lib/sessions'

const queryClient = new QueryClient()

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
  // Chat state
  chatSessions: ChatSession[]
  setChatSessions: React.Dispatch<React.SetStateAction<ChatSession[]>>
  currentChatSessionId: string
  setCurrentChatSessionId: React.Dispatch<React.SetStateAction<string>>
  chatSessionsLoaded: boolean
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
  const navigate = useNavigate()
  const loadedSessionRef = useRef<string | null>(null)
  const {
    sessions,
    sessionsLoaded,
    setCurrentSessionId,
    setResults,
    setQuery,
    queryEditorRef,
  } = useAppContext()

  useEffect(() => {
    if (!sessionsLoaded) return

    if (sessionId) {
      const session = sessions.find(s => s.id === sessionId)
      if (session) {
        // Always update currentSessionId
        setCurrentSessionId(sessionId)

        // Only load session data if we haven't loaded this session yet
        if (loadedSessionRef.current !== sessionId) {
          loadedSessionRef.current = sessionId
          setResults(null)
          if (session.history.length > 0) {
            const latestQuery = session.history[0].sql
            setQuery(latestQuery)
            setTimeout(() => {
              queryEditorRef.current?.run(latestQuery)
            }, 100)
          } else {
            setQuery('')
          }
        }
      } else {
        if (sessions.length > 0) {
          const mostRecent = [...sessions].sort(
            (a, b) => b.updatedAt.getTime() - a.updatedAt.getTime()
          )[0]
          navigate(`/query/${mostRecent.id}`, { replace: true })
        }
      }
    }
  }, [sessionId, sessionsLoaded, sessions, setCurrentSessionId, setResults, setQuery, queryEditorRef, navigate])

  return <>{children}</>
}

// Stable component for syncing chat session from URL
function ChatSessionSync({ children }: { children: React.ReactNode }) {
  const { sessionId } = useParams()
  const navigate = useNavigate()
  const {
    chatSessions,
    chatSessionsLoaded,
    setCurrentChatSessionId,
  } = useAppContext()

  useEffect(() => {
    if (!chatSessionsLoaded) return

    if (sessionId) {
      const session = chatSessions.find(s => s.id === sessionId)
      if (session) {
        setCurrentChatSessionId(sessionId)
      } else {
        if (chatSessions.length > 0) {
          const mostRecent = [...chatSessions].sort(
            (a, b) => b.updatedAt.getTime() - a.updatedAt.getTime()
          )[0]
          navigate(`/chat/${mostRecent.id}`, { replace: true })
        }
      }
    }
  }, [sessionId, chatSessionsLoaded, chatSessions, setCurrentChatSessionId, navigate])

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
  const { chatSessions, setChatSessions, chatSessionsLoaded } = useAppContext()

  useEffect(() => {
    if (!chatSessionsLoaded) return

    // Find the most recent session
    const mostRecent = [...chatSessions].sort(
      (a, b) => b.updatedAt.getTime() - a.updatedAt.getTime()
    )[0]

    // If most recent session is empty, use it; otherwise create a new one
    if (mostRecent && mostRecent.messages.length === 0) {
      navigate(`/chat/${mostRecent.id}`, { replace: true })
    } else {
      const newSession = createChatSession()
      setChatSessions(prev => [...prev, newSession])
      navigate(`/chat/${newSession.id}`, { replace: true })
    }
  }, [chatSessionsLoaded, chatSessions, setChatSessions, navigate])

  return null
}

// Query Editor View
function QueryEditorView() {
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
  }

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
            onResults={setResults}
            onClear={handleClear}
            onManualRun={handleManualRun}
          />
          <ResultsTable results={results} />
        </div>
      </div>
    </div>
  )
}

// Chat View
function ChatView() {
  const { chatSessions, setChatSessions, currentChatSessionId } = useAppContext()

  const currentChatSession = chatSessions.find(s => s.id === currentChatSessionId)
  const chatMessages = currentChatSession?.messages ?? []

  const handleChatMessagesChange = useCallback(async (messages: ChatMessage[]) => {
    // Update messages
    setChatSessions(prev => prev.map(session => {
      if (session.id === currentChatSessionId) {
        return {
          ...session,
          updatedAt: new Date(),
          messages,
        }
      }
      return session
    }))

    // Auto-generate title after first assistant response if no name
    const session = chatSessions.find(s => s.id === currentChatSessionId)
    if (session && !session.name && session.messages.length === 0 && messages.length >= 2) {
      // This is the first response - generate title
      try {
        const result = await generateChatSessionTitle(messages)
        if (result.title) {
          setChatSessions(prev => prev.map(s =>
            s.id === currentChatSessionId ? { ...s, name: result.title, updatedAt: new Date() } : s
          ))
        }
      } catch {
        // Silently fail - title generation is not critical
      }
    }
  }, [currentChatSessionId, setChatSessions, chatSessions])

  return (
    <Chat
      messages={chatMessages}
      onMessagesChange={handleChatMessagesChange}
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

    const queries: SessionQueryInfo[] = session.history
      .filter(h => h.type === 'generation' && h.prompt)
      .map(h => ({ prompt: h.prompt!, sql: h.sql }))
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
    chatSessions,
    setChatSessions,
    currentChatSessionId,
    setCurrentChatSessionId,
    chatSessionsLoaded,
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

    const queries: SessionQueryInfo[] = session.history
      .filter(h => h.type === 'generation' && h.prompt)
      .map(h => ({ prompt: h.prompt!, sql: h.sql }))
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
      <div className="h-screen flex">
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
            <Route path="*" element={<Navigate to="/query" replace />} />
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
