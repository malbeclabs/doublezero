export interface TableInfo {
  name: string
  database: string
  engine: string
  type: 'table' | 'view'
  columns?: string[]
}

export interface CatalogResponse {
  tables: TableInfo[]
}

export interface QueryResponse {
  columns: string[]
  rows: unknown[][]
  row_count: number
  elapsed_ms: number
  error?: string
}

// Cypher query response uses map rows instead of array rows
export interface CypherQueryResponse {
  columns: string[]
  rows: Record<string, unknown>[]
  row_count: number
  elapsed_ms: number
  error?: string
}

export type QueryMode = 'sql' | 'cypher' | 'auto'

// Auth token storage key
const AUTH_TOKEN_KEY = 'lake_auth_token'
const ANONYMOUS_ID_KEY = 'lake_anonymous_id'

// Get the auth token from localStorage
export function getAuthToken(): string | null {
  return localStorage.getItem(AUTH_TOKEN_KEY)
}

// Set the auth token in localStorage
export function setAuthToken(token: string): void {
  localStorage.setItem(AUTH_TOKEN_KEY, token)
}

// Clear the auth token from localStorage
export function clearAuthToken(): void {
  localStorage.removeItem(AUTH_TOKEN_KEY)
}

// Get or generate an anonymous ID for unauthenticated users
export function getAnonymousId(): string {
  let id = localStorage.getItem(ANONYMOUS_ID_KEY)
  if (!id) {
    id = crypto.randomUUID()
    localStorage.setItem(ANONYMOUS_ID_KEY, id)
  }
  return id
}

// Get auth headers if token is present
function getAuthHeaders(): Record<string, string> {
  const token = getAuthToken()
  if (token) {
    return { Authorization: `Bearer ${token}` }
  }
  return {}
}

// Retry configuration
const RETRY_CONFIG = {
  maxRetries: 3,
  baseDelayMs: 500,
  maxDelayMs: 5000,
}

// Check if an error is retryable (network errors, 502/503/504)
function isRetryableError(error: unknown, status?: number): boolean {
  // Network errors (fetch failed)
  if (error instanceof TypeError && error.message.includes('fetch')) {
    return true
  }
  // Server temporarily unavailable
  if (status && [502, 503, 504].includes(status)) {
    return true
  }
  return false
}

// Sleep helper
const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

// Retry wrapper for fetch calls with automatic auth headers
async function fetchWithRetry(
  url: string,
  options?: RequestInit,
  config = RETRY_CONFIG
): Promise<Response> {
  let lastError: unknown
  let lastStatus: number | undefined

  // Add auth headers to all requests
  const authHeaders = getAuthHeaders()
  const mergedOptions: RequestInit = {
    ...options,
    headers: {
      ...authHeaders,
      ...options?.headers,
    },
  }

  for (let attempt = 0; attempt <= config.maxRetries; attempt++) {
    try {
      const res = await fetch(url, mergedOptions)

      // Don't retry on successful responses or client errors (4xx)
      if (res.ok || (res.status >= 400 && res.status < 500)) {
        return res
      }

      // Server error - might be retryable
      lastStatus = res.status
      if (!isRetryableError(null, res.status) || attempt === config.maxRetries) {
        return res
      }
    } catch (err) {
      lastError = err
      // If not retryable or last attempt, throw
      if (!isRetryableError(err) || attempt === config.maxRetries) {
        throw err
      }
    }

    // Exponential backoff with jitter
    const delay = Math.min(
      config.baseDelayMs * Math.pow(2, attempt) + Math.random() * 100,
      config.maxDelayMs
    )
    await sleep(delay)
  }

  // Should not reach here, but handle edge case
  if (lastError) throw lastError
  throw new Error(`Request failed with status ${lastStatus}`)
}

export async function fetchCatalog(): Promise<CatalogResponse> {
  const res = await fetchWithRetry('/api/catalog')
  if (!res.ok) {
    throw new Error('Failed to fetch catalog')
  }
  return res.json()
}

// SQL query execution (uses new /api/sql/query endpoint)
export async function executeSqlQuery(query: string): Promise<QueryResponse> {
  const res = await fetchWithRetry('/api/sql/query', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to execute query')
  }
  return res.json()
}

// Cypher query execution
export async function executeCypherQuery(query: string): Promise<CypherQueryResponse> {
  const res = await fetchWithRetry('/api/cypher/query', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to execute Cypher query')
  }
  return res.json()
}

// Backward compatibility alias
export const executeQuery = executeSqlQuery

export interface GenerateResponse {
  sql: string
  error?: string
}

export interface HistoryMessage {
  role: 'user' | 'assistant'
  content: string
}

export async function generateSQL(prompt: string, currentQuery?: string, history?: HistoryMessage[]): Promise<GenerateResponse> {
  const res = await fetchWithRetry('/api/generate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ prompt, currentQuery, history }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to generate SQL')
  }
  return res.json()
}

export interface StreamCallbacks {
  onToken: (token: string) => void
  onStatus: (status: { provider?: string; status?: string; attempt?: number; error?: string }) => void
  onDone: (result: GenerateResponse) => void
  onError: (error: string) => void
}

// SQL generation stream (uses new /api/sql/generate/stream endpoint)
export async function generateSqlStream(
  prompt: string,
  currentQuery: string | undefined,
  history: HistoryMessage[] | undefined,
  callbacks: StreamCallbacks
): Promise<void> {
  await generateQueryStream('/api/sql/generate/stream', prompt, currentQuery, history, callbacks)
}

// Cypher generation stream
export async function generateCypherStream(
  prompt: string,
  currentQuery: string | undefined,
  history: HistoryMessage[] | undefined,
  callbacks: StreamCallbacks
): Promise<void> {
  await generateQueryStream('/api/cypher/generate/stream', prompt, currentQuery, history, callbacks)
}

// Auto-detection stream callbacks
export interface AutoStreamCallbacks {
  onMode: (mode: 'sql' | 'cypher') => void
  onToken: (token: string) => void
  onStatus: (status: { provider?: string; status?: string; attempt?: number; error?: string }) => void
  onDone: (result: GenerateResponse) => void
  onError: (error: string) => void
}

// Auto-detection generation stream
export async function generateAutoStream(
  prompt: string,
  currentQuery: string | undefined,
  callbacks: AutoStreamCallbacks
): Promise<void> {
  // Retry initial connection with backoff
  let res: Response

  for (let attempt = 0; attempt <= RETRY_CONFIG.maxRetries; attempt++) {
    try {
      res = await fetch('/api/auto/generate/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt, currentQuery }),
      })

      if (res.ok) break
      if (res.status >= 400 && res.status < 500) break // Don't retry client errors

      if (!isRetryableError(null, res.status) || attempt === RETRY_CONFIG.maxRetries) break
    } catch (err) {
      if (!isRetryableError(err) || attempt === RETRY_CONFIG.maxRetries) {
        callbacks.onError('Connection failed. Please check your network and try again.')
        return
      }
    }

    const delay = Math.min(
      RETRY_CONFIG.baseDelayMs * Math.pow(2, attempt) + Math.random() * 100,
      RETRY_CONFIG.maxDelayMs
    )
    await sleep(delay)
  }

  if (!res!) {
    callbacks.onError('Connection failed. Please check your network and try again.')
    return
  }

  if (!res.ok) {
    const text = await res.text()
    callbacks.onError(text || 'Failed to generate query')
    return
  }

  const reader = res.body?.getReader()
  if (!reader) {
    callbacks.onError('Streaming not supported')
    return
  }

  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (line.startsWith('event: ')) {
          const eventType = line.slice(7)
          const nextLine = lines[i + 1]
          if (nextLine?.startsWith('data: ')) {
            const data = nextLine.slice(6)
            i++ // Skip the data line we just processed
            switch (eventType) {
              case 'mode':
                try {
                  const modeData = JSON.parse(data)
                  callbacks.onMode(modeData.mode)
                } catch {
                  // Ignore parse errors
                }
                break
              case 'token':
                callbacks.onToken(data)
                break
              case 'status':
                try {
                  callbacks.onStatus(JSON.parse(data))
                } catch {
                  callbacks.onStatus({ status: data })
                }
                break
              case 'done':
                try {
                  callbacks.onDone(JSON.parse(data))
                } catch {
                  callbacks.onError('Invalid response')
                }
                break
              case 'error':
                callbacks.onError(data)
                break
            }
          }
        }
      }
    }
  } catch (err) {
    // Connection was interrupted mid-stream
    if (err instanceof TypeError || (err instanceof Error && err.message.includes('network'))) {
      callbacks.onError('Connection lost. Please try again.')
    } else {
      callbacks.onError(err instanceof Error ? err.message : 'Stream error')
    }
  }
}

// Generic query generation stream (internal helper)
async function generateQueryStream(
  endpoint: string,
  prompt: string,
  currentQuery: string | undefined,
  history: HistoryMessage[] | undefined,
  callbacks: StreamCallbacks
): Promise<void> {
  // Retry initial connection with backoff
  let res: Response

  for (let attempt = 0; attempt <= RETRY_CONFIG.maxRetries; attempt++) {
    try {
      res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt, currentQuery, history }),
      })

      if (res.ok) break
      if (res.status >= 400 && res.status < 500) break // Don't retry client errors

      if (!isRetryableError(null, res.status) || attempt === RETRY_CONFIG.maxRetries) break
    } catch (err) {
      if (!isRetryableError(err) || attempt === RETRY_CONFIG.maxRetries) {
        callbacks.onError('Connection failed. Please check your network and try again.')
        return
      }
    }

    const delay = Math.min(
      RETRY_CONFIG.baseDelayMs * Math.pow(2, attempt) + Math.random() * 100,
      RETRY_CONFIG.maxDelayMs
    )
    await sleep(delay)
  }

  if (!res!) {
    callbacks.onError('Connection failed. Please check your network and try again.')
    return
  }

  if (!res.ok) {
    const text = await res.text()
    callbacks.onError(text || 'Failed to generate query')
    return
  }

  const reader = res.body?.getReader()
  if (!reader) {
    callbacks.onError('Streaming not supported')
    return
  }

  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        if (line.startsWith('event: ')) {
          const eventType = line.slice(7)
          const nextLine = lines[i + 1]
          if (nextLine?.startsWith('data: ')) {
            const data = nextLine.slice(6)
            i++ // Skip the data line we just processed
            switch (eventType) {
              case 'token':
                callbacks.onToken(data)
                break
              case 'status':
                try {
                  callbacks.onStatus(JSON.parse(data))
                } catch {
                  callbacks.onStatus({ status: data })
                }
                break
              case 'done':
                try {
                  callbacks.onDone(JSON.parse(data))
                } catch {
                  callbacks.onError('Invalid response')
                }
                break
              case 'error':
                callbacks.onError(data)
                break
            }
          }
        }
      }
    }
  } catch (err) {
    // Connection was interrupted mid-stream
    if (err instanceof TypeError || (err instanceof Error && err.message.includes('network'))) {
      callbacks.onError('Connection lost. Please try again.')
    } else {
      callbacks.onError(err instanceof Error ? err.message : 'Stream error')
    }
  }
}

// Backward compatibility alias
export async function generateSQLStream(
  prompt: string,
  currentQuery: string | undefined,
  history: HistoryMessage[] | undefined,
  callbacks: StreamCallbacks
): Promise<void> {
  return generateSqlStream(prompt, currentQuery, history, callbacks)
}

export interface ChatMessage {
  id?: string // Unique message ID for deduplication (optional for backward compat)
  role: 'user' | 'assistant'
  content: string
  // Workflow data (only present on assistant messages)
  workflowData?: ChatWorkflowData
  // SQL queries for history transmission (extracted from workflowData for backend)
  executedQueries?: string[]
  // Status for streaming persistence (only present during/after streaming)
  status?: 'streaming' | 'complete' | 'error'
  // Workflow ID for durable persistence (only present on streaming messages)
  workflowId?: string
}

// Generate a unique message ID
export function generateMessageId(): string {
  return crypto.randomUUID()
}

// Ensure a message has an ID (for migration of old data)
export function ensureMessageId(message: ChatMessage): ChatMessage {
  if (message.id) return message
  return { ...message, id: generateMessageId() }
}

export interface DataQuestion {
  question: string
  rationale: string
}

export interface GeneratedQuery {
  question: string
  sql: string
  explanation: string
}

export interface ExecutedQuery {
  question: string
  sql: string
  columns: string[]
  rows: unknown[][]
  count: number
  error?: string
}

// Processing step types for unified timeline
export interface ThinkingStep {
  type: 'thinking'
  id: string
  content: string
}

export interface SqlQueryStep {
  type: 'sql_query'
  id: string
  question: string
  sql: string
  status: 'running' | 'completed' | 'error'
  rows?: number
  columns?: string[]
  data?: unknown[][]
  error?: string
}

export interface CypherQueryStep {
  type: 'cypher_query'
  id: string
  question: string
  cypher: string
  status: 'running' | 'completed' | 'error'
  rows?: number
  columns?: string[]
  data?: unknown[][]
  nodes?: unknown[]
  edges?: unknown[]
  error?: string
}

export interface ReadDocsStep {
  type: 'read_docs'
  id: string
  page: string
  status: 'running' | 'completed' | 'error'
  content?: string
  error?: string
}

export type ProcessingStep = ThinkingStep | SqlQueryStep | CypherQueryStep | ReadDocsStep

export interface ChatWorkflowData {
  dataQuestions: DataQuestion[]
  generatedQueries: GeneratedQuery[]
  executedQueries: ExecutedQuery[]
  followUpQuestions?: string[]
  // Processing timeline for unified display
  processingSteps?: ProcessingStep[]
}

// Server-side workflow step (matches WorkflowStep in Go)
export interface ServerWorkflowStep {
  id: string
  type: 'thinking' | 'sql_query' | 'cypher_query' | 'read_docs'
  // For thinking steps
  content?: string
  // For sql_query steps
  question?: string
  sql?: string
  status?: 'running' | 'completed' | 'error'
  columns?: string[]
  rows?: unknown[][]
  count?: number
  error?: string
  // For cypher_query steps
  cypher?: string
  nodes?: unknown[]
  edges?: unknown[]
  // For read_docs steps
  page?: string
}

export interface ChatResponse {
  answer: string
  dataQuestions?: DataQuestion[]
  generatedQueries?: GeneratedQuery[]
  executedQueries?: ExecutedQuery[]
  followUpQuestions?: string[]
  thinking_steps?: string[]  // Server sends snake_case (legacy)
  steps?: ServerWorkflowStep[]  // Unified steps in execution order
  error?: string
}

export async function sendChatMessage(
  message: string,
  history: ChatMessage[],
  signal?: AbortSignal
): Promise<ChatResponse> {
  const res = await fetchWithRetry('/api/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, history }),
    signal,
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to send message')
  }
  return res.json()
}

export interface ChatStreamCallbacks {
  // Processing events
  onThinking: (data: { id: string; content: string }) => void
  // SQL query events
  onSqlStarted: (data: { id: string; question: string; sql: string }) => void
  onSqlDone: (data: { id: string; question: string; sql: string; rows: number; error: string }) => void
  // Cypher query events
  onCypherStarted?: (data: { id: string; question: string; cypher: string }) => void
  onCypherDone?: (data: { id: string; question: string; cypher: string; rows: number; error: string }) => void
  // ReadDocs events
  onReadDocsStarted?: (data: { id: string; page: string }) => void
  onReadDocsDone?: (data: { id: string; page: string; content: string; error: string }) => void
  // Workflow events
  onWorkflowStarted?: (data: { workflow_id: string }) => void
  // Completion events
  onDone: (response: ChatResponse) => void
  onError: (error: string) => void
  onRetrying?: (attempt: number, maxAttempts: number) => void
}

// Stream retry configuration (separate from connection retry)
const STREAM_RETRY_CONFIG = {
  maxRetries: 2,  // Retry up to 2 times on mid-stream failure
  baseDelayMs: 1000,
  maxDelayMs: 3000,
}

export async function sendChatMessageStream(
  message: string,
  history: ChatMessage[],
  callbacks: ChatStreamCallbacks,
  signal: AbortSignal | undefined,
  sessionId: string
): Promise<void> {
  // Outer retry loop for mid-stream failures (e.g., deploy, pod restart)
  for (let streamAttempt = 0; streamAttempt <= STREAM_RETRY_CONFIG.maxRetries; streamAttempt++) {
    if (signal?.aborted) return

    // Notify UI of retry attempt (skip first attempt)
    if (streamAttempt > 0) {
      console.log(`[SSE] Retrying stream (attempt ${streamAttempt + 1}/${STREAM_RETRY_CONFIG.maxRetries + 1})`)
      callbacks.onRetrying?.(streamAttempt, STREAM_RETRY_CONFIG.maxRetries)
      const delay = Math.min(
        STREAM_RETRY_CONFIG.baseDelayMs * Math.pow(2, streamAttempt - 1) + Math.random() * 200,
        STREAM_RETRY_CONFIG.maxDelayMs
      )
      await sleep(delay)
    }

    // Inner retry loop for initial connection
    let res: Response | undefined
    for (let attempt = 0; attempt <= RETRY_CONFIG.maxRetries; attempt++) {
      if (signal?.aborted) return

      try {
        const chatBody: {
          message: string
          history: typeof history
          session_id: string
          anonymous_id?: string
        } = { message, history, session_id: sessionId }
        if (!getAuthToken()) {
          chatBody.anonymous_id = getAnonymousId()
        }
        res = await fetch('/api/chat/stream', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
          body: JSON.stringify(chatBody),
          signal,
        })

        if (res.ok) break
        if (res.status >= 400 && res.status < 500) break // Don't retry client errors

        if (!isRetryableError(null, res.status) || attempt === RETRY_CONFIG.maxRetries) break
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        if (!isRetryableError(err) || attempt === RETRY_CONFIG.maxRetries) {
          // On last stream attempt, show error
          if (streamAttempt === STREAM_RETRY_CONFIG.maxRetries) {
            callbacks.onError('Connection failed. Please check your network and try again.')
            return
          }
          // Otherwise, break to outer loop for stream retry
          break
        }
      }

      const delay = Math.min(
        RETRY_CONFIG.baseDelayMs * Math.pow(2, attempt) + Math.random() * 100,
        RETRY_CONFIG.maxDelayMs
      )
      await sleep(delay)
    }

    if (!res) {
      // Connection completely failed, try stream retry
      if (streamAttempt < STREAM_RETRY_CONFIG.maxRetries) continue
      callbacks.onError('Connection failed. Please check your network and try again.')
      return
    }

    if (!res.ok) {
      const text = await res.text()
      callbacks.onError(text || 'Failed to send message')
      return
    }

    const reader = res.body?.getReader()
    if (!reader) {
      callbacks.onError('Streaming not supported')
      return
    }

    const decoder = new TextDecoder()
    let buffer = ''
    let currentEvent = ''
    let streamCompleted = false  // Track if stream finished successfully

    const processLines = (lines: string[]) => {
      for (const line of lines) {
        if (line.startsWith('event: ')) {
          currentEvent = line.slice(7).trim()
          if (currentEvent === 'done') {
            console.log('[SSE] Got done event line, waiting for data line')
          }
        } else if (line.startsWith('data:') && currentEvent) {
          if (currentEvent === 'done') {
            console.log('[SSE] Got data line for done event, length:', line.length, 'preview:', line.slice(0, 100))
          }
          const data = line.startsWith('data: ') ? line.slice(6) : line.slice(5)
          if (!data.trim()) {
            continue
          }
          try {
            const parsed = JSON.parse(data)
            switch (currentEvent) {
              case 'thinking':
                callbacks.onThinking(parsed)
                break
              // SQL query events
              case 'sql_started':
                callbacks.onSqlStarted(parsed)
                break
              case 'sql_done':
                callbacks.onSqlDone(parsed)
                break
              // Cypher query events
              case 'cypher_started':
                callbacks.onCypherStarted?.(parsed)
                break
              case 'cypher_done':
                callbacks.onCypherDone?.(parsed)
                break
              // ReadDocs events
              case 'read_docs_started':
                callbacks.onReadDocsStarted?.(parsed)
                break
              case 'read_docs_done':
                callbacks.onReadDocsDone?.(parsed)
                break
              case 'workflow_started':
                callbacks.onWorkflowStarted?.(parsed)
                break
              case 'status':
              case 'heartbeat':
                // Ignore legacy status events and heartbeats
                break
              case 'done':
                streamCompleted = true
                callbacks.onDone(parsed)
                break
              case 'error':
                streamCompleted = true
                callbacks.onError(parsed.error || 'Unknown error')
                break
            }
          } catch (e) {
            console.error('[SSE] Parse error for event', currentEvent, e, 'data:', data.slice(0, 200))
          }
          currentEvent = ''
        }
      }
    }

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) {
          if (buffer.trim()) {
            console.log('[SSE] Stream ended, processing remaining buffer:', buffer.slice(0, 200))
            const lines = buffer.split('\n')
            processLines(lines)
          } else {
            console.log('[SSE] Stream ended, buffer empty')
          }
          break
        }

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''
        processLines(lines)
      }
      console.log('[SSE] Stream processing complete, currentEvent:', currentEvent, 'streamCompleted:', streamCompleted)

      // If stream ended without done/error event, it was interrupted
      if (!streamCompleted) {
        console.log('[SSE] Stream ended unexpectedly without completion event')
        if (streamAttempt < STREAM_RETRY_CONFIG.maxRetries) continue
        callbacks.onError('Connection lost. Please try again.')
      }
      return  // Stream completed normally or with server error
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        return
      }
      // Connection was interrupted mid-stream
      const isNetworkError = err instanceof TypeError || (err instanceof Error && err.message.includes('network'))
      console.log('[SSE] Stream error:', err, 'isNetworkError:', isNetworkError, 'attempt:', streamAttempt)

      if (isNetworkError && streamAttempt < STREAM_RETRY_CONFIG.maxRetries) {
        // Retry the entire stream
        continue
      }

      // Final attempt failed
      if (isNetworkError) {
        callbacks.onError('Connection lost. Please try again.')
      } else {
        callbacks.onError(err instanceof Error ? err.message : 'Stream error')
      }
      return
    }
  }
}

export interface GenerateTitleResponse {
  title: string
  error?: string
}

export interface SessionQueryInfo {
  prompt: string
  sql: string
}

export async function generateSessionTitle(
  queries: SessionQueryInfo[],
  signal?: AbortSignal
): Promise<GenerateTitleResponse> {
  // Use the complete endpoint with a specific prompt to generate a title
  const queryDescriptions = queries.slice(0, 3).map((q, i) => {
    if (q.prompt) {
      return `${i + 1}. Question: "${q.prompt}"
   SQL: ${q.sql.slice(0, 200)}${q.sql.length > 200 ? '...' : ''}`
    } else {
      return `${i + 1}. SQL: ${q.sql.slice(0, 300)}${q.sql.length > 300 ? '...' : ''}`
    }
  }).join('\n\n')

  const message = `Generate a very brief title (2-4 words) in sentence case (only first word capitalized) for this data session based on these queries:

${queryDescriptions}

Examples: "Sales by region", "User signups", "Revenue trends", "Order analysis".

Respond with ONLY the title. No quotes, no explanation.`

  const res = await fetchWithRetry('/api/complete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message }),
    signal,
  })

  if (!res.ok) {
    const text = await res.text()
    return { title: '', error: text || 'Failed to generate title' }
  }

  const data: { response: string; error?: string } = await res.json()
  if (data.error) {
    return { title: '', error: data.error }
  }

  // Clean up the response - remove quotes, trim, take first line
  const title = data.response
    .replace(/^["']|["']$/g, '')
    .split('\n')[0]
    .trim()
    .slice(0, 60)

  return { title }
}

export async function generateChatSessionTitle(
  messages: ChatMessage[],
  signal?: AbortSignal
): Promise<GenerateTitleResponse> {
  // Use the first few user messages to generate a title
  const userMessages = messages
    .filter(m => m.role === 'user')
    .slice(0, 3)
    .map((m, i) => `${i + 1}. "${m.content.slice(0, 200)}${m.content.length > 200 ? '...' : ''}"`)
    .join('\n')

  const message = `Generate a very brief title (2-4 words) in sentence case (only first word capitalized) for this chat conversation based on these user messages:

${userMessages}

Context: This is a data analytics chat for DoubleZero (abbreviated as "DZ"), a network of dedicated high-performance links delivering low-latency connectivity globally. "DZ" always means "DoubleZero".

Examples: "Sales analysis help", "Database questions", "Revenue report", "User data query", "DZ vs internet".

Respond with ONLY the title. No quotes, no explanation.`

  const res = await fetchWithRetry('/api/complete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message }),
    signal,
  })

  if (!res.ok) {
    const text = await res.text()
    return { title: '', error: text || 'Failed to generate title' }
  }

  const data: { response: string; error?: string } = await res.json()
  if (data.error) {
    return { title: '', error: data.error }
  }

  // Clean up the response - remove quotes, trim, take first line
  const title = data.response
    .replace(/^["']|["']$/g, '')
    .split('\n')[0]
    .trim()
    .slice(0, 60)

  return { title }
}

// Visualization recommendation types
export interface VisualizationRecommendRequest {
  columns: string[]
  sampleRows: unknown[][]
  rowCount: number
  query: string
}

export interface VisualizationRecommendResponse {
  recommended: boolean
  chartType?: 'bar' | 'line' | 'pie' | 'area' | 'scatter'
  xAxis?: string
  yAxis?: string[]
  reasoning?: string
  error?: string
}

export async function recommendVisualization(
  request: VisualizationRecommendRequest,
  signal?: AbortSignal
): Promise<VisualizationRecommendResponse> {
  const res = await fetchWithRetry('/api/visualize/recommend', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
    signal,
  })

  if (!res.ok) {
    const text = await res.text()
    return { recommended: false, error: text || 'Failed to get recommendation' }
  }

  return res.json()
}

// Stats for landing page
export interface StatsResponse {
  validators_on_dz: number
  total_stake_sol: number
  stake_share_pct: number
  users: number
  devices: number
  links: number
  contributors: number
  metros: number
  bandwidth_bps: number
  user_inbound_bps: number
  fetched_at: string
  error?: string
}

export async function fetchStats(): Promise<StatsResponse> {
  const res = await fetchWithRetry('/api/stats')
  if (!res.ok) {
    throw new Error('Failed to fetch stats')
  }
  return res.json()
}

// Status/health types
export interface SystemHealth {
  database: boolean
  database_msg?: string
  last_ingested?: string
}

export interface NetworkSummary {
  validators_on_dz: number
  total_stake_sol: number
  stake_share_pct: number
  stake_share_delta: number
  users: number
  devices: number
  links: number
  contributors: number
  metros: number
  bandwidth_bps: number
  user_inbound_bps: number
  devices_by_status: Record<string, number>
  links_by_status: Record<string, number>
}

export interface LinkIssue {
  code: string
  link_type: string
  contributor: string
  issue: string
  value: number
  threshold: number
  side_a_metro: string
  side_z_metro: string
  since: string
}

export interface LinkMetric {
  pk: string
  code: string
  link_type: string
  contributor: string
  bandwidth_bps: number
  in_bps: number
  out_bps: number
  utilization_in: number
  utilization_out: number
  side_a_metro: string
  side_z_metro: string
}

export interface LinkHealth {
  total: number
  healthy: number
  degraded: number
  unhealthy: number
  disabled: number
  issues: LinkIssue[]
  high_util_links: LinkMetric[]
  top_util_links: LinkMetric[]
}

export interface PerformanceMetrics {
  avg_latency_us: number
  p95_latency_us: number
  min_latency_us: number
  max_latency_us: number
  avg_loss_percent: number
  avg_jitter_us: number
  total_in_bps: number
  total_out_bps: number
}

export interface InterfaceIssue {
  device_pk: string
  device_code: string
  device_type: string
  contributor: string
  metro: string
  interface_name: string
  link_pk?: string
  link_code?: string
  link_type?: string
  link_side?: string
  in_errors: number
  out_errors: number
  in_discards: number
  out_discards: number
  carrier_transitions: number
  first_seen: string
  last_seen: string
}

export interface InterfaceHealth {
  issues: InterfaceIssue[]
}

export interface InterfaceIssuesResponse {
  issues: InterfaceIssue[]
  time_range: string
}

export interface NonActivatedDevice {
  pk: string
  code: string
  device_type: string
  metro: string
  status: string
  since: string
}

export interface NonActivatedLink {
  pk: string
  code: string
  link_type: string
  side_a_metro: string
  side_z_metro: string
  status: string
  since: string
}

export interface InfrastructureAlerts {
  devices: NonActivatedDevice[]
  links: NonActivatedLink[]
}

export interface DeviceUtilization {
  pk: string
  code: string
  device_type: string
  contributor: string
  metro: string
  current_users: number
  max_users: number
  utilization: number
}

export interface StatusResponse {
  status: 'healthy' | 'degraded' | 'unhealthy'
  timestamp: string
  system: SystemHealth
  network: NetworkSummary
  links: LinkHealth
  interfaces: InterfaceHealth
  alerts: InfrastructureAlerts
  performance: PerformanceMetrics
  top_device_util: DeviceUtilization[]
  error?: string
}

export async function fetchStatus(): Promise<StatusResponse> {
  const res = await fetchWithRetry('/api/status')
  if (!res.ok) {
    throw new Error('Failed to fetch status')
  }
  return res.json()
}

// Link history types for status timeline
export interface LinkHourStatus {
  hour: string
  status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'disabled'
  avg_latency_us: number
  avg_loss_pct: number
  samples: number
}

export interface LinkHistory {
  pk: string
  code: string
  link_type: string
  contributor: string
  side_a_metro: string
  side_z_metro: string
  side_a_device: string
  side_z_device: string
  bandwidth_bps: number
  committed_rtt_us: number
  hours: LinkHourStatus[]
  issue_reasons: string[] // "packet_loss", "high_latency", "disabled"
}

export interface LinkHistoryResponse {
  links: LinkHistory[]
  time_range: string      // "24h", "3d", "7d"
  bucket_minutes: number  // Size of each bucket in minutes
  bucket_count: number    // Number of buckets
}

export async function fetchLinkHistory(timeRange?: string, buckets?: number): Promise<LinkHistoryResponse> {
  const params = new URLSearchParams()
  if (timeRange) params.set('range', timeRange)
  if (buckets) params.set('buckets', buckets.toString())
  const url = `/api/status/link-history${params.toString() ? '?' + params.toString() : ''}`
  const res = await fetchWithRetry(url)
  if (!res.ok) {
    throw new Error('Failed to fetch link history')
  }
  return res.json()
}

// Device history types for status timeline
export interface DeviceHourStatus {
  hour: string
  status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'disabled'
  current_users: number
  max_users: number
  utilization_pct: number
  in_errors: number
  out_errors: number
  in_discards: number
  out_discards: number
  carrier_transitions: number
}

export interface DeviceHistory {
  pk: string
  code: string
  device_type: string
  contributor: string
  metro: string
  max_users: number
  hours: DeviceHourStatus[]
  issue_reasons: string[] // "interface_errors", "high_utilization", "drained", "no_data"
}

export interface DeviceHistoryResponse {
  devices: DeviceHistory[]
  time_range: string      // "24h", "3d", "7d"
  bucket_minutes: number  // Size of each bucket in minutes
  bucket_count: number    // Number of buckets
}

export async function fetchDeviceHistory(timeRange?: string, buckets?: number): Promise<DeviceHistoryResponse> {
  const params = new URLSearchParams()
  if (timeRange) params.set('range', timeRange)
  if (buckets) params.set('buckets', buckets.toString())
  const url = `/api/status/device-history${params.toString() ? '?' + params.toString() : ''}`
  const res = await fetchWithRetry(url)
  if (!res.ok) {
    throw new Error('Failed to fetch device history')
  }
  return res.json()
}

export async function fetchInterfaceIssues(timeRange?: string): Promise<InterfaceIssuesResponse> {
  const params = new URLSearchParams()
  if (timeRange) params.set('range', timeRange)
  const url = `/api/status/interface-issues${params.toString() ? '?' + params.toString() : ''}`
  const res = await fetchWithRetry(url)
  if (!res.ok) {
    throw new Error('Failed to fetch interface issues')
  }
  return res.json()
}

// Topology types
export interface TopologyMetro {
  pk: string
  code: string
  name: string
  latitude: number
  longitude: number
}

export interface TopologyDevice {
  pk: string
  code: string
  status: string
  device_type: string
  metro_pk: string
  contributor_pk: string
  contributor_code: string
  user_count: number
  validator_count: number
  stake_sol: number
  stake_share: number
}

export interface TopologyLink {
  pk: string
  code: string
  status: string
  link_type: string
  bandwidth_bps: number
  side_a_pk: string
  side_a_code: string
  side_a_iface_name: string
  side_z_pk: string
  side_z_code: string
  side_z_iface_name: string
  contributor_pk: string
  contributor_code: string
  latency_us: number
  jitter_us: number
  loss_percent: number
  sample_count: number
  in_bps: number
  out_bps: number
}

export interface TopologyValidator {
  vote_pubkey: string
  node_pubkey: string
  device_pk: string
  tunnel_id: number
  latitude: number
  longitude: number
  city: string
  country: string
  stake_sol: number
  stake_share: number
  commission: number
  version: string
  gossip_ip: string
  gossip_port: number
  tpu_quic_ip: string
  tpu_quic_port: number
  in_bps: number
  out_bps: number
}

export interface TopologyResponse {
  metros: TopologyMetro[]
  devices: TopologyDevice[]
  links: TopologyLink[]
  validators: TopologyValidator[]
  error?: string
}

export async function fetchTopology(): Promise<TopologyResponse> {
  const res = await fetchWithRetry('/api/topology')
  if (!res.ok) {
    throw new Error('Failed to fetch topology')
  }
  return res.json()
}

// ISIS Topology types (graph view)
export interface ISISNodeData {
  id: string
  label: string
  status: string
  deviceType: string
  metroPK?: string
  systemId?: string
  routerId?: string
}

export interface ISISNode {
  data: ISISNodeData
}

export interface ISISEdgeData {
  id: string
  source: string
  target: string
  metric?: number
  adjSids?: number[]
  neighborAddr?: string
}

export interface ISISEdge {
  data: ISISEdgeData
}

export interface ISISTopologyResponse {
  nodes: ISISNode[]
  edges: ISISEdge[]
  error?: string
}

export async function fetchISISTopology(): Promise<ISISTopologyResponse> {
  const res = await fetchWithRetry('/api/topology/isis')
  if (!res.ok) {
    throw new Error('Failed to fetch ISIS topology')
  }
  return res.json()
}

// Path finding types
export interface PathHop {
  devicePK: string
  deviceCode: string
  status: string
  deviceType: string
}

export interface PathResponse {
  path: PathHop[]
  totalMetric: number
  hopCount: number
  error?: string
}

export type PathMode = 'hops' | 'latency'

export async function fetchISISPath(fromPK: string, toPK: string, mode: PathMode = 'hops'): Promise<PathResponse> {
  const res = await fetch(`/api/topology/path?from=${encodeURIComponent(fromPK)}&to=${encodeURIComponent(toPK)}&mode=${mode}`)
  if (!res.ok) {
    throw new Error('Failed to fetch path')
  }
  return res.json()
}

// Multi-path types
export interface MultiPathHop {
  devicePK: string
  deviceCode: string
  status: string
  deviceType: string
  edgeMetric?: number
  edgeMeasuredMs?: number   // measured RTT in ms to reach this hop
  edgeJitterMs?: number     // measured jitter in ms
  edgeLossPct?: number      // packet loss percentage
  edgeSampleCount?: number  // number of samples for confidence
}

export interface SinglePath {
  path: MultiPathHop[]
  totalMetric: number
  hopCount: number
  measuredLatencyMs?: number  // sum of measured RTT along path
  totalSamples?: number       // min samples across hops
}

export interface MultiPathResponse {
  paths: SinglePath[]
  from: string
  to: string
  error?: string
}

export async function fetchISISPaths(fromPK: string, toPK: string, k: number = 5, mode: 'hops' | 'latency' = 'hops'): Promise<MultiPathResponse> {
  const res = await fetch(`/api/topology/paths?from=${encodeURIComponent(fromPK)}&to=${encodeURIComponent(toPK)}&k=${k}&mode=${mode}`)
  if (!res.ok) {
    throw new Error('Failed to fetch paths')
  }
  return res.json()
}

// Critical links types
export interface CriticalLink {
  sourcePK: string
  sourceCode: string
  targetPK: string
  targetCode: string
  metric: number
  criticality: 'critical' | 'important' | 'redundant'
}

export interface CriticalLinksResponse {
  links: CriticalLink[]
  error?: string
}

export async function fetchCriticalLinks(): Promise<CriticalLinksResponse> {
  const res = await fetch('/api/topology/critical-links')
  if (!res.ok) {
    throw new Error('Failed to fetch critical links')
  }
  return res.json()
}

// Redundancy report types
export interface RedundancyIssue {
  type: 'leaf_device' | 'critical_link' | 'single_exit_metro' | 'no_backup_device'
  severity: 'critical' | 'warning' | 'info'
  entityPK: string
  entityCode: string
  entityType: 'device' | 'link' | 'metro'
  description: string
  impact: string
  targetPK?: string
  targetCode?: string
  metroPK?: string
  metroCode?: string
}

export interface RedundancySummary {
  totalIssues: number
  criticalCount: number
  warningCount: number
  infoCount: number
  leafDevices: number
  criticalLinks: number
  singleExitMetros: number
}

export interface RedundancyReportResponse {
  issues: RedundancyIssue[]
  summary: RedundancySummary
  error?: string
}

export async function fetchRedundancyReport(): Promise<RedundancyReportResponse> {
  const res = await fetch('/api/topology/redundancy-report')
  if (!res.ok) {
    throw new Error('Failed to fetch redundancy report')
  }
  return res.json()
}

// Topology comparison types
export interface TopologyDiscrepancy {
  type: 'missing_isis' | 'extra_isis' | 'metric_mismatch'
  linkPK?: string
  linkCode?: string
  deviceAPK: string
  deviceACode: string
  deviceBPK: string
  deviceBCode: string
  configuredRttUs?: number
  isisMetric?: number
  details: string
}

export interface TopologyCompareResponse {
  configuredLinks: number
  isisAdjacencies: number
  matchedLinks: number
  discrepancies: TopologyDiscrepancy[]
  error?: string
}

export async function fetchTopologyCompare(): Promise<TopologyCompareResponse> {
  const res = await fetch('/api/topology/compare')
  if (!res.ok) {
    throw new Error('Failed to fetch topology comparison')
  }
  return res.json()
}

// Failure impact types
export interface ImpactDevice {
  pk: string
  code: string
  status: string
  deviceType: string
}

export interface FailureImpactPath {
  fromPK: string
  fromCode: string
  toPK: string
  toCode: string
  beforeHops: number
  beforeMetric: number
  afterHops: number
  afterMetric: number
  hasAlternate: boolean
}

export interface MetroImpact {
  pk: string
  code: string
  name: string
  totalDevices: number
  remainingDevices: number
  isolatedDevices: number
}

export interface FailureImpactResponse {
  devicePK: string
  deviceCode: string
  unreachableDevices: ImpactDevice[]
  unreachableCount: number
  affectedPaths: FailureImpactPath[]
  affectedPathCount: number
  metroImpact: MetroImpact[]
  error?: string
}

export async function fetchFailureImpact(devicePK: string): Promise<FailureImpactResponse> {
  const res = await fetch(`/api/topology/impact/${encodeURIComponent(devicePK)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch failure impact')
  }
  return res.json()
}

// What-If simulation types
export interface AffectedPath {
  fromPK: string
  fromCode: string
  toPK: string
  toCode: string
  beforeHops: number
  beforeMetric: number
  afterHops: number
  afterMetric: number
  hasAlternate: boolean
}

export interface SimulateLinkRemovalResponse {
  sourcePK: string
  sourceCode: string
  targetPK: string
  targetCode: string
  disconnectedDevices: ImpactDevice[]
  disconnectedCount: number
  affectedPaths: AffectedPath[]
  affectedPathCount: number
  causesPartition: boolean
  error?: string
}

export async function fetchSimulateLinkRemoval(
  sourcePK: string,
  targetPK: string
): Promise<SimulateLinkRemovalResponse> {
  const res = await fetch(
    `/api/topology/simulate-link-removal?sourcePK=${encodeURIComponent(sourcePK)}&targetPK=${encodeURIComponent(targetPK)}`
  )
  if (!res.ok) {
    throw new Error('Failed to simulate link removal')
  }
  return res.json()
}

export interface ImprovedPath {
  fromPK: string
  fromCode: string
  toPK: string
  toCode: string
  beforeHops: number
  beforeMetric: number
  afterHops: number
  afterMetric: number
  hopReduction: number
  metricReduction: number
}

export interface RedundancyGain {
  devicePK: string
  deviceCode: string
  oldDegree: number
  newDegree: number
  wasLeaf: boolean
}

export interface SimulateLinkAdditionResponse {
  sourcePK: string
  sourceCode: string
  targetPK: string
  targetCode: string
  metric: number
  improvedPaths: ImprovedPath[]
  improvedPathCount: number
  redundancyGains: RedundancyGain[]
  redundancyCount: number
  error?: string
}

export async function fetchSimulateLinkAddition(
  sourcePK: string,
  targetPK: string,
  metric: number = 1000
): Promise<SimulateLinkAdditionResponse> {
  const res = await fetch(
    `/api/topology/simulate-link-addition?sourcePK=${encodeURIComponent(sourcePK)}&targetPK=${encodeURIComponent(targetPK)}&metric=${metric}`
  )
  if (!res.ok) {
    throw new Error('Failed to simulate link addition')
  }
  return res.json()
}

// Link Health (SLA compliance) for topology overlay
export interface TopologyLinkHealth {
  link_pk: string
  side_a_pk: string
  side_a_code: string
  side_z_pk: string
  side_z_code: string
  avg_rtt_us: number
  p95_rtt_us: number
  committed_rtt_ns: number
  loss_pct: number
  exceeds_commit: boolean
  has_packet_loss: boolean
  is_dark: boolean
  sla_status: 'healthy' | 'warning' | 'critical' | 'unknown'
  sla_ratio: number
}

export interface LinkHealthResponse {
  links: TopologyLinkHealth[]
  total_links: number
  healthy_count: number
  warning_count: number
  critical_count: number
  unknown_count: number
}

export async function fetchLinkHealth(): Promise<LinkHealthResponse> {
  const res = await fetch('/api/dz/links-health')
  if (!res.ok) {
    throw new Error('Failed to fetch link health')
  }
  return res.json()
}

// Version check
export interface VersionResponse {
  buildTimestamp: string
}

export async function fetchVersion(): Promise<VersionResponse | null> {
  try {
    const res = await fetch('/api/version')
    if (!res.ok) return null
    return res.json()
  } catch {
    return null
  }
}

// Session persistence types
export interface SessionMetadata {
  id: string
  type: 'chat' | 'query'
  name: string | null
  content_length: number
  created_at: string
  updated_at: string
}

export interface ServerSession<T> {
  id: string
  type: 'chat' | 'query'
  name: string | null
  content: T
  created_at: string
  updated_at: string
}

export interface SessionListResponse {
  sessions: SessionMetadata[]
  total: number
  has_more: boolean
}

export interface SessionListWithContentResponse<T> {
  sessions: ServerSession<T>[]
  total: number
  has_more: boolean
}

// Get anonymous_id query param for unauthenticated requests
function getAnonymousIdParam(): string {
  if (getAuthToken()) {
    return '' // Authenticated users don't need anonymous_id
  }
  return `anonymous_id=${encodeURIComponent(getAnonymousId())}`
}

// Session API functions
export async function listSessions(
  type: 'chat' | 'query',
  limit = 50,
  offset = 0
): Promise<SessionListResponse> {
  const anonParam = getAnonymousIdParam()
  const params = [`type=${type}`, `limit=${limit}`, `offset=${offset}`]
  if (anonParam) params.push(anonParam)

  const res = await fetchWithRetry(`/api/sessions?${params.join('&')}`)
  if (!res.ok) {
    throw new Error('Failed to list sessions')
  }
  return res.json()
}

// List sessions with full content (avoids N+1 queries when loading all sessions)
export async function listSessionsWithContent<T>(
  type: 'chat' | 'query',
  limit = 50,
  offset = 0
): Promise<SessionListWithContentResponse<T>> {
  const anonParam = getAnonymousIdParam()
  const params = [`type=${type}`, `limit=${limit}`, `offset=${offset}`, 'include_content=true']
  if (anonParam) params.push(anonParam)

  const res = await fetchWithRetry(`/api/sessions?${params.join('&')}`)
  if (!res.ok) {
    throw new Error('Failed to list sessions')
  }
  return res.json()
}

export async function getSession<T>(id: string): Promise<ServerSession<T>> {
  const anonParam = getAnonymousIdParam()
  const url = anonParam ? `/api/sessions/${id}?${anonParam}` : `/api/sessions/${id}`

  const res = await fetchWithRetry(url)
  if (!res.ok) {
    if (res.status === 404) {
      throw new Error('Session not found')
    }
    throw new Error('Failed to get session')
  }
  return res.json()
}

export async function batchGetSessions<T>(
  ids: string[]
): Promise<{ sessions: ServerSession<T>[] }> {
  if (ids.length === 0) {
    return { sessions: [] }
  }
  const body: { ids: string[]; anonymous_id?: string } = { ids }
  if (!getAuthToken()) {
    body.anonymous_id = getAnonymousId()
  }

  const res = await fetchWithRetry('/api/sessions/batch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    throw new Error('Failed to batch fetch sessions')
  }
  return res.json()
}

export async function createSession<T>(
  id: string,
  type: 'chat' | 'query',
  content: T,
  name?: string
): Promise<ServerSession<T>> {
  const body: { id: string; type: string; name: string | null; content: T; anonymous_id?: string } = {
    id,
    type,
    name: name ?? null,
    content,
  }
  if (!getAuthToken()) {
    body.anonymous_id = getAnonymousId()
  }

  const res = await fetchWithRetry('/api/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    if (res.status === 409) {
      throw new Error('Session already exists')
    }
    throw new Error('Failed to create session')
  }
  return res.json()
}

export async function updateSession<T>(
  id: string,
  content: T,
  name?: string
): Promise<ServerSession<T>> {
  const body: { content: T; name: string | null; anonymous_id?: string } = {
    content,
    name: name ?? null,
  }
  if (!getAuthToken()) {
    body.anonymous_id = getAnonymousId()
  }

  const res = await fetchWithRetry(`/api/sessions/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    if (res.status === 404) {
      throw new Error('Session not found')
    }
    throw new Error('Failed to update session')
  }
  return res.json()
}

export async function deleteSession(id: string): Promise<void> {
  const anonParam = getAnonymousIdParam()
  const url = anonParam ? `/api/sessions/${id}?${anonParam}` : `/api/sessions/${id}`

  const res = await fetchWithRetry(url, {
    method: 'DELETE',
  })
  if (!res.ok && res.status !== 404) {
    throw new Error('Failed to delete session')
  }
}

// Upsert helper - creates if not exists, updates otherwise
export async function upsertSession<T>(
  id: string,
  type: 'chat' | 'query',
  content: T,
  name?: string
): Promise<ServerSession<T>> {
  try {
    return await updateSession(id, content, name)
  } catch (err) {
    if (err instanceof Error && err.message === 'Session not found') {
      return await createSession(id, type, content, name)
    }
    throw err
  }
}

// Workflow types for durable workflow persistence
export interface WorkflowStep {
  type: 'thinking' | 'query'
  content?: string
  question?: string
  sql?: string
  status?: string
  columns?: string[]
  rows?: unknown[][]
  count?: number
  error?: string
}

export interface WorkflowRun {
  id: string
  session_id: string
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  user_question: string
  iteration: number
  steps?: WorkflowStep[]
  final_answer?: string
  llm_calls: number
  input_tokens: number
  output_tokens: number
  started_at: string
  updated_at: string
  completed_at?: string
  error?: string
}

// Get a workflow run by ID
export async function getWorkflow(workflowId: string): Promise<WorkflowRun | null> {
  const res = await fetch(`/api/workflows/${workflowId}`)
  if (res.status === 404) {
    return null
  }
  if (!res.ok) {
    throw new Error('Failed to get workflow')
  }
  return res.json()
}

// Get the latest workflow for a session (running, completed, or failed)
export async function getLatestWorkflowForSession(sessionId: string): Promise<WorkflowRun | null> {
  const res = await fetch(`/api/sessions/${sessionId}/workflow`)
  if (res.status === 204 || res.status === 404) {
    return null // No workflow
  }
  if (!res.ok) {
    throw new Error('Failed to get workflow')
  }
  return res.json()
}

// Legacy alias for backwards compatibility
export const getRunningWorkflowForSession = getLatestWorkflowForSession

// Workflow reconnection callbacks
export interface WorkflowReconnectCallbacks {
  onThinking: (data: { id: string; content: string }) => void
  // SQL query events
  onSqlDone: (data: { id: string; question: string; sql: string; rows: number; error: string }) => void
  // Cypher query events
  onCypherDone?: (data: { id: string; question: string; cypher: string; rows: number; error: string }) => void
  // ReadDocs events
  onReadDocsDone?: (data: { id: string; page: string; content: string; error: string }) => void
  onDone: (response: ChatResponse) => void
  onError: (error: string) => void
  onStatus?: (data: { status: string; iteration: number }) => void
  onRetry?: () => void
}

// Reconnect to a running or completed workflow stream
// This allows the frontend to catch up on progress if the page was refreshed
export async function reconnectToWorkflow(
  workflowId: string,
  callbacks: WorkflowReconnectCallbacks,
  signal?: AbortSignal
): Promise<void> {
  const res = await fetch(`/api/workflows/${workflowId}/stream`, { signal })

  if (!res.ok) {
    if (res.status === 404) {
      callbacks.onError('Workflow not found')
      return
    }
    callbacks.onError('Failed to connect to workflow')
    return
  }

  if (!res.body) {
    callbacks.onError('No response body')
    return
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let currentEvent = ''

  const processLines = (lines: string[]) => {
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        currentEvent = line.slice(7).trim()
      } else if (line.startsWith('data:') && currentEvent) {
        const data = line.startsWith('data: ') ? line.slice(6) : line.slice(5)
        if (!data.trim()) {
          continue
        }
        try {
          const parsed = JSON.parse(data)
          switch (currentEvent) {
            case 'workflow_status':
              callbacks.onStatus?.(parsed)
              break
            case 'thinking':
              callbacks.onThinking(parsed)
              break
            // SQL query events
            case 'sql_done':
              callbacks.onSqlDone(parsed)
              break
            // Cypher query events
            case 'cypher_done':
              callbacks.onCypherDone?.(parsed)
              break
            // ReadDocs events
            case 'read_docs_done':
              callbacks.onReadDocsDone?.(parsed)
              break
            case 'done':
              callbacks.onDone(parsed)
              break
            case 'error':
              callbacks.onError(parsed.error || 'Unknown error')
              break
            case 'live':
              // Workflow is still running - keep connection open
              console.log('[Workflow] Connected to live workflow')
              break
            case 'retry':
              // Workflow is running but not on this server - client should retry
              console.log('[Workflow] Server says retry')
              callbacks.onRetry?.()
              break
          }
        } catch (e) {
          console.error('[Workflow] Parse error for event', currentEvent, e)
        }
        currentEvent = ''
      }
    }
  }

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) {
        if (buffer.trim()) {
          const lines = buffer.split('\n')
          processLines(lines)
        }
        break
      }

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      processLines(lines)
    }
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') {
      return
    }
    callbacks.onError(err instanceof Error ? err.message : 'Stream error')
  }
}

// Paginated response type
export interface PaginatedResponse<T> {
  items: T[]
  total: number
  limit: number
  offset: number
}

export async function fetchAllPaginated<T, R extends PaginatedResponse<T> = PaginatedResponse<T>>(
  fetchPage: (limit: number, offset: number) => Promise<R>,
  pageSize = 100
): Promise<R> {
  const items: T[] = []
  let offset = 0
  let firstResponse: R | null = null

  while (true) {
    const response = await fetchPage(pageSize, offset)
    if (offset === 0) {
      firstResponse = response
    }
    items.push(...response.items)
    offset += response.limit
    if (items.length >= firstResponse!.total || response.items.length === 0) {
      break
    }
  }

  return {
    ...firstResponse!,
    items,
    total: firstResponse!.total,
    limit: pageSize,
    offset: 0,
  }
}

// Entity types - DoubleZero
export interface Device {
  pk: string
  code: string
  status: string
  device_type: string
  contributor_pk: string
  contributor_code: string
  metro_pk: string
  metro_code: string
  public_ip: string
  max_users: number
  current_users: number
  in_bps: number
  out_bps: number
  peak_in_bps: number
  peak_out_bps: number
}

export async function fetchDevices(limit = 100, offset = 0): Promise<PaginatedResponse<Device>> {
  const res = await fetchWithRetry(`/api/dz/devices?limit=${limit}&offset=${offset}`)
  if (!res.ok) {
    throw new Error('Failed to fetch devices')
  }
  return res.json()
}

export interface DeviceDetail extends Device {
  metro_name: string
  validator_count: number
  stake_sol: number
}

export async function fetchDevice(pk: string): Promise<DeviceDetail> {
  const res = await fetchWithRetry(`/api/dz/devices/${encodeURIComponent(pk)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch device')
  }
  return res.json()
}

export interface Link {
  pk: string
  code: string
  status: string
  link_type: string
  bandwidth_bps: number
  side_a_pk: string
  side_a_code: string
  side_a_metro: string
  side_z_pk: string
  side_z_code: string
  side_z_metro: string
  contributor_pk: string
  contributor_code: string
  in_bps: number
  out_bps: number
  utilization_in: number
  utilization_out: number
  latency_us: number
  jitter_us: number
  loss_percent: number
}

export async function fetchLinks(limit = 100, offset = 0): Promise<PaginatedResponse<Link>> {
  const res = await fetchWithRetry(`/api/dz/links?limit=${limit}&offset=${offset}`)
  if (!res.ok) {
    throw new Error('Failed to fetch links')
  }
  return res.json()
}

export interface LinkDetail extends Link {
  peak_in_bps: number
  peak_out_bps: number
}

export async function fetchLink(pk: string): Promise<LinkDetail> {
  const res = await fetchWithRetry(`/api/dz/links/${encodeURIComponent(pk)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch link')
  }
  return res.json()
}

export interface Metro {
  pk: string
  code: string
  name: string
  latitude: number
  longitude: number
  device_count: number
  user_count: number
}

export async function fetchMetros(limit = 100, offset = 0): Promise<PaginatedResponse<Metro>> {
  const res = await fetchWithRetry(`/api/dz/metros?limit=${limit}&offset=${offset}`)
  if (!res.ok) {
    throw new Error('Failed to fetch metros')
  }
  return res.json()
}

export interface MetroDetail extends Metro {
  validator_count: number
  stake_sol: number
  in_bps: number
  out_bps: number
}

export async function fetchMetro(pk: string): Promise<MetroDetail> {
  const res = await fetchWithRetry(`/api/dz/metros/${encodeURIComponent(pk)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch metro')
  }
  return res.json()
}

export interface Contributor {
  pk: string
  code: string
  name: string
  device_count: number
  side_a_devices: number
  side_z_devices: number
  link_count: number
}

export async function fetchContributors(limit = 100, offset = 0): Promise<PaginatedResponse<Contributor>> {
  const res = await fetchWithRetry(`/api/dz/contributors?limit=${limit}&offset=${offset}`)
  if (!res.ok) {
    throw new Error('Failed to fetch contributors')
  }
  return res.json()
}

export interface ContributorDetail extends Contributor {
  user_count: number
  in_bps: number
  out_bps: number
}

export async function fetchContributor(pk: string): Promise<ContributorDetail> {
  const res = await fetchWithRetry(`/api/dz/contributors/${encodeURIComponent(pk)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch contributor')
  }
  return res.json()
}

export interface User {
  pk: string
  owner_pubkey: string
  status: string
  kind: string
  dz_ip: string
  device_pk: string
  device_code: string
  metro_code: string
  metro_name: string
  in_bps: number
  out_bps: number
}

export async function fetchUsers(limit = 100, offset = 0): Promise<PaginatedResponse<User>> {
  const res = await fetchWithRetry(`/api/dz/users?limit=${limit}&offset=${offset}`)
  if (!res.ok) {
    throw new Error('Failed to fetch users')
  }
  return res.json()
}

export interface UserDetail extends User {
  metro_pk: string
  contributor_pk: string
  contributor_code: string
  is_validator: boolean
  vote_pubkey: string
  stake_sol: number
}

export async function fetchUser(pk: string): Promise<UserDetail> {
  const res = await fetchWithRetry(`/api/dz/users/${encodeURIComponent(pk)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch user')
  }
  return res.json()
}

// Solana entity types
export interface Validator {
  vote_pubkey: string
  node_pubkey: string
  stake_sol: number
  stake_share: number
  commission: number
  on_dz: boolean
  device_code: string
  metro_code: string
  city: string
  country: string
  in_bps: number
  out_bps: number
  skip_rate: number
  version: string
}

export interface ValidatorsResponse extends PaginatedResponse<Validator> {
  on_dz_count: number
}

export async function fetchValidators(
  limit = 100,
  offset = 0,
  sortBy?: string,
  sortDir?: 'asc' | 'desc',
  filterField?: string,
  filterValue?: string
): Promise<ValidatorsResponse> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (sortBy) params.set('sort_by', sortBy)
  if (sortDir) params.set('sort_dir', sortDir)
  if (filterValue) {
    // If field is specified, use it; otherwise use 'all' to search across all text fields
    params.set('filter_field', filterField || 'all')
    params.set('filter_value', filterValue)
  }
  const res = await fetchWithRetry(`/api/solana/validators?${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch validators')
  }
  return res.json()
}

export interface ValidatorDetail extends Validator {
  device_pk: string
  metro_pk: string
  gossip_ip: string
  gossip_port: number
}

export async function fetchValidator(votePubkey: string): Promise<ValidatorDetail> {
  const res = await fetchWithRetry(`/api/solana/validators/${encodeURIComponent(votePubkey)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch validator')
  }
  return res.json()
}

export interface GossipNode {
  pubkey: string
  gossip_ip: string
  gossip_port: number
  version: string
  city: string
  country: string
  on_dz: boolean
  device_code: string
  metro_code: string
  stake_sol: number
  is_validator: boolean
}

export interface GossipNodesResponse extends PaginatedResponse<GossipNode> {
  on_dz_count: number
  validator_count: number
}

export async function fetchGossipNodes(
  limit = 100,
  offset = 0,
  sortBy?: string,
  sortDir?: 'asc' | 'desc',
  filterField?: string,
  filterValue?: string
): Promise<GossipNodesResponse> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (sortBy) params.set('sort_by', sortBy)
  if (sortDir) params.set('sort_dir', sortDir)
  if (filterValue) {
    // If field is specified, use it; otherwise use 'all' to search across all text fields
    params.set('filter_field', filterField || 'all')
    params.set('filter_value', filterValue)
  }
  const res = await fetchWithRetry(`/api/solana/gossip-nodes?${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch gossip nodes')
  }
  return res.json()
}

export interface GossipNodeDetail extends GossipNode {
  device_pk: string
  metro_pk: string
  vote_pubkey: string
  in_bps: number
  out_bps: number
}

export async function fetchGossipNode(pubkey: string): Promise<GossipNodeDetail> {
  const res = await fetchWithRetry(`/api/solana/gossip-nodes/${encodeURIComponent(pubkey)}`)
  if (!res.ok) {
    throw new Error('Failed to fetch gossip node')
  }
  return res.json()
}

// Timeline types
export interface TimelineEvent {
  id: string
  event_type: string
  timestamp: string
  category: 'state_change' | 'packet_loss' | 'interface_carrier' | 'interface_errors' | 'interface_discards'
  severity: 'info' | 'warning' | 'critical' | 'success'
  title: string
  description?: string
  entity_type: string
  entity_pk: string
  entity_code: string
  details?: EntityChangeDetails | PacketLossEventDetails | InterfaceEventDetails | ValidatorEventDetails
}

export interface EntityChangeDetails {
  change_type: 'created' | 'updated' | 'deleted'
  changes?: FieldChange[]
  entity?: DeviceEntity | LinkEntity | MetroEntity | ContributorEntity | UserEntity
}

export interface FieldChange {
  field: string
  old_value?: unknown
  new_value?: unknown
}

export interface DeviceEntity {
  pk: string
  code: string
  status: string
  device_type: string
  public_ip: string
  contributor_pk: string
  metro_pk: string
  max_users: number
  contributor_code?: string
  metro_code?: string
}

export interface LinkEntity {
  pk: string
  code: string
  status: string
  link_type: string
  tunnel_net: string
  contributor_pk: string
  side_a_pk: string
  side_z_pk: string
  side_a_iface_name: string
  side_z_iface_name: string
  committed_rtt_ns: number
  committed_jitter_ns: number
  bandwidth_bps: number
  isis_delay_override_ns: number
  contributor_code?: string
  side_a_code?: string
  side_z_code?: string
  side_a_metro_code?: string
  side_z_metro_code?: string
  side_a_metro_pk?: string
  side_z_metro_pk?: string
}

export interface MetroEntity {
  pk: string
  code: string
  name: string
  longitude: number
  latitude: number
}

export interface ContributorEntity {
  pk: string
  code: string
  name: string
}

export interface UserEntity {
  pk: string
  owner_pubkey: string
  status: string
  kind: string
  client_ip: string
  dz_ip: string
  device_pk: string
  tunnel_id: number
  device_code?: string
  metro_code?: string
}

export interface PacketLossEventDetails {
  link_pk: string
  link_code: string
  link_type: string
  side_a_metro: string
  side_z_metro: string
  previous_loss_pct: number
  current_loss_pct: number
  direction: 'increased' | 'decreased'
}

export interface InterfaceEventDetails {
  device_pk: string
  device_code: string
  interface_name: string
  link_pk?: string
  link_code?: string
  in_errors?: number
  out_errors?: number
  in_discards?: number
  out_discards?: number
  carrier_transitions?: number
  issue_type: 'errors' | 'discards' | 'carrier'
}

export interface GroupedInterfaceDetails {
  device_pk: string
  device_code: string
  issue_type: 'errors' | 'discards' | 'carrier'
  interfaces: InterfaceEventDetails[]
}

export interface ValidatorEventDetails {
  owner_pubkey: string
  dz_ip?: string
  vote_pubkey?: string
  node_pubkey?: string
  stake_lamports?: number
  stake_sol?: number
  stake_share_pct?: number
  user_pk?: string
  device_pk?: string
  device_code?: string
  metro_code?: string
  kind: 'validator' | 'gossip_only'
  action: 'joined' | 'left'
}

export interface HistogramBucket {
  timestamp: string
  count: number
}

export interface TimelineResponse {
  events: TimelineEvent[]
  total: number
  limit: number
  offset: number
  time_range: {
    start: string
    end: string
  }
  histogram?: HistogramBucket[]
  error?: string
}

export type TimeRange = '1h' | '6h' | '12h' | '24h' | '3d' | '7d'
export type ActionFilter = 'added' | 'removed' | 'changed' | 'alerting' | 'resolved'

export interface TimelineParams {
  range?: TimeRange
  start?: string // ISO 8601 timestamp for custom range
  end?: string   // ISO 8601 timestamp for custom range
  category?: string
  entity_type?: string
  severity?: string
  action?: string // Comma-separated action filters
  dz_filter?: 'on_dz' | 'off_dz' // Filter Solana events by DZ connection
  search?: string // Comma-separated search terms to filter by entity codes, device codes, etc.
  limit?: number
  offset?: number
  include_internal?: boolean
}

export interface TimelineBoundsResponse {
  earliest_data: string // ISO 8601 timestamp
  latest_data: string   // ISO 8601 timestamp
}

export async function fetchTimelineBounds(): Promise<TimelineBoundsResponse> {
  const res = await fetchWithRetry('/api/timeline/bounds')
  if (!res.ok) {
    throw new Error('Failed to fetch timeline bounds')
  }
  return res.json()
}

export async function fetchTimeline(params: TimelineParams = {}): Promise<TimelineResponse> {
  const searchParams = new URLSearchParams()
  if (params.range) searchParams.set('range', params.range)
  if (params.start) searchParams.set('start', params.start)
  if (params.end) searchParams.set('end', params.end)
  if (params.category) searchParams.set('category', params.category)
  if (params.entity_type) searchParams.set('entity_type', params.entity_type)
  if (params.severity) searchParams.set('severity', params.severity)
  if (params.action) searchParams.set('action', params.action)
  if (params.dz_filter) searchParams.set('dz_filter', params.dz_filter)
  if (params.search) searchParams.set('search', params.search)
  if (params.limit) searchParams.set('limit', params.limit.toString())
  if (params.offset) searchParams.set('offset', params.offset.toString())
  if (params.include_internal) searchParams.set('include_internal', 'true')

  const url = `/api/timeline${searchParams.toString() ? '?' + searchParams.toString() : ''}`
  const res = await fetchWithRetry(url)
  if (!res.ok) {
    throw new Error('Failed to fetch timeline')
  }
  return res.json()
}

// Metro connectivity matrix types
export interface MetroInfo {
  pk: string
  code: string
  name: string
}

export interface MetroConnectivity {
  fromMetroPK: string
  fromMetroCode: string
  fromMetroName: string
  toMetroPK: string
  toMetroCode: string
  toMetroName: string
  pathCount: number
  minHops: number
  minMetric: number
  bottleneckBwGbps?: number  // min bandwidth along best path
}

export interface MetroConnectivityResponse {
  metros: MetroInfo[]
  connectivity: MetroConnectivity[]
  error?: string
}

export async function fetchMetroConnectivity(): Promise<MetroConnectivityResponse> {
  const res = await fetch('/api/topology/metro-connectivity')
  if (!res.ok) {
    throw new Error('Failed to fetch metro connectivity')
  }
  return res.json()
}

// DZ vs Internet latency comparison types
export interface LatencyComparison {
  origin_metro_pk: string
  origin_metro_code: string
  origin_metro_name: string
  target_metro_pk: string
  target_metro_code: string
  target_metro_name: string
  dz_avg_rtt_ms: number
  dz_p95_rtt_ms: number
  dz_avg_jitter_ms: number | null
  dz_loss_pct: number
  dz_sample_count: number
  internet_avg_rtt_ms: number
  internet_p95_rtt_ms: number
  internet_avg_jitter_ms: number | null
  internet_sample_count: number
  rtt_improvement_pct: number | null
  jitter_improvement_pct: number | null
}

export interface LatencyComparisonResponse {
  comparisons: LatencyComparison[]
  summary: {
    total_pairs: number
    avg_improvement_pct: number
    max_improvement_pct: number
    pairs_with_data: number
  }
}

export async function fetchLatencyComparison(): Promise<LatencyComparisonResponse> {
  const res = await fetch('/api/topology/latency-comparison')
  if (!res.ok) {
    throw new Error('Failed to fetch latency comparison')
  }
  return res.json()
}

// Latency history time series types
export interface LatencyHistoryPoint {
  timestamp: string
  dz_avg_rtt_ms: number | null
  dz_avg_jitter_ms: number | null
  dz_sample_count: number
  inet_avg_rtt_ms: number | null
  inet_avg_jitter_ms: number | null
  inet_sample_count: number
}

export interface LatencyHistoryResponse {
  origin_metro_code: string
  target_metro_code: string
  points: LatencyHistoryPoint[]
}

export async function fetchLatencyHistory(
  originCode: string,
  targetCode: string,
  timeRange?: string
): Promise<LatencyHistoryResponse> {
  const params = timeRange ? `?range=${timeRange}` : ''
  const res = await fetch(`/api/topology/latency-history/${originCode}/${targetCode}${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch latency history')
  }
  return res.json()
}

// Metro path latency types (path-based DZ vs Internet comparison)
export type PathOptimizeMode = 'hops' | 'latency' | 'bandwidth'

export interface MetroPathLatency {
  fromMetroPK: string
  fromMetroCode: string
  toMetroPK: string
  toMetroCode: string
  pathLatencyMs: number
  hopCount: number
  bottleneckBwGbps: number
  internetLatencyMs: number
  improvementPct: number
}

export interface MetroPathLatencyResponse {
  optimize: PathOptimizeMode
  paths: MetroPathLatency[]
  summary: {
    totalPairs: number
    pairsWithInternet: number
    avgImprovementPct: number
    maxImprovementPct: number
  }
  error?: string
}

export async function fetchMetroPathLatency(optimize: PathOptimizeMode = 'latency'): Promise<MetroPathLatencyResponse> {
  const res = await fetch(`/api/topology/metro-path-latency?optimize=${optimize}`)
  if (!res.ok) {
    throw new Error('Failed to fetch metro path latency')
  }
  return res.json()
}

// Metro path detail types (single path breakdown)
export interface MetroPathDetailHop {
  devicePK: string
  deviceCode: string
  metroPK: string
  metroCode: string
  linkMetric: number
  linkBwGbps: number
  linkLatency: number
}

export interface MetroPathDetailResponse {
  fromMetroCode: string
  toMetroCode: string
  optimize: PathOptimizeMode
  totalLatencyMs: number
  totalHops: number
  bottleneckBwGbps: number
  internetLatencyMs: number
  improvementPct: number
  hops: MetroPathDetailHop[]
  error?: string
}

export async function fetchMetroPathDetail(
  from: string,
  to: string,
  optimize: PathOptimizeMode = 'latency'
): Promise<MetroPathDetailResponse> {
  const res = await fetch(`/api/topology/metro-path-detail?from=${from}&to=${to}&optimize=${optimize}`)
  if (!res.ok) {
    throw new Error('Failed to fetch metro path detail')
  }
  return res.json()
}

// Metro paths types (for connectivity matrix detail)
export interface MetroPathsHop {
  devicePK: string
  deviceCode: string
  metroPK: string
  metroCode: string
}

export interface MetroPath {
  hops: MetroPathsHop[]
  totalHops: number
  totalMetric: number
  latencyMs: number
}

export interface MetroPathsResponse {
  fromMetroCode: string
  toMetroCode: string
  paths: MetroPath[]
  error?: string
}

export async function fetchMetroPaths(
  fromPK: string,
  toPK: string,
  k: number = 5
): Promise<MetroPathsResponse> {
  const res = await fetch(`/api/topology/metro-paths?from=${fromPK}&to=${toPK}&k=${k}`)
  if (!res.ok) {
    throw new Error('Failed to fetch metro paths')
  }
  return res.json()
}

// Maintenance planner types
export interface MaintenanceAffectedPath {
  source: string
  target: string
  sourceMetro: string
  targetMetro: string
  hopsBefore: number
  hopsAfter: number
  metricBefore: number
  metricAfter: number
  status: 'rerouted' | 'degraded' | 'disconnected'
}

export interface MaintenanceItem {
  type: 'device' | 'link'
  pk: string
  code: string
  impact: number
  disconnected: number
  causesPartition: boolean
  disconnectedDevices?: string[]
  affectedPaths?: MaintenanceAffectedPath[]
}

export interface AffectedLink {
  sourceDevice: string
  targetDevice: string
  status: 'offline' | 'rerouted'
}

export interface AffectedMetroPair {
  sourceMetro: string
  targetMetro: string
  affectedLinks: AffectedLink[]
  status: 'reduced' | 'degraded' | 'disconnected'
}

export interface MaintenanceImpactRequest {
  devices: string[]
  links: string[]
}

export interface MaintenanceImpactResponse {
  items: MaintenanceItem[]
  totalImpact: number
  totalDisconnected: number
  recommendedOrder: string[]
  affectedPaths?: MaintenanceAffectedPath[]
  affectedMetros?: AffectedMetroPair[]
  disconnectedList?: string[]
  error?: string
}

export async function fetchMaintenanceImpact(
  devices: string[],
  links: string[]
): Promise<MaintenanceImpactResponse> {
  const res = await fetch('/api/topology/maintenance-impact', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ devices, links }),
  })
  if (!res.ok) {
    throw new Error('Failed to analyze maintenance impact')
  }
  return res.json()
}

// What-if removal types (unified API for devices and links)
export interface WhatIfAffectedPath {
  source: string
  target: string
  sourceMetro?: string
  targetMetro?: string
  hopsBefore: number
  metricBefore: number
  hopsAfter: number
  metricAfter: number
  status: 'rerouted' | 'degraded' | 'disconnected'
}

export interface WhatIfRemovalItem {
  type: 'device' | 'link'
  pk: string
  code: string
  affectedPaths: WhatIfAffectedPath[]
  affectedPathCount: number
  disconnectedDevices: string[]
  disconnectedCount: number
  causesPartition: boolean
}

export interface WhatIfRemovalResponse {
  items: WhatIfRemovalItem[]
  totalAffectedPaths: number
  totalDisconnected: number
  affectedPaths?: WhatIfAffectedPath[]
  disconnectedList?: string[]
  error?: string
}

export async function fetchWhatIfRemoval(
  devices: string[],
  links: string[]
): Promise<WhatIfRemovalResponse> {
  const res = await fetch('/api/topology/whatif-removal', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ devices, links }),
  })
  if (!res.ok) {
    throw new Error('Failed to analyze what-if removal impact')
  }
  return res.json()
}

// Stake analytics types
export interface StakeOverview {
  dz_stake_sol: number
  total_stake_sol: number
  stake_share_pct: number
  validator_count: number
  dz_stake_sol_24h_ago: number
  stake_share_pct_24h_ago: number
  dz_stake_change_24h: number
  share_change_24h: number
  dz_stake_sol_7d_ago: number
  stake_share_pct_7d_ago: number
  dz_stake_change_7d: number
  share_change_7d: number
  fetched_at: string
  error?: string
}

export interface StakeHistoryPoint {
  timestamp: string
  dz_stake_sol: number
  total_stake_sol: number
  stake_share_pct: number
}

export interface StakeHistoryResponse {
  points: StakeHistoryPoint[]
  fetched_at: string
  error?: string
}

export async function fetchStakeOverview(): Promise<StakeOverview> {
  const res = await fetchWithRetry('/api/stake/overview')
  if (!res.ok) {
    throw new Error('Failed to fetch stake overview')
  }
  return res.json()
}

export async function fetchStakeHistory(
  range: '24h' | '7d' | '30d' = '7d',
  interval: '5m' | '15m' | '1h' | '6h' | '1d' = '1h'
): Promise<StakeHistoryResponse> {
  const params = new URLSearchParams({ range, interval })
  const res = await fetchWithRetry(`/api/stake/history?${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch stake history')
  }
  return res.json()
}

// Stake changes (attribution)
export interface StakeChange {
  category: 'joined' | 'left' | 'stake_increase' | 'stake_decrease'
  vote_pubkey: string
  node_pubkey: string
  stake_sol: number
  stake_change_sol: number
  timestamp: string
  city?: string
  country?: string
}

export interface ChangeSummary {
  joined_count: number
  joined_stake_sol: number
  left_count: number
  left_stake_sol: number
  stake_increase_sol: number
  stake_decrease_sol: number
  net_change_sol: number
}

export interface StakeChangesResponse {
  changes: StakeChange[]
  summary: ChangeSummary
  range: string
  fetched_at: string
  error?: string
}

export async function fetchStakeChanges(
  range: '24h' | '7d' | '30d' = '24h'
): Promise<StakeChangesResponse> {
  const params = new URLSearchParams({ range })
  const res = await fetchWithRetry(`/api/stake/changes?${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch stake changes')
  }
  return res.json()
}

// Stake validators
export interface StakeValidator {
  vote_pubkey: string
  node_pubkey: string
  stake_sol: number
  stake_share_pct: number
  commission: number
  version: string
  city: string
  country: string
  on_dz: boolean
  device_code?: string
  metro_code?: string
}

export interface StakeValidatorsResponse {
  validators: StakeValidator[]
  total_count: number
  on_dz_count: number
  total_stake_sol: number
  dz_stake_sol: number
  fetched_at: string
  error?: string
}

export async function fetchStakeValidators(
  filter: 'all' | 'on_dz' | 'off_dz' = 'on_dz',
  limit: number = 100
): Promise<StakeValidatorsResponse> {
  const params = new URLSearchParams({ filter, limit: limit.toString() })
  const res = await fetchWithRetry(`/api/stake/validators?${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch stake validators')
  }
  return res.json()
}

// Search types and functions
export type SearchEntityType = 'device' | 'link' | 'metro' | 'contributor' | 'user' | 'validator' | 'gossip'

export interface SearchSuggestion {
  type: SearchEntityType
  id: string
  label: string
  sublabel: string
  url: string
}

export interface AutocompleteResponse {
  suggestions: SearchSuggestion[]
}

export interface SearchResultGroup {
  items: SearchSuggestion[]
  total: number
}

export interface SearchResponse {
  query: string
  results: Partial<Record<SearchEntityType, SearchResultGroup>>
}

export async function fetchAutocomplete(query: string, limit = 10): Promise<AutocompleteResponse> {
  const params = new URLSearchParams({ q: query, limit: limit.toString() })
  const res = await fetchWithRetry(`/api/search/autocomplete?${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch autocomplete')
  }
  return res.json()
}

export async function fetchSearch(
  query: string,
  types?: SearchEntityType[],
  limit = 20
): Promise<SearchResponse> {
  const params = new URLSearchParams({ q: query, limit: limit.toString() })
  if (types && types.length > 0) {
    params.set('types', types.join(','))
  }
  const res = await fetchWithRetry(`/api/search?${params}`)
  if (!res.ok) {
    throw new Error('Failed to fetch search')
  }
  return res.json()
}

// Link outages types
export type OutageTimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d' | '30d'
export type OutageType = 'status' | 'packet_loss' | 'no_data'
export type OutageThreshold = 1 | 10

export interface LinkOutage {
  id: string
  link_pk: string
  link_code: string
  link_type: string
  side_a_metro: string
  side_z_metro: string
  contributor_code: string
  outage_type: OutageType
  // Status outages
  previous_status?: string
  new_status?: string
  // Packet loss outages
  threshold_pct?: number
  peak_loss_pct?: number
  // Common
  started_at: string
  ended_at?: string
  duration_seconds?: number
  is_ongoing: boolean
}

export interface LinkOutagesSummary {
  total: number
  ongoing: number
  by_type: {
    status: number
    packet_loss: number
    no_data?: number
  }
}

export interface LinkOutagesResponse {
  outages: LinkOutage[]
  summary: LinkOutagesSummary
}

export interface FetchLinkOutagesParams {
  range?: OutageTimeRange
  threshold?: OutageThreshold
  type?: 'all' | 'status' | 'loss' | 'no_data'
  filter?: string // Format: "type:value,type:value" (e.g., "metro:SAO,contributor:ZAYO")
}

export async function fetchLinkOutages(params: FetchLinkOutagesParams = {}): Promise<LinkOutagesResponse> {
  const searchParams = new URLSearchParams()
  if (params.range) searchParams.set('range', params.range)
  if (params.threshold) searchParams.set('threshold', params.threshold.toString())
  if (params.type) searchParams.set('type', params.type)
  if (params.filter) searchParams.set('filter', params.filter)

  const queryString = searchParams.toString()
  const url = `/api/outages/links${queryString ? `?${queryString}` : ''}`

  const res = await fetchWithRetry(url)
  if (!res.ok) {
    throw new Error('Failed to fetch link outages')
  }
  return res.json()
}

// Auth types and functions
export type AccountType = 'domain' | 'wallet'

export interface Account {
  id: string
  account_type: AccountType
  wallet_address?: string
  email?: string
  email_domain?: string
  display_name?: string
  sol_balance?: number
  sol_balance_updated_at?: string
  is_active: boolean
  created_at: string
  updated_at: string
  last_login_at?: string
}

export interface QuotaInfo {
  remaining: number | null  // null = unlimited
  limit: number | null      // null = unlimited
  resets_at: string         // ISO timestamp
}

export interface AuthMeResponse {
  account: Account | null
  quota: QuotaInfo | null
}

export interface WalletNonceResponse {
  nonce: string
}

export interface WalletAuthResponse {
  token: string
  account: Account
}

export interface GoogleAuthResponse {
  token: string
  account: Account
}

// Get current user and quota
export class AuthError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'AuthError'
    this.status = status
  }
}

export async function fetchAuthMe(): Promise<AuthMeResponse> {
  const res = await fetchWithRetry('/api/auth/me')
  if (!res.ok) {
    throw new AuthError('Failed to get auth status', res.status)
  }
  return res.json()
}

// Logout
export async function logout(): Promise<void> {
  try {
    await fetchWithRetry('/api/auth/logout', { method: 'POST' })
  } finally {
    clearAuthToken()
  }
}

// Get nonce for wallet signing
export async function getWalletNonce(): Promise<string> {
  const res = await fetch('/api/auth/nonce')
  if (!res.ok) {
    throw new Error('Failed to get nonce')
  }
  const data: WalletNonceResponse = await res.json()
  return data.nonce
}

// Authenticate with wallet signature
export async function authenticateWallet(
  publicKey: string,
  signature: string,
  message: string
): Promise<WalletAuthResponse> {
  // Include anonymous_id for session migration
  const anonymousId = localStorage.getItem(ANONYMOUS_ID_KEY)

  const res = await fetch('/api/auth/wallet', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      public_key: publicKey,
      signature,
      message,
      anonymous_id: anonymousId,
    }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Wallet authentication failed')
  }
  const data: WalletAuthResponse = await res.json()
  setAuthToken(data.token)
  return data
}

// Authenticate with Google ID token
export async function authenticateGoogle(idToken: string): Promise<GoogleAuthResponse> {
  // Include anonymous_id for session migration
  const anonymousId = localStorage.getItem(ANONYMOUS_ID_KEY)

  const res = await fetch('/api/auth/google', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      id_token: idToken,
      anonymous_id: anonymousId,
    }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Google authentication failed')
  }
  const data: GoogleAuthResponse = await res.json()
  setAuthToken(data.token)
  return data
}

// Get current quota
export async function fetchQuota(): Promise<QuotaInfo> {
  const res = await fetchWithRetry('/api/usage/quota')
  if (!res.ok) {
    throw new Error('Failed to get quota')
  }
  return res.json()
}

// Build SIWS message for signing
export function buildSIWSMessage(nonce: string): string {
  return `Sign this message to authenticate with DoubleZero Data.\n\nNonce: ${nonce}`
}

// Field values for autocomplete
export interface FieldValuesResponse {
  values: string[]
}

export async function fetchFieldValues(entity: string, field: string): Promise<string[]> {
  const res = await fetchWithRetry(`/api/dz/field-values?entity=${encodeURIComponent(entity)}&field=${encodeURIComponent(field)}`)
  if (!res.ok) {
    return []
  }
  const data: FieldValuesResponse = await res.json()
  return data.values
}
