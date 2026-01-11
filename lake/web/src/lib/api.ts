export interface TableInfo {
  name: string
  database: string
  engine: string
  type: 'table' | 'view'
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

export async function fetchCatalog(): Promise<CatalogResponse> {
  const res = await fetch('/api/catalog')
  if (!res.ok) {
    throw new Error('Failed to fetch catalog')
  }
  return res.json()
}

export async function executeQuery(query: string): Promise<QueryResponse> {
  const res = await fetch('/api/query', {
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
  const res = await fetch('/api/generate', {
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
  const res = await fetch('/api/generate/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ prompt, currentQuery, history }),
  })

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

  while (true) {
    const { done, value } = await reader.read()
    if (done) break

    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() || ''

    for (const line of lines) {
      if (line.startsWith('event: ')) {
        const eventType = line.slice(7)
        const dataLineIndex = lines.indexOf(line) + 1
        if (dataLineIndex < lines.length) {
          const dataLine = lines[dataLineIndex]
          if (dataLine.startsWith('data: ')) {
            const data = dataLine.slice(6)
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
  }
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  // Pipeline data (only present on assistant messages)
  pipelineData?: ChatPipelineData
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
  const res = await fetch('/api/chat', {
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
  const res = await fetch('/api/chat/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, history }),
    signal,
  })

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

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      let currentEvent = ''
      for (const line of lines) {
        if (line.startsWith('event: ')) {
          currentEvent = line.slice(7)
        } else if (line.startsWith('data: ') && currentEvent) {
          const data = line.slice(6)
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
              case 'done':
                callbacks.onDone(parsed)
                break
              case 'error':
                callbacks.onError(parsed.error || 'Unknown error')
                break
            }
          } catch {
            // Ignore parse errors
          }
          currentEvent = ''
        }
      }
    }
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') {
      return
    }
    callbacks.onError(err instanceof Error ? err.message : 'Stream error')
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
  // Use the chat endpoint with a specific prompt to generate a title
  const queryDescriptions = queries.slice(0, 3).map((q, i) =>
    `${i + 1}. Question: "${q.prompt}"
   SQL: ${q.sql.slice(0, 200)}${q.sql.length > 200 ? '...' : ''}`
  ).join('\n\n')

  const message = `Generate a very brief title (2-4 words) for this data session based on these queries:

${queryDescriptions}

Examples: "Sales by Region", "User Signups", "Revenue Trends", "Order Analysis".

Respond with ONLY the title. No quotes, no explanation.`

  const res = await fetch('/api/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, history: [] }),
    signal,
  })

  if (!res.ok) {
    const text = await res.text()
    return { title: '', error: text || 'Failed to generate title' }
  }

  const data: ChatResponse = await res.json()
  if (data.error) {
    return { title: '', error: data.error }
  }

  // Clean up the response - remove quotes, trim, take first line
  const title = data.answer
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

  const message = `Generate a very brief title (2-4 words) for this chat conversation based on these user messages:

${userMessages}

Examples: "Sales Analysis Help", "Database Questions", "Revenue Report", "User Data Query".

Respond with ONLY the title. No quotes, no explanation.`

  const res = await fetch('/api/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, history: [] }),
    signal,
  })

  if (!res.ok) {
    const text = await res.text()
    return { title: '', error: text || 'Failed to generate title' }
  }

  const data: ChatResponse = await res.json()
  if (data.error) {
    return { title: '', error: data.error }
  }

  // Clean up the response - remove quotes, trim, take first line
  const title = data.answer
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
  const res = await fetch('/api/visualize/recommend', {
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
