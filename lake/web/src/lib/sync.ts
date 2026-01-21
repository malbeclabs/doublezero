import React, { useCallback, useEffect, useRef } from 'react'
import type { GenerationRecord } from '@/components/session-history'
import type { ChatMessage } from './api'
import type { QuerySession, ChatSession } from './sessions'
import { serverToQuerySession, serverToChatSession } from './sessions'
import * as api from './api'

// Debounce delay for syncing sessions to server
const SYNC_DEBOUNCE_MS = 500

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

// Query sync functions type
export interface QuerySyncFunctions {
  sync: (session: QuerySession) => void
  refreshFromServer: () => Promise<void>
}

// Hook for syncing query sessions to server
export function useQuerySessionSync(
  onSessionsUpdated: React.Dispatch<React.SetStateAction<QuerySession[]>>,
  enabled = true
): [QuerySyncFunctions, boolean] {
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
  const syncDebounced = useCallback(
    debounce((session: QuerySession) => {
      doSync(session)
    }, SYNC_DEBOUNCE_MS),
    [doSync]
  )

  // Function to refresh sessions from server (e.g., after login)
  const refreshFromServer = useCallback(async () => {
    console.log('[Sync] Refreshing query sessions from server')
    try {
      const response = await api.listSessionsWithContent<GenerationRecord[]>('query', 100)
      const sessions = response.sessions.map(serverToQuerySession)
      onSessionsUpdated(sessions)
    } catch (err) {
      console.error('[Sync] Failed to refresh query sessions from server:', err)
    }
  }, [onSessionsUpdated])

  // Bundle sync functions together
  const syncFunctions = React.useMemo<QuerySyncFunctions>(() => ({
    sync: syncDebounced,
    refreshFromServer,
  }), [syncDebounced, refreshFromServer])

  // Load sessions from server on mount
  useEffect(() => {
    if (!enabled || initialLoadDone.current) return
    initialLoadDone.current = true

    const loadFromServer = async () => {
      try {
        const response = await api.listSessionsWithContent<GenerationRecord[]>('query', 100)
        const sessions = response.sessions.map(serverToQuerySession)
        onSessionsUpdated(sessions)
        setServerSyncComplete(true)
      } catch (err) {
        console.error('[Sync] Failed to load query sessions from server:', err)
        setServerSyncComplete(true) // Mark complete even on error to unblock UI
      }
    }

    loadFromServer()
  }, [enabled, onSessionsUpdated])

  return [syncFunctions, serverSyncComplete]
}

// Sync functions type - debounced and immediate
export interface ChatSyncFunctions {
  sync: (session: ChatSession) => void
  syncImmediate: (session: ChatSession) => void
  refreshFromServer: () => Promise<void>
}

// Hook for syncing chat sessions to server
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

    // Log what we're syncing to help debug message loss
    console.log('[Sync] Syncing session to server:', {
      id: session.id,
      messageCount: session.messages.length,
      lastMessageRole: session.messages[session.messages.length - 1]?.role,
      lastMessageStatus: session.messages[session.messages.length - 1]?.status,
    })
    console.trace('[Sync] Stack trace for sync')

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

  // Function to refresh sessions from server (e.g., after login)
  const refreshFromServer = useCallback(async () => {
    console.log('[Sync] Refreshing chat sessions from server')
    try {
      const response = await api.listSessionsWithContent<ChatMessage[]>('chat', 100)
      const sessions = response.sessions.map(serverToChatSession)
      onSessionsUpdated(sessions)
    } catch (err) {
      console.error('[Sync] Failed to refresh chat sessions from server:', err)
    }
  }, [onSessionsUpdated])

  // Bundle sync functions together
  const syncFunctions = React.useMemo<ChatSyncFunctions>(() => ({
    sync: syncDebounced,
    syncImmediate: doSync,
    refreshFromServer,
  }), [syncDebounced, doSync, refreshFromServer])

  // Load sessions from server on mount
  useEffect(() => {
    if (!enabled || initialLoadDone.current) return
    initialLoadDone.current = true

    const loadFromServer = async () => {
      try {
        const response = await api.listSessionsWithContent<ChatMessage[]>('chat', 100)
        const sessions = response.sessions.map(serverToChatSession)
        console.log('[Sync] Loaded chat sessions from server:', sessions.map(s => ({ id: s.id, messageCount: s.messages.length })))
        onSessionsUpdated(sessions)
        setServerSyncComplete(true)
      } catch (err) {
        console.error('[Sync] Failed to load chat sessions from server:', err)
        setServerSyncComplete(true) // Mark complete even on error to unblock UI
      }
    }

    loadFromServer()
  }, [enabled, onSessionsUpdated])

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
