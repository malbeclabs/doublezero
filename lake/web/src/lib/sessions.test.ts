import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  createSession,
  createSessionWithId,
  createChatSession,
  createChatSessionWithId,
  getSessionPreview,
  getChatSessionPreview,
  formatSessionDate,
  serverToQuerySession,
  serverToChatSession,
  findIncompleteMessage,
  loadCurrentSessionId,
  saveCurrentSessionId,
  type QuerySession,
  type ChatSession,
} from './sessions'
import type { ChatMessage } from './api'

describe('createSession', () => {
  it('creates a session with a UUID', () => {
    const session = createSession()
    expect(session.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
  })

  it('creates a session with empty history', () => {
    const session = createSession()
    expect(session.history).toEqual([])
  })

  it('creates a session with timestamps', () => {
    const before = new Date()
    const session = createSession()
    const after = new Date()

    expect(session.createdAt.getTime()).toBeGreaterThanOrEqual(before.getTime())
    expect(session.createdAt.getTime()).toBeLessThanOrEqual(after.getTime())
  })
})

describe('createSessionWithId', () => {
  it('creates a session with the given ID', () => {
    const session = createSessionWithId('my-test-id')
    expect(session.id).toBe('my-test-id')
  })

  it('creates a session with empty history', () => {
    const session = createSessionWithId('test')
    expect(session.history).toEqual([])
  })
})

describe('createChatSession', () => {
  it('creates a chat session with a UUID', () => {
    const session = createChatSession()
    expect(session.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
  })

  it('creates a chat session with empty messages', () => {
    const session = createChatSession()
    expect(session.messages).toEqual([])
  })
})

describe('createChatSessionWithId', () => {
  it('creates a chat session with the given ID', () => {
    const session = createChatSessionWithId('my-chat-id')
    expect(session.id).toBe('my-chat-id')
  })
})

describe('getSessionPreview', () => {
  it('returns session name if available', () => {
    const session: QuerySession = {
      id: '1',
      name: 'My Session',
      createdAt: new Date(),
      updatedAt: new Date(),
      history: [],
    }
    expect(getSessionPreview(session)).toBe('My Session')
  })

  it('returns "Empty session" for sessions with no history', () => {
    const session: QuerySession = {
      id: '1',
      createdAt: new Date(),
      updatedAt: new Date(),
      history: [],
    }
    expect(getSessionPreview(session)).toBe('Empty session')
  })

  it('returns truncated prompt for generation records', () => {
    const session: QuerySession = {
      id: '1',
      createdAt: new Date(),
      updatedAt: new Date(),
      history: [
        {
          id: 'rec-1',
          type: 'generation',
          prompt: 'This is a really long prompt that should be truncated at fifty characters for display',
          sql: 'SELECT * FROM users',
          timestamp: new Date(),
        },
      ],
    }
    const preview = getSessionPreview(session)
    expect(preview).toHaveLength(53) // 50 chars + '...'
    expect(preview).toContain('...')
  })

  it('returns short prompts without truncation', () => {
    const session: QuerySession = {
      id: '1',
      createdAt: new Date(),
      updatedAt: new Date(),
      history: [
        {
          id: 'rec-1',
          type: 'generation',
          prompt: 'Short prompt',
          sql: 'SELECT 1',
          timestamp: new Date(),
        },
      ],
    }
    expect(getSessionPreview(session)).toBe('Short prompt')
  })

  it('returns "Manual queries" for manual records', () => {
    const session: QuerySession = {
      id: '1',
      createdAt: new Date(),
      updatedAt: new Date(),
      history: [
        {
          id: 'rec-1',
          type: 'manual',
          sql: 'SELECT 1',
          timestamp: new Date(),
        },
      ],
    }
    expect(getSessionPreview(session)).toBe('Manual queries')
  })
})

describe('getChatSessionPreview', () => {
  it('returns session name if available', () => {
    const session: ChatSession = {
      id: '1',
      name: 'My Chat',
      createdAt: new Date(),
      updatedAt: new Date(),
      messages: [],
    }
    expect(getChatSessionPreview(session)).toBe('My Chat')
  })

  it('returns "Empty chat" for sessions with no messages', () => {
    const session: ChatSession = {
      id: '1',
      createdAt: new Date(),
      updatedAt: new Date(),
      messages: [],
    }
    expect(getChatSessionPreview(session)).toBe('Empty chat')
  })

  it('returns truncated first user message', () => {
    const session: ChatSession = {
      id: '1',
      createdAt: new Date(),
      updatedAt: new Date(),
      messages: [
        {
          id: '1',
          role: 'user',
          content: 'This is a really long question that should be truncated at fifty characters for display purposes',
        },
      ],
    }
    const preview = getChatSessionPreview(session)
    expect(preview).toHaveLength(53) // 50 chars + '...'
  })

  it('finds user message even if assistant messages come first', () => {
    const session: ChatSession = {
      id: '1',
      createdAt: new Date(),
      updatedAt: new Date(),
      messages: [
        { id: '1', role: 'assistant', content: 'Hello!' },
        { id: '2', role: 'user', content: 'Hi there' },
      ],
    }
    expect(getChatSessionPreview(session)).toBe('Hi there')
  })
})

describe('formatSessionDate', () => {
  it('returns "Just now" for very recent dates', () => {
    const date = new Date()
    expect(formatSessionDate(date)).toBe('Just now')
  })

  it('returns minutes ago for recent dates', () => {
    const date = new Date(Date.now() - 5 * 60 * 1000) // 5 minutes ago
    expect(formatSessionDate(date)).toBe('5m ago')
  })

  it('returns hours ago for dates within 24 hours', () => {
    const date = new Date(Date.now() - 3 * 60 * 60 * 1000) // 3 hours ago
    expect(formatSessionDate(date)).toBe('3h ago')
  })

  it('returns days ago for dates within a week', () => {
    const date = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000) // 3 days ago
    expect(formatSessionDate(date)).toBe('3d ago')
  })

  it('returns formatted date for older dates', () => {
    const date = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000) // 30 days ago
    const result = formatSessionDate(date)
    expect(result).not.toContain('ago')
    expect(result).toMatch(/\w+ \d+/) // e.g., "Dec 25"
  })
})

describe('serverToQuerySession', () => {
  it('converts server format to QuerySession', () => {
    const server = {
      id: 'test-id',
      name: 'Test Session',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-02T00:00:00Z',
      content: [
        {
          id: 'rec-1',
          type: 'generation' as const,
          sql: 'SELECT 1',
          timestamp: new Date('2024-01-01T12:00:00Z'),
        },
      ],
    }

    const result = serverToQuerySession(server)

    expect(result.id).toBe('test-id')
    expect(result.name).toBe('Test Session')
    expect(result.createdAt).toBeInstanceOf(Date)
    expect(result.updatedAt).toBeInstanceOf(Date)
    expect(result.history).toHaveLength(1)
  })

  it('converts null name to undefined', () => {
    const server = {
      id: 'test-id',
      name: null,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-02T00:00:00Z',
      content: [],
    }

    const result = serverToQuerySession(server)
    expect(result.name).toBeUndefined()
  })
})

describe('serverToChatSession', () => {
  it('converts server format to ChatSession', () => {
    const server = {
      id: 'chat-id',
      name: 'Test Chat',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-02T00:00:00Z',
      content: [
        { id: '1', role: 'user' as const, content: 'Hello' },
        { id: '2', role: 'assistant' as const, content: 'Hi!' },
      ],
    }

    const result = serverToChatSession(server)

    expect(result.id).toBe('chat-id')
    expect(result.name).toBe('Test Chat')
    expect(result.messages).toHaveLength(2)
  })
})

describe('findIncompleteMessage', () => {
  it('returns null for empty messages', () => {
    expect(findIncompleteMessage([])).toBeNull()
  })

  it('returns null when no streaming messages', () => {
    const messages: ChatMessage[] = [
      { id: '1', role: 'user', content: 'Hello' },
      { id: '2', role: 'assistant', content: 'Hi!' },
    ]
    expect(findIncompleteMessage(messages)).toBeNull()
  })

  it('finds incomplete streaming message', () => {
    const messages: ChatMessage[] = [
      { id: '1', role: 'user', content: 'Hello' },
      { id: '2', role: 'assistant', content: 'Hi', status: 'streaming' },
    ]

    const result = findIncompleteMessage(messages)

    expect(result).not.toBeNull()
    expect(result?.userMessage.id).toBe('1')
    expect(result?.assistantMessage.id).toBe('2')
    expect(result?.index).toBe(1)
  })

  it('finds the last incomplete message when multiple exist', () => {
    const messages: ChatMessage[] = [
      { id: '1', role: 'user', content: 'First' },
      { id: '2', role: 'assistant', content: 'Response 1', status: 'streaming' },
      { id: '3', role: 'user', content: 'Second' },
      { id: '4', role: 'assistant', content: 'Response 2', status: 'streaming' },
    ]

    const result = findIncompleteMessage(messages)

    expect(result?.userMessage.id).toBe('3')
    expect(result?.assistantMessage.id).toBe('4')
    expect(result?.index).toBe(3)
  })

  it('ignores completed assistant messages', () => {
    const messages: ChatMessage[] = [
      { id: '1', role: 'user', content: 'Hello' },
      { id: '2', role: 'assistant', content: 'Hi!', status: 'complete' },
    ]
    expect(findIncompleteMessage(messages)).toBeNull()
  })
})

describe('localStorage functions', () => {
  const testKey = 'lake-current-session-id'
  const mockStorage: Record<string, string> = {}

  beforeEach(() => {
    // Mock localStorage for testing
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => mockStorage[key] ?? null,
      setItem: (key: string, value: string) => { mockStorage[key] = value },
      removeItem: (key: string) => { delete mockStorage[key] },
    })
    delete mockStorage[testKey]
  })

  it('saves and loads session ID', () => {
    saveCurrentSessionId('test-session-123')
    expect(loadCurrentSessionId()).toBe('test-session-123')
  })

  it('returns null when no session ID saved', () => {
    expect(loadCurrentSessionId()).toBeNull()
  })
})
