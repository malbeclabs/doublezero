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

// Current session ID storage (just tracks which session is active, not content)
const CURRENT_SESSION_KEY = 'lake-current-session-id'
const CURRENT_CHAT_SESSION_KEY = 'lake-current-chat-session-id'

export function loadCurrentSessionId(): string | null {
  return localStorage.getItem(CURRENT_SESSION_KEY)
}

export function saveCurrentSessionId(id: string): void {
  localStorage.setItem(CURRENT_SESSION_KEY, id)
}

export function loadCurrentChatSessionId(): string | null {
  return localStorage.getItem(CURRENT_CHAT_SESSION_KEY)
}

export function saveCurrentChatSessionId(id: string): void {
  localStorage.setItem(CURRENT_CHAT_SESSION_KEY, id)
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

// Convert server session to local QuerySession format
export function serverToQuerySession(server: {
  id: string
  name: string | null
  created_at: string
  updated_at: string
  content: GenerationRecord[]
}): QuerySession {
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
export function serverToChatSession(server: {
  id: string
  name: string | null
  created_at: string
  updated_at: string
  content: ChatMessage[]
}): ChatSession {
  return {
    id: server.id,
    name: server.name ?? undefined,
    createdAt: new Date(server.created_at),
    updatedAt: new Date(server.updated_at),
    // Ensure all messages have IDs (migration for old data)
    messages: server.content.map(ensureMessageId),
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
