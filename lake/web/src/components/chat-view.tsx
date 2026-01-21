import { useCallback, useState, useEffect } from 'react'
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

  // Fetch session data
  const { data: session, isLoading: sessionLoading, error: sessionError } = useChatSession(sessionId)

  // Streaming state
  const { sendMessage, abort, isStreaming, processingSteps } = useChatStream(sessionId)

  // Handle workflow reconnection for incomplete messages
  const [reconnectState, setReconnectState] = useState<Partial<ChatStreamState>>({})
  const handleReconnectUpdate = useCallback((state: Partial<ChatStreamState>) => {
    setReconnectState(prev => ({ ...prev, ...state }))
  }, [])

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

  // Handle initial question from URL param
  const [initialQuestionHandled, setInitialQuestionHandled] = useState<string | null>(null)
  useEffect(() => {
    const initialQuestion = searchParams.get('q')
    if (initialQuestion && session && !sessionLoading && !isPending && initialQuestionHandled !== sessionId) {
      // Only send if session is empty
      if (session.messages.length === 0) {
        setInitialQuestionHandled(sessionId ?? null)
        // Clear the query param
        setSearchParams({}, { replace: true })
        // Send the message
        sendMessage(initialQuestion)
      }
    }
  }, [searchParams, session, sessionLoading, isPending, sessionId, initialQuestionHandled, setSearchParams, sendMessage])

  // Handle creating new session for URL that doesn't exist
  useEffect(() => {
    if (sessionId && sessionError && !sessionLoading) {
      // Session doesn't exist, redirect to /chat to create new one
      navigate('/chat', { replace: true })
    }
  }, [sessionId, sessionError, sessionLoading, navigate])

  // Create new session when navigating to /chat without ID
  useEffect(() => {
    if (!sessionId) {
      // Always create a new session - don't try to reuse empty ones
      // as the cached sessions list may be stale
      const newId = crypto.randomUUID()
      createSession(newId, 'chat', []).then(() => {
        // Pre-populate cache so we don't have to fetch
        queryClient.setQueryData<ChatSession>(chatKeys.detail(newId), {
          id: newId,
          messages: [],
          createdAt: new Date(),
          updatedAt: new Date(),
        })
        queryClient.invalidateQueries({ queryKey: chatKeys.list() })
        navigate(`/chat/${newId}`, { replace: true })
      }).catch(() => {
        // If creation fails, just navigate and let the stream create it
        navigate(`/chat/${newId}`, { replace: true })
      })
    }
  }, [sessionId, navigate, queryClient])

  // Handle send message
  const handleSendMessage = useCallback((message: string) => {
    if (!sessionId || isPending) return
    sendMessage(message)
  }, [sessionId, isPending, sendMessage])

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

  // Show loading state
  if (sessionLoading || !session) {
    return <ChatSkeleton />
  }

  return (
    <Chat
      messages={session.messages}
      isPending={isPending}
      processingSteps={activeProcessingSteps}
      onSendMessage={handleSendMessage}
      onAbort={handleAbort}
      onRetry={handleRetry}
      onOpenInQueryEditor={handleOpenInQueryEditor}
    />
  )
}
