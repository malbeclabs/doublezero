import { useCallback, useState, useEffect, useRef } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { Chat, ChatSkeleton } from './chat'
import {
  useChatSession,
  useChatStream,
  useWorkflowReconnect,
  chatKeys,
  type ChatStreamState,
} from '@/hooks/use-chat'
import type { ChatSession } from '@/lib/sessions'
import { useQueryClient } from '@tanstack/react-query'
import { createSession } from '@/lib/api'

export function SimplifiedChatView() {
  const { sessionId } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const queryClient = useQueryClient()

  // Track if we're creating a session (for new chat flow)
  const [isCreatingSession, setIsCreatingSession] = useState(false)

  // Fetch session data (only when we have a sessionId)
  const { data: session, isLoading: sessionLoading } = useChatSession(sessionId)

  // Streaming state (only when we have a sessionId)
  const { sendMessage, abort, isStreaming, processingSteps } = useChatStream(sessionId)

  // Handle workflow reconnection for incomplete messages
  const [reconnectState, setReconnectState] = useState<Partial<ChatStreamState>>({})
  const handleReconnectUpdate = useCallback((state: Partial<ChatStreamState>) => {
    setReconnectState(prev => {
      // Accumulate processing steps instead of replacing, but deduplicate by ID
      if (state.processingSteps && state.processingSteps.length > 0) {
        const existingSteps = prev.processingSteps ?? []
        // Deduplicate by step ID
        const newSteps = state.processingSteps.filter(newStep => {
          return !existingSteps.some(s => s.id === newStep.id)
        })
        if (newSteps.length === 0) {
          return { ...prev, ...state, processingSteps: existingSteps }
        }
        return {
          ...prev,
          ...state,
          processingSteps: [...existingSteps, ...newSteps],
        }
      }
      return { ...prev, ...state }
    })
  }, [])

  // Reset reconnect state when sessionId changes (e.g., navigating to new chat)
  const prevSessionIdRef = useRef(sessionId)
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId
      setReconnectState({})
    }
  }, [sessionId])

  useWorkflowReconnect(
    sessionId,
    session?.messages ?? [],
    handleReconnectUpdate
  )

  // Combined streaming state (from active send or reconnect)
  const isPending = isStreaming || reconnectState.isStreaming || false
  const activeProcessingSteps = isStreaming
    ? processingSteps
    : reconnectState.processingSteps ?? []

  // Handle initial question from URL param (when we have a sessionId)
  // Use a ref to track which session+question combos we've handled
  const handledQueriesRef = useRef<Set<string>>(new Set())
  useEffect(() => {
    const initialQuestion = searchParams.get('q')
    if (!initialQuestion || !sessionId || !session) return

    const key = `${sessionId}:${initialQuestion}`
    if (handledQueriesRef.current.has(key)) return

    // Only send if session is empty and we're not already streaming
    if (session.messages.length === 0 && !isStreaming) {
      handledQueriesRef.current.add(key)
      // Clear the query param
      setSearchParams({}, { replace: true })
      // Send the message
      sendMessage(initialQuestion)
    }
  }, [searchParams, session, sessionId, isStreaming, setSearchParams, sendMessage])

  // Handle initial question from URL param (when on /chat without sessionId)
  const initialQuestionHandledForNewRef = useRef(false)
  useEffect(() => {
    const initialQuestion = searchParams.get('q')
    if (initialQuestion && !sessionId && !isCreatingSession && !initialQuestionHandledForNewRef.current) {
      initialQuestionHandledForNewRef.current = true
      setSearchParams({}, { replace: true })
      handleSendMessageForNewChat(initialQuestion)
    }
  }, [searchParams, sessionId, isCreatingSession, setSearchParams])

  // Create session and navigate (for /chat route)
  const handleSendMessageForNewChat = useCallback(async (message: string) => {
    if (isCreatingSession) return

    setIsCreatingSession(true)
    const newId = crypto.randomUUID()

    try {
      // Create session on server
      await createSession(newId, 'chat', [])

      // Pre-populate caches
      const newSession: ChatSession = {
        id: newId,
        messages: [],
        createdAt: new Date(),
        updatedAt: new Date(),
      }
      queryClient.setQueryData<ChatSession>(chatKeys.detail(newId), newSession)
      queryClient.setQueryData<ChatSession[]>(chatKeys.list(), (oldList) => {
        if (!oldList) return [newSession]
        return [newSession, ...oldList]
      })

      // Navigate to new session with message in URL param
      // The effect above will handle sending the message
      navigate(`/chat/${newId}?q=${encodeURIComponent(message)}`, { replace: true })
    } catch {
      // If creation fails, still try to navigate - the stream will create it
      navigate(`/chat/${newId}?q=${encodeURIComponent(message)}`, { replace: true })
    } finally {
      setIsCreatingSession(false)
    }
  }, [isCreatingSession, navigate, queryClient])

  // Handle send message
  const handleSendMessage = useCallback((message: string) => {
    if (isPending || isCreatingSession) return

    if (!sessionId) {
      // No session yet - create one and send message
      handleSendMessageForNewChat(message)
    } else {
      // Have session - send directly
      sendMessage(message)
    }
  }, [sessionId, isPending, isCreatingSession, sendMessage, handleSendMessageForNewChat])

  // Handle retry (resend last user message)
  const handleRetry = useCallback(() => {
    if (!session || isPending) return
    const lastUserMessage = [...session.messages].reverse().find(m => m.role === 'user')
    if (lastUserMessage) {
      sendMessage(lastUserMessage.content)
    }
  }, [session, isPending, sendMessage])

  // Handle abort
  const handleAbort = useCallback(() => {
    abort()
  }, [abort])

  // Handle opening query in query editor
  const handleOpenInQueryEditor = useCallback((query: string, type: 'sql' | 'cypher') => {
    // Navigate to query editor with query in URL param
    const newSessionId = crypto.randomUUID()
    navigate(`/query/${newSessionId}?${type}=${encodeURIComponent(query)}`)
  }, [navigate])

  // New chat (no sessionId) - show empty chat
  if (!sessionId) {
    return (
      <Chat
        messages={[]}
        isPending={isCreatingSession}
        processingSteps={[]}
        onSendMessage={handleSendMessage}
        onAbort={handleAbort}
        onOpenInQueryEditor={handleOpenInQueryEditor}
      />
    )
  }

  // Loading existing session - show skeleton
  if (sessionLoading && !session) {
    return <ChatSkeleton />
  }

  // Show Chat with session data
  return (
    <Chat
      messages={session?.messages ?? []}
      isPending={isPending}
      processingSteps={activeProcessingSteps}
      onSendMessage={handleSendMessage}
      onAbort={handleAbort}
      onRetry={handleRetry}
      onOpenInQueryEditor={handleOpenInQueryEditor}
    />
  )
}
