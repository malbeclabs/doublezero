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

// Retry wrapper for fetch calls
async function fetchWithRetry(
  url: string,
  options?: RequestInit,
  config = RETRY_CONFIG
): Promise<Response> {
  let lastError: unknown
  let lastStatus: number | undefined

  for (let attempt = 0; attempt <= config.maxRetries; attempt++) {
    try {
      const res = await fetch(url, options)

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

export async function executeQuery(query: string): Promise<QueryResponse> {
  const res = await fetchWithRetry('/api/query', {
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

export async function generateSQLStream(
  prompt: string,
  currentQuery: string | undefined,
  history: HistoryMessage[] | undefined,
  callbacks: StreamCallbacks
): Promise<void> {
  // Retry initial connection with backoff
  let res: Response

  for (let attempt = 0; attempt <= RETRY_CONFIG.maxRetries; attempt++) {
    try {
      res = await fetch('/api/generate/stream', {
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
    callbacks.onError(text || 'Failed to generate SQL')
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

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  // Pipeline data (only present on assistant messages)
  pipelineData?: ChatPipelineData
  // SQL queries for history transmission (extracted from pipelineData for backend)
  executedQueries?: string[]
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

export interface ChatPipelineData {
  dataQuestions: DataQuestion[]
  generatedQueries: GeneratedQuery[]
  executedQueries: ExecutedQuery[]
}

export interface ChatResponse {
  answer: string
  dataQuestions?: DataQuestion[]
  generatedQueries?: GeneratedQuery[]
  executedQueries?: ExecutedQuery[]
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
  onStatus: (status: { step: string; message: string }) => void
  onDecomposed: (data: { count: number; questions: DataQuestion[] }) => void
  onQueryProgress: (data: { completed: number; total: number; question: string; success: boolean; rows: number }) => void
  onDone: (response: ChatResponse) => void
  onError: (error: string) => void
}

export async function sendChatMessageStream(
  message: string,
  history: ChatMessage[],
  callbacks: ChatStreamCallbacks,
  signal?: AbortSignal
): Promise<void> {
  // Retry initial connection with backoff
  let res: Response

  for (let attempt = 0; attempt <= RETRY_CONFIG.maxRetries; attempt++) {
    // Check if aborted before attempting
    if (signal?.aborted) return

    try {
      res = await fetch('/api/chat/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, history }),
        signal,
      })

      if (res.ok) break
      if (res.status >= 400 && res.status < 500) break // Don't retry client errors

      if (!isRetryableError(null, res.status) || attempt === RETRY_CONFIG.maxRetries) break
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
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
  let currentEvent = ''  // Persist across chunks

  const processLines = (lines: string[]) => {
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        currentEvent = line.slice(7).trim()
      } else if (line.startsWith('data:') && currentEvent) {
        // Handle both 'data: {...}' and 'data:{...}' formats
        const data = line.startsWith('data: ') ? line.slice(6) : line.slice(5)
        // Skip empty data lines
        if (!data.trim()) {
          continue
        }
        try {
          const parsed = JSON.parse(data)
          switch (currentEvent) {
            case 'status':
              callbacks.onStatus(parsed)
              break
            case 'decomposed':
              callbacks.onDecomposed(parsed)
              break
            case 'query_progress':
              callbacks.onQueryProgress(parsed)
              break
            case 'heartbeat':
              // Ignore heartbeat events - they're just to keep the connection alive
              break
            case 'done':
              console.log('[SSE] done event received', { answerLength: parsed.answer?.length })
              callbacks.onDone(parsed)
              console.log('[SSE] onDone callback completed')
              break
            case 'error':
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
        // Process any remaining buffer when stream ends
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
    // Connection was interrupted mid-stream
    if (err instanceof TypeError || (err instanceof Error && err.message.includes('network'))) {
      callbacks.onError('Connection lost. Please try again.')
    } else {
      callbacks.onError(err instanceof Error ? err.message : 'Stream error')
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
