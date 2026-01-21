import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { GenerationRecord } from '@/components/session-history'
import {
  listSessionsWithContent,
  getSession,
  createSession,
  deleteSession,
  updateSession,
  generateSessionTitle,
  type SessionQueryInfo,
} from '@/lib/api'
import { serverToQuerySession, type QuerySession } from '@/lib/sessions'

// Query keys
export const querySessionKeys = {
  all: ['query-sessions'] as const,
  lists: () => [...querySessionKeys.all, 'list'] as const,
  list: () => [...querySessionKeys.lists()] as const,
  details: () => [...querySessionKeys.all, 'detail'] as const,
  detail: (id: string) => [...querySessionKeys.details(), id] as const,
}

// Hook to list all query sessions
export function useQuerySessions() {
  return useQuery({
    queryKey: querySessionKeys.list(),
    queryFn: async () => {
      const response = await listSessionsWithContent<GenerationRecord[]>('query', 100)
      return response.sessions.map(serverToQuerySession)
    },
    staleTime: 30 * 1000, // Consider data fresh for 30 seconds
  })
}

// Hook to get a single query session
export function useQuerySession(sessionId: string | undefined) {
  return useQuery({
    queryKey: querySessionKeys.detail(sessionId ?? ''),
    queryFn: async () => {
      if (!sessionId) return null
      const session = await getSession<GenerationRecord[]>(sessionId)
      return serverToQuerySession(session)
    },
    enabled: !!sessionId,
    staleTime: 10 * 1000, // Shorter stale time for active session
  })
}

// Hook to delete a query session
export function useDeleteQuerySession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: deleteSession,
    onSuccess: (_, sessionId) => {
      // Remove from cache
      queryClient.removeQueries({ queryKey: querySessionKeys.detail(sessionId) })
      // Invalidate list to refetch
      queryClient.invalidateQueries({ queryKey: querySessionKeys.list() })
    },
  })
}

// Hook to rename a query session
export function useRenameQuerySession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ sessionId, name }: { sessionId: string; name: string }) => {
      // Get current session to preserve history
      const session = queryClient.getQueryData<QuerySession>(querySessionKeys.detail(sessionId))
      if (!session) throw new Error('Session not found')
      await updateSession(sessionId, session.history, name)
      return { sessionId, name }
    },
    onSuccess: ({ sessionId }) => {
      queryClient.invalidateQueries({ queryKey: querySessionKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: querySessionKeys.list() })
    },
  })
}

// Hook to generate a title for a query session
export function useGenerateQueryTitle() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (sessionId: string) => {
      const session = queryClient.getQueryData<QuerySession>(querySessionKeys.detail(sessionId))
      if (!session || session.history.length === 0) {
        throw new Error('Session not found or empty')
      }

      // Include both generated queries (with prompts) and manual queries (SQL only)
      const queries: SessionQueryInfo[] = session.history
        .filter(h => h.sql) // Must have SQL
        .map(h => ({ prompt: h.prompt || '', sql: h.sql }))
        .slice(0, 3)

      if (queries.length === 0) throw new Error('No queries to generate title from')

      const result = await generateSessionTitle(queries)
      if (!result.title) throw new Error('Failed to generate title')

      // Update the session with the new title
      await updateSession(sessionId, session.history, result.title)
      return { sessionId, title: result.title }
    },
    onSuccess: ({ sessionId }) => {
      queryClient.invalidateQueries({ queryKey: querySessionKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: querySessionKeys.list() })
    },
  })
}

// Hook to create a new query session
export function useCreateQuerySession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (sessionId?: string) => {
      const id = sessionId ?? crypto.randomUUID()
      await createSession(id, 'query', [])
      return id
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: querySessionKeys.list() })
    },
  })
}

// Hook to add a history record to a query session
export function useAddQueryHistory() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ sessionId, record }: { sessionId: string; record: GenerationRecord }) => {
      // Fetch current session (may not be in cache if just created)
      let session = queryClient.getQueryData<QuerySession>(querySessionKeys.detail(sessionId))
      if (!session) {
        // Try fetching from server
        const serverSession = await getSession<GenerationRecord[]>(sessionId)
        session = serverToQuerySession(serverSession)
      }

      const newHistory = [record, ...session.history]
      await updateSession(sessionId, newHistory, session.name)
      return { sessionId, record }
    },
    onMutate: async ({ sessionId, record }) => {
      // Cancel any outgoing refetches
      await queryClient.cancelQueries({ queryKey: querySessionKeys.detail(sessionId) })

      // Snapshot previous value
      const previousSession = queryClient.getQueryData<QuerySession>(querySessionKeys.detail(sessionId))

      // Optimistically update
      if (previousSession) {
        queryClient.setQueryData<QuerySession>(querySessionKeys.detail(sessionId), {
          ...previousSession,
          history: [record, ...previousSession.history],
          updatedAt: new Date(),
        })
      }

      return { previousSession }
    },
    onError: (_, { sessionId }, context) => {
      // Rollback on error
      if (context?.previousSession) {
        queryClient.setQueryData(querySessionKeys.detail(sessionId), context.previousSession)
      }
    },
    onSettled: (_, __, { sessionId }) => {
      // Refetch to ensure we're in sync
      queryClient.invalidateQueries({ queryKey: querySessionKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: querySessionKeys.list() })
    },
  })
}

// Hook to update a query session's title (without fetching from cache first)
export function useUpdateQueryTitle() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ sessionId, title }: { sessionId: string; title: string }) => {
      // Fetch current session
      let session = queryClient.getQueryData<QuerySession>(querySessionKeys.detail(sessionId))
      if (!session) {
        const serverSession = await getSession<GenerationRecord[]>(sessionId)
        session = serverToQuerySession(serverSession)
      }

      await updateSession(sessionId, session.history, title)
      return { sessionId, title }
    },
    onSuccess: ({ sessionId }) => {
      queryClient.invalidateQueries({ queryKey: querySessionKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: querySessionKeys.list() })
    },
  })
}
