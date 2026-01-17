import type { GenerationRecord } from '@/components/session-history'
import type { ChatMessage } from './api'
import { ensureMessageId } from './api'

export interface QuerySession {
  id: string
  createdAt: Date
  updatedAt: Date
  name?: string
  history: GenerationRecord[]
}

export interface ChatSession {
  id: string
  createdAt: Date
  updatedAt: Date
  name?: string
  messages: ChatMessage[]
}

const SESSIONS_KEY = 'lake-query-sessions'
const CURRENT_SESSION_KEY = 'lake-current-session-id'
const CHAT_SESSIONS_KEY = 'lake-chat-sessions'
const CURRENT_CHAT_SESSION_KEY = 'lake-current-chat-session-id'

export function loadSessions(): QuerySession[] {
  try {
    const raw = localStorage.getItem(SESSIONS_KEY)
    if (!raw) return []
    const sessions = JSON.parse(raw) as QuerySession[]
    // Convert date strings back to Date objects
    // Handle missing dates from old localStorage data
    const result = sessions.map(s => {
      const createdAt = s.createdAt ? new Date(s.createdAt) : new Date(0)
      const updatedAt = s.updatedAt ? new Date(s.updatedAt) : createdAt
      return {
        ...s,
        createdAt,
        updatedAt,
        history: s.history.map(h => ({
          ...h,
          timestamp: new Date(h.timestamp),
        })),
      }
    })

    // Sort by updatedAt descending, with id as tiebreaker for stable ordering
    result.sort((a, b) => {
      const timeDiff = b.updatedAt.getTime() - a.updatedAt.getTime()
      return timeDiff !== 0 ? timeDiff : a.id.localeCompare(b.id)
    })

    return result
  } catch {
    return []
  }
}

export function saveSessions(sessions: QuerySession[]): void {
  try {
    localStorage.setItem(SESSIONS_KEY, JSON.stringify(sessions))
  } catch (e) {
    // localStorage quota exceeded - prune oldest sessions and retry
    if (e instanceof DOMException && e.name === 'QuotaExceededError') {
      console.warn('[Sessions] localStorage quota exceeded, pruning old query sessions')
      const pruned = [...sessions]
        .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())
        .slice(0, 50)
      try {
        localStorage.setItem(SESSIONS_KEY, JSON.stringify(pruned))
      } catch {
        console.error('[Sessions] Failed to save query sessions even after pruning')
      }
    }
  }
}

export function loadCurrentSessionId(): string | null {
  return localStorage.getItem(CURRENT_SESSION_KEY)
}

export function saveCurrentSessionId(id: string): void {
  localStorage.setItem(CURRENT_SESSION_KEY, id)
}

export function createSession(): QuerySession {
  return {
    id: crypto.randomUUID(),
    createdAt: new Date(),
    updatedAt: new Date(),
    history: [],
  }
}

export function createSessionWithId(id: string): QuerySession {
  return {
    id,
    createdAt: new Date(),
    updatedAt: new Date(),
    history: [],
  }
}

export function getSessionPreview(session: QuerySession): string {
  if (session.name) return session.name
  if (session.history.length === 0) return 'Empty session'

  // Get first generation prompt or first manual edit
  const firstRecord = session.history[session.history.length - 1]
  if (firstRecord.type === 'generation' && firstRecord.prompt) {
    return firstRecord.prompt.slice(0, 50) + (firstRecord.prompt.length > 50 ? '...' : '')
  }
  return 'Manual queries'
}

export function formatSessionDate(date: Date): string {
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'Just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`

  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// Chat session functions
export function loadChatSessions(): ChatSession[] {
  try {
    const raw = localStorage.getItem(CHAT_SESSIONS_KEY)
    if (!raw) return []
    const sessions = JSON.parse(raw) as ChatSession[]

    // Debug: check for streaming messages
    for (const s of sessions) {
      const hasStreaming = s.messages?.some((m: { status?: string }) => m.status === 'streaming')
      if (hasStreaming) {
        console.log('[Sessions] Loaded session with streaming from localStorage:', s.id, 'msgCount:', s.messages?.length)
      }
    }

    // Convert date strings back to Date objects
    // Handle missing dates from old localStorage data
    const result = sessions.map(s => {
      const createdAt = s.createdAt ? new Date(s.createdAt) : new Date(0)
      const updatedAt = s.updatedAt ? new Date(s.updatedAt) : createdAt
      return {
        ...s,
        createdAt,
        updatedAt,
        // Ensure all messages have IDs (migration for old data)
        messages: s.messages?.map(ensureMessageId) ?? [],
      }
    })

    // Sort by updatedAt descending, with id as tiebreaker for stable ordering
    result.sort((a, b) => {
      const timeDiff = b.updatedAt.getTime() - a.updatedAt.getTime()
      return timeDiff !== 0 ? timeDiff : a.id.localeCompare(b.id)
    })

    return result
  } catch {
    return []
  }
}

export function saveChatSessions(sessions: ChatSession[]): void {
  try {
    localStorage.setItem(CHAT_SESSIONS_KEY, JSON.stringify(sessions))
  } catch (e) {
    // localStorage quota exceeded - prune oldest sessions and retry
    if (e instanceof DOMException && e.name === 'QuotaExceededError') {
      console.warn('[Sessions] localStorage quota exceeded, pruning old sessions')
      // Keep only the 50 most recent sessions
      const pruned = [...sessions]
        .sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())
        .slice(0, 50)
      try {
        localStorage.setItem(CHAT_SESSIONS_KEY, JSON.stringify(pruned))
      } catch {
        // If still failing, just log and continue - server has the data
        console.error('[Sessions] Failed to save sessions even after pruning')
      }
    }
  }
}

export function loadCurrentChatSessionId(): string | null {
  return localStorage.getItem(CURRENT_CHAT_SESSION_KEY)
}

export function saveCurrentChatSessionId(id: string): void {
  localStorage.setItem(CURRENT_CHAT_SESSION_KEY, id)
}

export function createChatSession(): ChatSession {
  return {
    id: crypto.randomUUID(),
    createdAt: new Date(),
    updatedAt: new Date(),
    messages: [],
  }
}

export function createChatSessionWithId(id: string): ChatSession {
  return {
    id,
    createdAt: new Date(),
    updatedAt: new Date(),
    messages: [],
  }
}

export function getChatSessionPreview(session: ChatSession): string {
  if (session.name) return session.name
  if (session.messages.length === 0) return 'Empty chat'

  // Get first user message
  const firstUserMsg = session.messages.find(m => m.role === 'user')
  if (firstUserMsg) {
    return firstUserMsg.content.slice(0, 50) + (firstUserMsg.content.length > 50 ? '...' : '')
  }
  return 'Chat session'
}

// Browser ID for server-side lock coordination
// Persisted in localStorage so it survives page refreshes
const BROWSER_ID_KEY = 'lake-browser-id'

function getBrowserId(): string {
  let id = localStorage.getItem(BROWSER_ID_KEY)
  if (!id) {
    id = crypto.randomUUID()
    localStorage.setItem(BROWSER_ID_KEY, id)
  }
  return id
}

export const BROWSER_ID = getBrowserId()
