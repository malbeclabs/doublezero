import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback, useRef, useState, useEffect } from 'react'
import type { ChatMessage, ProcessingStep } from '@/lib/api'
import { useAuth } from '@/contexts/AuthContext'
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
  const { refreshAuth } = useAuth()
  const abortControllerRef = useRef<AbortController | null>(null)
  const [streamState, setStreamState] = useState<ChatStreamState>({
    isStreaming: false,
    workflowId: null,
    processingSteps: [],
    error: null,
  })

  // Reset stream state when sessionId changes (e.g., navigating to new chat)
  const prevSessionIdRef = useRef(sessionId)
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId
      // Abort any in-progress stream from previous session
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
        abortControllerRef.current = null
      }
      setStreamState({
        isStreaming: false,
        workflowId: null,
        processingSteps: [],
        error: null,
      })
    }
  }, [sessionId])

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
    await Promise.all([
      queryClient.cancelQueries({ queryKey: chatKeys.detail(sessionId) }),
      queryClient.cancelQueries({ queryKey: chatKeys.list() }),
    ])

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
    const updatedSession = queryClient.setQueryData<ChatSession>(chatKeys.detail(sessionId), (old) => {
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

    // Also update the sessions list cache so the sidebar shows this session immediately
    if (updatedSession) {
      queryClient.setQueryData<ChatSession[]>(chatKeys.list(), (oldList) => {
        if (!oldList) return [updatedSession]
        const existingIndex = oldList.findIndex(s => s.id === sessionId)
        if (existingIndex >= 0) {
          // Update existing session in list
          const newList = [...oldList]
          newList[existingIndex] = updatedSession
          return newList
        }
        // Add new session to list
        return [updatedSession, ...oldList]
      })
    }

    // Reset stream state
    setStreamState({
      isStreaming: true,
      workflowId: null,
      processingSteps: [],
      error: null,
    })

    // Refresh auth to update quota immediately (don't await - fire and forget)
    refreshAuth()

    try {
      await sendChatMessageStream(
        message,
        currentMessages,
        {
          onThinking: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: [...prev.processingSteps, { type: 'thinking', id: data.id, content: data.content }],
            }))
          },
          // SQL query events
          onSqlStarted: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: [...prev.processingSteps, {
                type: 'sql_query',
                id: data.id,
                question: data.question,
                sql: data.sql,
                status: 'running',
              }],
            }))
          },
          onSqlDone: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: prev.processingSteps.map(step =>
                step.type === 'sql_query' && step.id === data.id
                  ? { ...step, status: data.error ? 'error' : 'completed', rows: data.rows, error: data.error || undefined }
                  : step
              ),
            }))
          },
          // Cypher query events
          onCypherStarted: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: [...prev.processingSteps, {
                type: 'cypher_query',
                id: data.id,
                question: data.question,
                cypher: data.cypher,
                status: 'running',
              }],
            }))
          },
          onCypherDone: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: prev.processingSteps.map(step =>
                step.type === 'cypher_query' && step.id === data.id
                  ? { ...step, status: data.error ? 'error' : 'completed', rows: data.rows, error: data.error || undefined }
                  : step
              ),
            }))
          },
          // ReadDocs events
          onReadDocsStarted: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: [...prev.processingSteps, {
                type: 'read_docs',
                id: data.id,
                page: data.page,
                status: 'running',
              }],
            }))
          },
          onReadDocsDone: (data) => {
            setStreamState(prev => ({
              ...prev,
              processingSteps: prev.processingSteps.map(step =>
                step.type === 'read_docs' && step.id === data.id
                  ? { ...step, status: data.error ? 'error' : 'completed', content: data.content, error: data.error || undefined }
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
          onDone: async () => {
            // Fetch fresh data from server and update cache before clearing streaming state
            // This ensures the complete message is in the cache before we hide the streaming UI
            try {
              const freshSession = await getSession<ChatMessage[]>(sessionId)
              const converted = serverToChatSession(freshSession)

              // Update cache and clear streaming state together
              // React will batch these updates into one render
              // Note: We don't clear processingSteps here - they're kept around so the UI
              // can show them until the message's own workflowData takes over. This prevents
              // a flash when transitioning from streaming to complete state.
              queryClient.setQueryData<ChatSession>(chatKeys.detail(sessionId), converted)
              setStreamState(prev => ({
                isStreaming: false,
                workflowId: null,
                processingSteps: prev.processingSteps, // Keep steps for smooth transition
                error: null,
              }))

              queryClient.invalidateQueries({ queryKey: chatKeys.list() })
              refreshAuth()

              // Generate title for new sessions
              if (!converted.name && converted.messages.length <= 3) {
                generateChatSessionTitle(converted.messages).then(result => {
                  if (result.title) {
                    queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
                    queryClient.invalidateQueries({ queryKey: chatKeys.list() })
                  }
                }).catch(() => {})
              }
            } catch {
              // If fetch fails, just invalidate to trigger a refetch
              queryClient.invalidateQueries({ queryKey: chatKeys.detail(sessionId) })
              queryClient.invalidateQueries({ queryKey: chatKeys.list() })
              setStreamState({
                isStreaming: false,
                workflowId: null,
                processingSteps: [],
                error: null,
              })
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
      // Initialize with existing steps from the message (if any were saved before disconnect)
      const existingSteps = streamingMsg.workflowData?.processingSteps ?? []
      onStreamUpdate({ isStreaming: true, workflowId: workflow.id, processingSteps: existingSteps })

      const abortController = new AbortController()
      abortControllerRef.current = abortController

      reconnectToWorkflow(
        workflow.id,
        {
          onThinking: (data) => {
            onStreamUpdate({
              processingSteps: [{ type: 'thinking', id: data.id, content: data.content }],
            })
          },
          onSqlDone: (data) => {
            onStreamUpdate({
              processingSteps: [{
                type: 'sql_query',
                id: data.id,
                question: data.question,
                sql: data.sql,
                status: data.error ? 'error' : 'completed',
                rows: data.rows,
                error: data.error || undefined,
              }],
            })
          },
          onCypherDone: (data) => {
            onStreamUpdate({
              processingSteps: [{
                type: 'cypher_query',
                id: data.id,
                question: data.question,
                cypher: data.cypher,
                status: data.error ? 'error' : 'completed',
                rows: data.rows,
                error: data.error || undefined,
              }],
            })
          },
          onReadDocsDone: (data) => {
            onStreamUpdate({
              processingSteps: [{
                type: 'read_docs',
                id: data.id,
                page: data.page,
                status: data.error ? 'error' : 'completed',
                content: data.content,
                error: data.error || undefined,
              }],
            })
          },
          onDone: async () => {
            // Refetch session data first, then clear streaming state
            // This prevents a flash of empty content between stream end and refetch
            await Promise.all([
              queryClient.refetchQueries({ queryKey: chatKeys.detail(sessionId) }),
              queryClient.invalidateQueries({ queryKey: chatKeys.list() }),
            ])
            onStreamUpdate({ isStreaming: false, workflowId: null, processingSteps: [] })
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
