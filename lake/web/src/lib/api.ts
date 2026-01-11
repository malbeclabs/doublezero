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
}

export interface ChatQueryResult {
  sql: string
  columns: string[]
  rows: unknown[][]
  error?: string
}

export interface ChatResponse {
  response: string
  queries?: ChatQueryResult[]
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
  const title = data.response
    .replace(/^["']|["']$/g, '')
    .split('\n')[0]
    .trim()
    .slice(0, 60)

  return { title }
}
