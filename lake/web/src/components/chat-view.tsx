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
  const pendingMessageRef = useRef<string | null>(null)

  // Fetch session data (only when we have a sessionId)
  const { data: session, isLoading: sessionLoading } = useChatSession(sessionId)

  // Streaming state (only when we have a sessionId)
  const { sendMessage, abort, isStreaming, processingSteps } = useChatStream(sessionId)

  // Handle workflow reconnection for incomplete messages
  const [reconnectState, setReconnectState] = useState<Partial<ChatStreamState>>({})
  const handleReconnectUpdate = useCallback((state: Partial<ChatStreamState>) => {
    setReconnectState(prev => ({ ...prev, ...state }))
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
  const [initialQuestionHandled, setInitialQuestionHandled] = useState<string | null>(null)
  useEffect(() => {
    const initialQuestion = searchParams.get('q')
    if (initialQuestion && sessionId && session && !sessionLoading && !isPending && initialQuestionHandled !== sessionId) {
      // Only send if session is empty
      if (session.messages.length === 0) {
        setInitialQuestionHandled(sessionId)
        // Clear the query param
        setSearchParams({}, { replace: true })
        // Send the message
        sendMessage(initialQuestion)
      }
    }
  }, [searchParams, session, sessionLoading, isPending, sessionId, initialQuestionHandled, setSearchParams, sendMessage])

  // Handle initial question from URL param (when on /chat without sessionId)
  const initialQuestionHandledForNewRef = useRef(false)
  useEffect(() => {
    const initialQuestion = searchParams.get('q')
    if (initialQuestion && !sessionId && !isCreatingSession && !initialQuestionHandledForNewRef.current) {
      initialQuestionHandledForNewRef.current = true
      // Clear the query param and create session with this message
      setSearchParams({}, { replace: true })
      handleSendMessageForNewChat(initialQuestion)
    }
  }, [searchParams, sessionId, isCreatingSession, setSearchParams])

  // Create session and send first message (for /chat route)
  const handleSendMessageForNewChat = useCallback(async (message: string) => {
    if (isCreatingSession) return

    setIsCreatingSession(true)
    pendingMessageRef.current = message

    const newId = crypto.randomUUID()
    try {
      await createSession(newId, 'chat', [])
      // Pre-populate detail cache
      const newSession: ChatSession = {
        id: newId,
        messages: [],
        createdAt: new Date(),
        updatedAt: new Date(),
      }
      queryClient.setQueryData<ChatSession>(chatKeys.detail(newId), newSession)
      // Add to list cache directly (don't invalidate - that causes race conditions)
      queryClient.setQueryData<ChatSession[]>(chatKeys.list(), (oldList) => {
        if (!oldList) return [newSession]
        return [newSession, ...oldList]
      })
      // Navigate to new session with the message as query param so it gets sent
      navigate(`/chat/${newId}?q=${encodeURIComponent(message)}`, { replace: true })
    } catch {
      // If creation fails, still try to navigate - the stream will create it
      navigate(`/chat/${newId}?q=${encodeURIComponent(message)}`, { replace: true })
    } finally {
      setIsCreatingSession(false)
      pendingMessageRef.current = null
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

  // Handle opening SQL in query editor
  const handleOpenInQueryEditor = useCallback((sql: string) => {
    // Navigate to query editor with SQL in URL param
    const newSessionId = crypto.randomUUID()
    navigate(`/query/${newSessionId}?sql=${encodeURIComponent(sql)}`)
  }, [navigate])

  // New chat (no sessionId) - show empty chat immediately
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
