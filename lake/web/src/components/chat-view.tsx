import { useCallback, useState, useEffect } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { Chat, ChatSkeleton } from './chat'
import {
  useChatSession,
  useChatSessions,
  useChatStream,
  useWorkflowReconnect,
  chatKeys,
  type ChatStreamState,
} from '@/hooks/use-chat'
import { useQueryClient } from '@tanstack/react-query'
import { createSession } from '@/lib/api'

interface ChatViewProps {
  onOpenInQueryEditor?: (sql: string) => void
}

export function SimplifiedChatView({ onOpenInQueryEditor }: ChatViewProps) {
  const { sessionId } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const queryClient = useQueryClient()

  // Fetch session data
  const { data: session, isLoading: sessionLoading, error: sessionError } = useChatSession(sessionId)
  const { data: sessions } = useChatSessions()

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
    if (!sessionId && sessions !== undefined) {
      // Find empty session or create new one
      const emptySession = sessions.find(s => s.messages.length === 0)
      if (emptySession) {
        navigate(`/chat/${emptySession.id}`, { replace: true })
      } else {
        // Create new session on server
        const newId = crypto.randomUUID()
        createSession(newId, 'chat', []).then(() => {
          // Invalidate list and navigate
          queryClient.invalidateQueries({ queryKey: chatKeys.list() })
          navigate(`/chat/${newId}`, { replace: true })
        }).catch(() => {
          // If creation fails, just navigate and let the stream create it
          navigate(`/chat/${newId}`, { replace: true })
        })
      }
    }
  }, [sessionId, sessions, navigate, queryClient])

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
      onOpenInQueryEditor={onOpenInQueryEditor}
    />
  )
}
