import React, { useCallback, useEffect, useRef } from 'react'
import type { GenerationRecord } from '@/components/session-history'
import type { ChatMessage, ServerSession } from './api'
import { ensureMessageId } from './api'
import type { QuerySession, ChatSession } from './sessions'
import * as api from './api'

// Debounce delay for syncing sessions to server
const SYNC_DEBOUNCE_MS = 500

// Key for tracking if migration has been done
const MIGRATION_KEY = 'lake-sessions-migrated'

// Debounce helper
function debounce<T extends (...args: Parameters<T>) => void>(
  fn: T,
  ms: number
): T & { cancel: () => void } {
  let timeoutId: ReturnType<typeof setTimeout> | null = null
  const debounced = ((...args: Parameters<T>) => {
    if (timeoutId) clearTimeout(timeoutId)
    timeoutId = setTimeout(() => fn(...args), ms)
  }) as T & { cancel: () => void }
  debounced.cancel = () => {
    if (timeoutId) clearTimeout(timeoutId)
  }
  return debounced
}

// Convert server session to local QuerySession format
function serverToQuerySession(server: ServerSession<GenerationRecord[]>): QuerySession {
  return {
    id: server.id,
    name: server.name ?? undefined,
    createdAt: new Date(server.created_at),
    updatedAt: new Date(server.updated_at),
    history: server.content.map(record => ({
      ...record,
      timestamp: new Date(record.timestamp),
    })),
  }
}

// Convert server session to local ChatSession format
function serverToChatSession(server: ServerSession<ChatMessage[]>): ChatSession {
  return {
    id: server.id,
    name: server.name ?? undefined,
    createdAt: new Date(server.created_at),
    updatedAt: new Date(server.updated_at),
    // Ensure all messages have IDs (migration for old data)
    messages: server.content.map(ensureMessageId),
  }
}

// Check if a session has a streaming message (for ChatSession type)
function hasStreamingMessage(session: unknown): boolean {
  if (typeof session === 'object' && session !== null && 'messages' in session) {
    const messages = (session as { messages: Array<{ status?: string }> }).messages
    return messages.some(m => m.status === 'streaming')
  }
  return false
}

// Merge local and server sessions
// - Local ALWAYS wins if it has a streaming message (critical for resume)
// - Local wins if it has more messages (server might have stale data)
// - Otherwise, newer wins based on updatedAt
function mergeSessions<T extends { id: string; updatedAt: Date }>(
  local: T[],
  server: T[]
): T[] {
  console.log('[Sync] Merging sessions:', { localCount: local.length, serverCount: server.length })

  const serverMap = new Map(server.map(s => [s.id, s]))
  const localMap = new Map(local.map(s => [s.id, s]))
  const merged: T[] = []

  // Process all unique session IDs
  const allIds = new Set([...serverMap.keys(), ...localMap.keys()])
  for (const id of allIds) {
    const serverSession = serverMap.get(id)
    const localSession = localMap.get(id)

    // Only include sessions that exist locally - server is for backup, not discovery
    if (!localSession) {
      continue
    }

    if (serverSession) {
      // Both exist - check various conditions for which to use
      const localHasStreaming = hasStreamingMessage(localSession)
      const localMsgCount = getMessageCount(localSession)
      const serverMsgCount = getMessageCount(serverSession)

      if (localHasStreaming) {
        // Local has streaming - ALWAYS use local (critical for resume)
        merged.push(localSession)
      } else if (localMsgCount > serverMsgCount) {
        // Local has more messages - use local (server is stale)
        merged.push(localSession)
      } else if (localSession.updatedAt.getTime() > serverSession.updatedAt.getTime()) {
        merged.push(localSession)
      } else {
        merged.push(serverSession)
      }
    } else {
      // Local only - keep it (will sync to server)
      merged.push(localSession)
    }
  }

  // Sort by updatedAt descending, with id as tiebreaker for stable ordering
  merged.sort((a, b) => {
    const timeDiff = b.updatedAt.getTime() - a.updatedAt.getTime()
    return timeDiff !== 0 ? timeDiff : a.id.localeCompare(b.id)
  })

  return merged
}

// Get message count from a session (for ChatSession type)
function getMessageCount(session: unknown): number {
  if (typeof session === 'object' && session !== null && 'messages' in session) {
    return (session as { messages: unknown[] }).messages.length
  }
  return 0
}

// Hook for syncing query sessions
export function useQuerySessionSync(
  onSessionsUpdated: React.Dispatch<React.SetStateAction<QuerySession[]>>,
  enabled = true
): [(session: QuerySession) => void, boolean] {
  const syncingRef = useRef<Set<string>>(new Set())
  const pendingRef = useRef<Map<string, QuerySession>>(new Map())
  const initialLoadDone = useRef(false)
  const [serverSyncComplete, setServerSyncComplete] = React.useState(false)

  // Internal sync function that handles pending updates
  const doSync = useCallback(async (session: QuerySession) => {
    if (syncingRef.current.has(session.id)) {
      // Already syncing - queue this update
      pendingRef.current.set(session.id, session)
      return
    }
    syncingRef.current.add(session.id)

    try {
      await api.upsertSession(
        session.id,
        'query',
        session.history,
        session.name
      )
    } catch (err) {
      console.error('[Sync] Failed to sync query session:', session.id, err)
    } finally {
      syncingRef.current.delete(session.id)
      // Check if there's a pending update
      const pending = pendingRef.current.get(session.id)
      if (pending) {
        pendingRef.current.delete(session.id)
        doSync(pending) // Sync the pending update
      }
    }
  }, [])

  // Create debounced sync function
  const syncSession = useCallback(
    debounce((session: QuerySession) => {
      doSync(session)
    }, SYNC_DEBOUNCE_MS),
    [doSync]
  )

  // Load sessions from server on mount (only when enabled becomes true)
  useEffect(() => {
    if (!enabled || initialLoadDone.current) return
    initialLoadDone.current = true // Set immediately to prevent race conditions

    const loadFromServer = async (localSessions: QuerySession[]) => {
      try {
        // Only fetch sessions that exist locally (max 30 most recent)
        const localIds = localSessions
          .slice(0, 30)
          .map(s => s.id)

        if (localIds.length === 0) {
          setServerSyncComplete(true)
          return
        }

        const response = await api.batchGetSessions<GenerationRecord[]>(localIds)
        if (response.sessions.length === 0) {
          setServerSyncComplete(true)
          return
        }

        const serverSessions = response.sessions.map(serverToQuerySession)
        // Merge server data with local - local sessions are already in state
        onSessionsUpdated(prev => mergeSessions(prev, serverSessions))
        setServerSyncComplete(true)
      } catch (err) {
        console.error('[Sync] Failed to load query sessions from server:', err)
        setServerSyncComplete(true) // Mark complete even on error to unblock UI
      }
    }

    // Get current local sessions and pass to loader
    onSessionsUpdated(prev => {
      loadFromServer(prev)
      return prev // Don't modify state
    })
  }, [enabled]) // Only depend on enabled

  // Return sync function and server sync status
  return [syncSession, serverSyncComplete]
}

// Sync functions type - debounced and immediate
export interface ChatSyncFunctions {
  sync: (session: ChatSession) => void
  syncImmediate: (session: ChatSession) => void
}

// Hook for syncing chat sessions
export function useChatSessionSync(
  onSessionsUpdated: React.Dispatch<React.SetStateAction<ChatSession[]>>,
  enabled = true
): [ChatSyncFunctions, boolean] {
  const syncingRef = useRef<Set<string>>(new Set())
  const pendingRef = useRef<Map<string, ChatSession>>(new Map())
  const initialLoadDone = useRef(false)
  const [serverSyncComplete, setServerSyncComplete] = React.useState(false)

  // Internal sync function that handles pending updates
  const doSync = useCallback(async (session: ChatSession) => {
    if (syncingRef.current.has(session.id)) {
      // Already syncing - queue this update
      pendingRef.current.set(session.id, session)
      return
    }
    syncingRef.current.add(session.id)

    try {
      await api.upsertSession(
        session.id,
        'chat',
        session.messages,
        session.name
      )
    } catch (err) {
      console.error('[Sync] Failed to sync chat session:', session.id, err)
    } finally {
      syncingRef.current.delete(session.id)
      // Check if there's a pending update
      const pending = pendingRef.current.get(session.id)
      if (pending) {
        pendingRef.current.delete(session.id)
        doSync(pending) // Sync the pending update
      }
    }
  }, [])

  // Create debounced sync function
  const syncDebounced = useCallback(
    debounce((session: ChatSession) => {
      doSync(session)
    }, SYNC_DEBOUNCE_MS),
    [doSync]
  )

  // Bundle sync functions together
  const syncFunctions = React.useMemo<ChatSyncFunctions>(() => ({
    sync: syncDebounced,
    syncImmediate: doSync,
  }), [syncDebounced, doSync])

  // Load sessions from server on mount (only when enabled becomes true)
  useEffect(() => {
    if (!enabled || initialLoadDone.current) return
    initialLoadDone.current = true // Set immediately to prevent race conditions

    const loadFromServer = async (localSessions: ChatSession[]) => {
      try {
        // Only fetch sessions that exist locally (max 30 most recent)
        const localIds = localSessions
          .slice(0, 30)
          .map(s => s.id)

        if (localIds.length === 0) {
          setServerSyncComplete(true)
          return
        }

        const response = await api.batchGetSessions<ChatMessage[]>(localIds)
        if (response.sessions.length === 0) {
          setServerSyncComplete(true)
          return
        }

        const serverSessions = response.sessions.map(serverToChatSession)
        // Merge server data with local - local sessions are already in state
        onSessionsUpdated(prev => mergeSessions(prev, serverSessions))
        setServerSyncComplete(true)
      } catch (err) {
        console.error('[Sync] Failed to load chat sessions from server:', err)
        setServerSyncComplete(true) // Mark complete even on error to unblock UI
      }
    }

    // Get current local sessions and pass to loader
    onSessionsUpdated(prev => {
      loadFromServer(prev)
      return prev // Don't modify state
    })
  }, [enabled]) // Only depend on enabled

  // Return sync functions and server sync status
  return [syncFunctions, serverSyncComplete]
}

// Hook for deleting a session from server
export function useSessionDelete() {
  return useCallback(async (id: string) => {
    try {
      await api.deleteSession(id)
    } catch (err) {
      console.error('[Sync] Failed to delete session:', id, err)
    }
  }, [])
}

// One-time migration of localStorage sessions to server
export async function migrateLocalSessions(
  querySessions: QuerySession[],
  chatSessions: ChatSession[]
): Promise<void> {
  const migrated = localStorage.getItem(MIGRATION_KEY)
  if (migrated === 'true') return

  console.log('[Sync] Starting one-time migration of localStorage sessions to server')

  try {
    // Migrate query sessions
    for (const session of querySessions) {
      if (session.history.length > 0) {
        try {
          await api.createSession(session.id, 'query', session.history, session.name)
        } catch (err) {
          // Ignore conflicts (already exists)
          if (!(err instanceof Error && err.message === 'Session already exists')) {
            console.error('[Sync] Failed to migrate query session:', session.id, err)
          }
        }
      }
    }

    // Migrate chat sessions
    for (const session of chatSessions) {
      if (session.messages.length > 0) {
        try {
          await api.createSession(session.id, 'chat', session.messages, session.name)
        } catch (err) {
          // Ignore conflicts (already exists)
          if (!(err instanceof Error && err.message === 'Session already exists')) {
            console.error('[Sync] Failed to migrate chat session:', session.id, err)
          }
        }
      }
    }

    localStorage.setItem(MIGRATION_KEY, 'true')
    console.log('[Sync] Migration completed')
  } catch (err) {
    console.error('[Sync] Migration failed:', err)
    // Don't set migrated flag - will retry next time
  }
}

// Check if there's an incomplete streaming message that needs resuming
export function findIncompleteMessage(messages: ChatMessage[]): {
  userMessage: ChatMessage
  assistantMessage: ChatMessage
  index: number
} | null {
  // Look for the last assistant message with status 'streaming'
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i]
    if (msg.role === 'assistant' && msg.status === 'streaming') {
      // Find the preceding user message
      for (let j = i - 1; j >= 0; j--) {
        if (messages[j].role === 'user') {
          return {
            userMessage: messages[j],
            assistantMessage: msg,
            index: i,
          }
        }
      }
    }
  }
  return null
}
