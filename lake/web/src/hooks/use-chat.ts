import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback, useRef, useState, useEffect } from 'react'
import type { ChatMessage, ProcessingStep } from '@/lib/api'
import {
  listSessionsWithContent,
  getSession,
  deleteSession,
  updateSession,
  sendChatMessageStream,
  getLatestWorkflowForSession,
  reconnectToWorkflow,
  generateMessageId,
  generateChatSessionTitle,
} from '@/lib/api'
import { serverToChatSession, type ChatSession } from '@/lib/sessions'

// Query keys
export const chatKeys = {
  all: ['chat-sessions'] as const,
  lists: () => [...chatKeys.all, 'list'] as const,
  list: () => [...chatKeys.lists()] as const,
  details: () => [...chatKeys.all, 'detail'] as const,
  detail: (id: string) => [...chatKeys.details(), id] as const,
}

// Hook to list all chat sessions
export function useChatSessions() {
  return useQuery({
    queryKey: chatKeys.list(),
    queryFn: async () => {
      const response = await listSessionsWithContent<ChatMessage[]>('chat', 100)
      return response.sessions.map(serverToChatSession)
    },
    staleTime: 30 * 1000, // Consider data fresh for 30 seconds
  })
}

// Hook to get a single chat session
export function useChatSession(sessionId: string | undefined) {
  return useQuery({
    queryKey: chatKeys.detail(sessionId ?? ''),
    queryFn: async () => {
      if (!sessionId) return null
      const session = await getSession<ChatMessage[]>(sessionId)
      return serverToChatSession(session)
    },
    enabled: !!sessionId,
    staleTime: 10 * 1000, // Shorter stale time for active session
  })
}

// Hook to delete a chat session
export function useDeleteChatSession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: deleteSession,
    onSuccess: (_, sessionId) => {
      // Remove from cache
      queryClient.removeQueries({ queryKey: chatKeys.detail(sessionId) })
      // Invalidate list to refetch
      queryClient.invalidateQueries({ queryKey: chatKeys.list() })
    },
  })
}

// Hook to rename a chat session
export function useRenameChatSession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ sessionId, name }: { sessionId: string; name: string }) => {
      // Get current session to preserve messages
      const session = queryClient.getQueryData<ChatSession>(chatKeys.detail(sessionId))
      if (!session) throw new Error('Session not found')
      await updateSession(sessionId, session.messages, name)
      return { sessionId, name }
    },
    onSuccess: ({ sessionId }) => {
      queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: chatKeys.list() })
    },
  })
}

// Hook to generate a title for a chat session
export function useGenerateChatTitle() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (sessionId: string) => {
      const session = queryClient.getQueryData<ChatSession>(chatKeys.detail(sessionId))
      if (!session || session.messages.length === 0) {
        throw new Error('Session not found or empty')
      }
      const result = await generateChatSessionTitle(session.messages)
      if (!result.title) throw new Error('Failed to generate title')
      // Update the session with the new title
      await updateSession(sessionId, session.messages, result.title)
      return { sessionId, title: result.title }
    },
    onSuccess: ({ sessionId }) => {
      queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: chatKeys.list() })
    },
  })
}

// Streaming state for a chat session
export interface ChatStreamState {
  isStreaming: boolean
  workflowId: string | null
  processingSteps: ProcessingStep[]
  error: string | null
}

// Hook to send a message with streaming support
export function useChatStream(sessionId: string | undefined) {
  const queryClient = useQueryClient()
  const abortControllerRef = useRef<AbortController | null>(null)
  const [streamState, setStreamState] = useState<ChatStreamState>({
    isStreaming: false,
    workflowId: null,
    processingSteps: [],
    error: null,
  })

  // Abort any in-progress stream
  const abort = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
      abortControllerRef.current = null
    }
    setStreamState(prev => ({ ...prev, isStreaming: false }))
  }, [])

  // Send a message
  const sendMessage = useCallback(async (message: string) => {
    if (!sessionId) return

    // Abort any existing stream
    abort()

    // Create new abort controller
    const abortController = new AbortController()
    abortControllerRef.current = abortController

    // Cancel any outgoing refetches so they don't overwrite our optimistic update
    await queryClient.cancelQueries({ queryKey: chatKeys.detail(sessionId) })

    // Get current messages from cache
    const cachedSession = queryClient.getQueryData<ChatSession>(chatKeys.detail(sessionId))
    const currentMessages = cachedSession?.messages ?? []

    // Create user message
    const userMessage: ChatMessage = {
      id: generateMessageId(),
      role: 'user',
      content: message,
    }

    // Create streaming placeholder
    const streamingMessage: ChatMessage = {
      id: generateMessageId(),
      role: 'assistant',
      content: '',
      status: 'streaming',
    }

    // Optimistically update cache with user message + streaming placeholder
    queryClient.setQueryData<ChatSession>(chatKeys.detail(sessionId), (old) => {
      if (!old) {
        // Create new session in cache if it doesn't exist yet
        return {
          id: sessionId,
          messages: [userMessage, streamingMessage],
          createdAt: new Date(),
          updatedAt: new Date(),
        }
      }
      return {
        ...old,
        messages: [...old.messages, userMessage, streamingMessage],
        updatedAt: new Date(),
      }
    })

    // Reset stream state
    setStreamState({
      isStreaming: true,
      workflowId: null,
      processingSteps: [],
      error: null,
    })

    try {
      await sendChatMessageStream(
        message,
        currentMessages,
        {
          onThinking: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: [...prev.processingSteps, { type: 'thinking', content: data.content }],
            }))
          },
          onQueryStarted: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: [...prev.processingSteps, {
                type: 'query',
                question: data.question,
                sql: data.sql,
                status: 'running',
              }],
            }))
          },
          onQueryDone: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: prev.processingSteps.map(step =>
                step.type === 'query' && step.question === data.question
                  ? { ...step, status: data.error ? 'error' : 'completed', rows: data.rows, error: data.error || undefined }
                  : step
              ),
            }))
          },
          onWorkflowStarted: (data) => {
            setStreamState(prev => ({ ...prev, workflowId: data.workflow_id }))
            // Update streaming message with workflow ID
            queryClient.setQueryData<ChatSession>(chatKeys.detail(sessionId), (old) => {
              if (!old) return old
              return {
                ...old,
                messages: old.messages.map(m =>
                  m.status === 'streaming' ? { ...m, workflowId: data.workflow_id } : m
                ),
              }
            })
          },
          onDone: () => {
            // Invalidate to refetch fresh data from server
            // The server has already saved the complete message
            queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
            queryClient.invalidateQueries({ queryKey: chatKeys.list() })

            setStreamState({
              isStreaming: false,
              workflowId: null,
              processingSteps: [],
              error: null,
            })

            // Generate title for new sessions
            const session = queryClient.getQueryData<ChatSession>(chatKeys.detail(sessionId))
            if (session && !session.name && session.messages.length <= 3) {
              generateChatSessionTitle(session.messages).then(result => {
                if (result.title) {
                  queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
                  queryClient.invalidateQueries({ queryKey: chatKeys.list() })
                }
              }).catch(() => {})
            }
          },
          onError: (error) => {
            setStreamState(prev => ({
              ...prev,
              isStreaming: false,
              error,
            }))
            // Invalidate to get actual server state
            queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
          },
        },
        abortController.signal,
        sessionId
      )
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        // User aborted, just invalidate to sync with server
        queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
        return
      }
      setStreamState(prev => ({
        ...prev,
        isStreaming: false,
        error: err instanceof Error ? err.message : 'Unknown error',
      }))
      queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
    }
  }, [sessionId, queryClient, abort])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
      }
    }
  }, [])

  return {
    sendMessage,
    abort,
    ...streamState,
  }
}

// Hook to handle workflow reconnection on page load
export function useWorkflowReconnect(
  sessionId: string | undefined,
  messages: ChatMessage[],
  onStreamUpdate: (state: Partial<ChatStreamState>) => void
) {
  const queryClient = useQueryClient()
  const reconnectAttempted = useRef<string | null>(null)
  const abortControllerRef = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!sessionId || reconnectAttempted.current === sessionId) return

    // Find incomplete streaming message
    const streamingMsg = messages.find(m => m.role === 'assistant' && m.status === 'streaming')
    if (!streamingMsg) return

    reconnectAttempted.current = sessionId

    // Check for running workflow
    getLatestWorkflowForSession(sessionId).then(async (workflow) => {
      if (!workflow) {
        // No workflow, invalidate to sync
        queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
        return
      }

      if (workflow.status === 'completed') {
        // Already completed, just refetch
        queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
        return
      }

      if (workflow.status === 'failed' || workflow.status === 'cancelled') {
        // Failed/cancelled, refetch to get error state
        queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
        return
      }

      // Workflow is running, reconnect to stream
      onStreamUpdate({ isStreaming: true, workflowId: workflow.id })

      const abortController = new AbortController()
      abortControllerRef.current = abortController

      reconnectToWorkflow(
        workflow.id,
        {
          onThinking: (data) => {
            onStreamUpdate({
              processingSteps: [{ type: 'thinking', content: data.content }],
            })
          },
          onQueryDone: (data) => {
            onStreamUpdate({
              processingSteps: [{
                type: 'query',
                question: data.question,
                sql: data.sql,
                status: data.error ? 'error' : 'completed',
                rows: data.rows,
                error: data.error || undefined,
              }],
            })
          },
          onDone: () => {
            onStreamUpdate({ isStreaming: false, workflowId: null, processingSteps: [] })
            queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
            queryClient.invalidateQueries({ queryKey: chatKeys.list() })
          },
          onError: (error) => {
            onStreamUpdate({ isStreaming: false, error })
            queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
          },
          onRetry: () => {
            // Workflow running on another server, poll for completion
            const pollInterval = setInterval(async () => {
              try {
                const updated = await getLatestWorkflowForSession(sessionId)
                if (updated?.status === 'completed' || updated?.status === 'failed') {
                  clearInterval(pollInterval)
                  onStreamUpdate({ isStreaming: false, workflowId: null })
                  queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
                }
              } catch {
                // Ignore poll errors
              }
            }, 2000)

            // Stop polling after 2 minutes
            setTimeout(() => clearInterval(pollInterval), 120000)
          },
        },
        abortController.signal
      )
    }).catch(() => {
      // Failed to check workflow, just sync
      queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
    })

    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
      }
    }
  }, [sessionId, messages, queryClient, onStreamUpdate])
}
