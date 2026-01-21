import { useRef, useEffect, useState, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { format as formatSQL } from 'sql-formatter'
import type { ChatMessage, ProcessingStep } from '@/lib/api'
import { ArrowUp, Square, Loader2, Copy, Check, ChevronDown, ChevronRight, ExternalLink, MessageCircle, CheckCircle2, XCircle, Brain, RotateCcw } from 'lucide-react'
import { useTheme } from '@/hooks/use-theme'
import { formatNeo4jValue, isNeo4jValue } from '@/lib/neo4j-utils'
import { isCypherQuery, formatCypher } from '@/lib/format-cypher'

// Format error message for display
function formatErrorMessage(content: string): { message: string; isQuotaError: boolean; resetsAt?: Date } {
  try {
    const parsed = JSON.parse(content)
    if (parsed.error && typeof parsed.error === 'string') {
      const isQuotaError = parsed.error.toLowerCase().includes('limit exceeded') ||
                          parsed.error.toLowerCase().includes('quota')
      return {
        message: parsed.error,
        isQuotaError,
        resetsAt: parsed.resets_at ? new Date(parsed.resets_at) : undefined
      }
    }
  } catch {
    // Not JSON, return as-is
  }
  return { message: content, isQuotaError: false }
}

// Format reset time for display
function formatResetTime(date: Date): string {
  const now = new Date()
  const diffMs = date.getTime() - now.getTime()
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
  const diffMins = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60))

  if (diffHours > 0) {
    return `${diffHours}h ${diffMins}m`
  } else if (diffMins > 0) {
    return `${diffMins}m`
  }
  return 'soon'
}

// Format SQL for display
function formatSQLForDisplay(sql: string): string {
  try {
    return formatSQL(sql, {
      language: 'sql',
      tabWidth: 2,
      keywordCase: 'upper',
    })
  } catch {
    // If formatting fails, return original
    return sql
  }
}

// Format a query (SQL or Cypher) for display, returning both formatted text and language
function formatQueryForDisplay(query: string): { formatted: string; language: 'sql' | 'cypher' } {
  if (isCypherQuery(query)) {
    return { formatted: formatCypher(query), language: 'cypher' }
  }
  return { formatted: formatSQLForDisplay(query), language: 'sql' }
}

// Light theme for syntax highlighting
const lightCodeTheme: { [key: string]: React.CSSProperties } = {
  'code[class*="language-"]': {
    color: 'hsl(220, 25%, 20%)',
    background: 'transparent',
    fontFamily: "'SF Mono', 'Menlo', 'Monaco', monospace",
    fontSize: '0.875em',
    textAlign: 'left',
    whiteSpace: 'pre',
    wordSpacing: 'normal',
    wordBreak: 'normal',
    wordWrap: 'normal',
    lineHeight: '1.6',
  },
  'pre[class*="language-"]': {
    color: 'hsl(220, 25%, 20%)',
    background: 'transparent',
    padding: '0',
    margin: '0',
    overflow: 'auto',
    borderRadius: '0',
  },
  comment: { color: 'hsl(220, 10%, 40%)' },
  prolog: { color: 'hsl(220, 10%, 40%)' },
  doctype: { color: 'hsl(220, 10%, 40%)' },
  cdata: { color: 'hsl(220, 10%, 40%)' },
  punctuation: { color: 'hsl(220, 15%, 40%)' },
  property: { color: 'hsl(200, 70%, 40%)' },
  tag: { color: 'hsl(200, 70%, 40%)' },
  boolean: { color: 'hsl(30, 70%, 45%)' },
  number: { color: 'hsl(30, 70%, 45%)' },
  constant: { color: 'hsl(30, 70%, 45%)' },
  symbol: { color: 'hsl(30, 70%, 45%)' },
  deleted: { color: 'hsl(0, 65%, 50%)' },
  selector: { color: 'hsl(100, 40%, 40%)' },
  'attr-name': { color: 'hsl(100, 40%, 40%)' },
  string: { color: 'hsl(100, 40%, 40%)' },
  char: { color: 'hsl(100, 40%, 40%)' },
  builtin: { color: 'hsl(100, 40%, 40%)' },
  inserted: { color: 'hsl(100, 40%, 40%)' },
  operator: { color: 'hsl(220, 15%, 40%)' },
  entity: { color: 'hsl(220, 15%, 40%)' },
  url: { color: 'hsl(220, 15%, 40%)' },
  atrule: { color: 'hsl(260, 50%, 50%)' },
  'attr-value': { color: 'hsl(260, 50%, 50%)' },
  keyword: { color: 'hsl(260, 50%, 50%)' },
  function: { color: 'hsl(200, 70%, 40%)' },
  'class-name': { color: 'hsl(200, 70%, 40%)' },
  regex: { color: 'hsl(30, 70%, 45%)' },
  important: { color: 'hsl(30, 70%, 45%)', fontWeight: 'bold' },
  variable: { color: 'hsl(30, 70%, 45%)' },
  bold: { fontWeight: 'bold' },
  italic: { fontStyle: 'italic' },
}

// Dark theme for syntax highlighting
const darkCodeTheme: { [key: string]: React.CSSProperties } = {
  'code[class*="language-"]': {
    color: 'hsl(220, 15%, 85%)',
    background: 'transparent',
    fontFamily: "'SF Mono', 'Menlo', 'Monaco', monospace",
    fontSize: '0.875em',
    textAlign: 'left',
    whiteSpace: 'pre',
    wordSpacing: 'normal',
    wordBreak: 'normal',
    wordWrap: 'normal',
    lineHeight: '1.6',
  },
  'pre[class*="language-"]': {
    color: 'hsl(220, 15%, 85%)',
    background: 'transparent',
    padding: '0',
    margin: '0',
    overflow: 'auto',
    borderRadius: '0',
  },
  comment: { color: 'hsl(220, 10%, 60%)' },
  prolog: { color: 'hsl(220, 10%, 60%)' },
  doctype: { color: 'hsl(220, 10%, 60%)' },
  cdata: { color: 'hsl(220, 10%, 60%)' },
  punctuation: { color: 'hsl(220, 15%, 65%)' },
  property: { color: 'hsl(200, 70%, 65%)' },
  tag: { color: 'hsl(200, 70%, 65%)' },
  boolean: { color: 'hsl(30, 70%, 65%)' },
  number: { color: 'hsl(30, 70%, 65%)' },
  constant: { color: 'hsl(30, 70%, 65%)' },
  symbol: { color: 'hsl(30, 70%, 65%)' },
  deleted: { color: 'hsl(0, 65%, 60%)' },
  selector: { color: 'hsl(100, 50%, 60%)' },
  'attr-name': { color: 'hsl(100, 50%, 60%)' },
  string: { color: 'hsl(100, 50%, 60%)' },
  char: { color: 'hsl(100, 50%, 60%)' },
  builtin: { color: 'hsl(100, 50%, 60%)' },
  inserted: { color: 'hsl(100, 50%, 60%)' },
  operator: { color: 'hsl(220, 15%, 65%)' },
  entity: { color: 'hsl(220, 15%, 65%)' },
  url: { color: 'hsl(220, 15%, 65%)' },
  atrule: { color: 'hsl(260, 60%, 70%)' },
  'attr-value': { color: 'hsl(260, 60%, 70%)' },
  keyword: { color: 'hsl(260, 60%, 70%)' },
  function: { color: 'hsl(200, 70%, 65%)' },
  'class-name': { color: 'hsl(200, 70%, 65%)' },
  regex: { color: 'hsl(30, 70%, 65%)' },
  important: { color: 'hsl(30, 70%, 65%)', fontWeight: 'bold' },
  variable: { color: 'hsl(30, 70%, 65%)' },
  bold: { fontWeight: 'bold' },
  italic: { fontStyle: 'italic' },
}

// Example questions for empty chat state
const EXAMPLE_QUESTIONS = [
  "How is the network doing?",
  "How many Solana validators are on DZ?",
  "Compare DZ to the public internet",
  "Which links have the highest utilization?",
  "What is the total stake connected to DZ?",
  "Which metros have the most validators?",
  "Show me link latency by metro pair",
  "Are there any links with packet loss?",
  "What's the average RTT for DZ links?",
  "Which validators connected recently?",
  "Which validators have the highest stake?",
  "Compare validator performance on vs off DZ",
  // Graph/topology questions
  "If the Hong Kong device goes down, what metros lose connectivity?",
  "What metros can I reach from Singapore?",
  "Show the paths between NYC and LON",
  "Show me all devices and links connected to the Singapore device",
]

// Randomly select n items from an array (Fisher-Yates shuffle, take first n)
function selectRandom<T>(arr: T[], n: number): T[] {
  const shuffled = [...arr]
  for (let i = shuffled.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]]
  }
  return shuffled.slice(0, n)
}

function CodeBlock({ language, children, isDark }: { language: string; children: string; isDark: boolean }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(children)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="relative group not-prose my-4 bg-muted px-5 py-4 overflow-auto">
      <button
        onClick={handleCopy}
        className="absolute right-2 top-2 p-1.5 rounded border border-border bg-card/80 text-muted-foreground hover:text-foreground hover:bg-accent-orange-20 transition-colors z-10"
        title="Copy code"
      >
        {copied ? (
          <Check className="h-4 w-4 text-accent" />
        ) : (
          <Copy className="h-4 w-4" />
        )}
      </button>
      <SyntaxHighlighter
        key={isDark ? 'dark' : 'light'}
        style={isDark ? darkCodeTheme : lightCodeTheme}
        language={language}
        PreTag="div"
        customStyle={{ background: 'transparent', padding: 0, margin: 0 }}
      >
        {children}
      </SyntaxHighlighter>
    </div>
  )
}

// Processing timeline component - shows thinking and query steps
interface ProcessingTimelineProps {
  steps: ProcessingStep[]
  isStreaming?: boolean
  onOpenInQueryEditor?: (query: string, type: 'sql' | 'cypher') => void
  onAskAboutQuery?: (question: string, sql: string, rowCount: number) => void
  highlightedQuery?: number | null
  onHighlightClear?: () => void
  isDark: boolean
}

function ProcessingTimeline({
  steps,
  isStreaming = false,
  onOpenInQueryEditor,
  onAskAboutQuery,
  highlightedQuery,
  onHighlightClear,
  isDark
}: ProcessingTimelineProps) {
  const [isExpanded, setIsExpanded] = useState(isStreaming)
  const [expandedQueries, setExpandedQueries] = useState<Set<number>>(new Set())
  const [expandedThinking, setExpandedThinking] = useState<Set<number>>(new Set())
  const queryRefs = useRef<Map<number, HTMLDivElement>>(new Map())

  // Auto-expand when streaming
  useEffect(() => {
    if (isStreaming) setIsExpanded(true)
  }, [isStreaming])

  // Auto-expand and scroll when a query is highlighted
  useEffect(() => {
    if (highlightedQuery !== null && highlightedQuery !== undefined) {
      setIsExpanded(true)
      setExpandedQueries(prev => new Set([...prev, highlightedQuery]))
      setTimeout(() => {
        const ref = queryRefs.current.get(highlightedQuery)
        if (ref) {
          ref.scrollIntoView({ behavior: 'smooth', block: 'center' })
        }
        if (onHighlightClear) {
          setTimeout(onHighlightClear, 1500)
        }
      }, 100)
    }
  }, [highlightedQuery, onHighlightClear])

  if (steps.length === 0 && !isStreaming) return null

  // Filter steps by type
  const querySteps = steps.filter((s): s is Extract<ProcessingStep, { type: 'sql_query' | 'cypher_query' }> =>
    s.type === 'sql_query' || s.type === 'cypher_query'
  )
  const completedQueries = querySteps.filter(q => q.status === 'completed').length
  const totalQueries = querySteps.length
  const docsSteps = steps.filter((s): s is Extract<ProcessingStep, { type: 'read_docs' }> => s.type === 'read_docs')
  const thinkingSteps = steps.filter(s => s.type === 'thinking')

  const toggleQuery = (index: number) => {
    setExpandedQueries(prev => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  const toggleThinking = (index: number) => {
    setExpandedThinking(prev => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  // Build summary text
  const getSummary = () => {
    if (isStreaming && steps.length === 0) return 'Processing...'
    if (isStreaming) {
      if (totalQueries === 0 && thinkingSteps.length > 0) return 'Thinking...'
      if (totalQueries > 0) return `Running ${totalQueries} ${totalQueries === 1 ? 'query' : 'queries'}...`
      if (docsSteps.length > 0) return 'Reading documentation...'
      return 'Processing...'
    }
    // Completed state - show what was done
    const parts: string[] = []
    if (thinkingSteps.length > 0) parts.push(`${thinkingSteps.length} ${thinkingSteps.length === 1 ? 'thought' : 'thoughts'}`)
    if (totalQueries > 0) parts.push(`${completedQueries} ${completedQueries === 1 ? 'query' : 'queries'}`)
    if (docsSteps.length > 0) parts.push(`${docsSteps.length} doc ${docsSteps.length === 1 ? 'lookup' : 'lookups'}`)
    if (parts.length === 0) return 'Processing complete'
    return parts.join(', ')
  }

  return (
    <div className={`border border-border rounded-lg overflow-hidden ${isStreaming ? 'border-accent/30' : ''}`}>
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:bg-secondary/50 transition-colors"
      >
        {isStreaming ? (
          <Loader2 className="w-4 h-4 text-accent animate-spin" />
        ) : isExpanded ? (
          <ChevronDown className="w-4 h-4" />
        ) : (
          <ChevronRight className="w-4 h-4" />
        )}
        <span>{getSummary()}</span>
      </button>

      {isExpanded && (
        <div className="border-t border-border divide-y divide-border">
          {steps.map((step, i) => {
            if (step.type === 'thinking') {
              // Track thinking step index for expansion state
              const thinkingIndex = steps.slice(0, i + 1).filter(s => s.type === 'thinking').length - 1
              const isThinkingExpanded = isStreaming || expandedThinking.has(thinkingIndex)
              const needsTruncation = step.content.length > 80 || step.content.includes('\n')

              return (
                <div key={i} className="px-3 py-2">
                  <button
                    onClick={() => toggleThinking(thinkingIndex)}
                    className="w-full flex items-start gap-2 text-left"
                  >
                    <Brain className="w-4 h-4 text-muted-foreground mt-0.5 flex-shrink-0" />
                    {isThinkingExpanded ? (
                      <p className="text-sm text-muted-foreground leading-relaxed">
                        {step.content}
                      </p>
                    ) : (
                      <p className="text-sm text-muted-foreground truncate">
                        {needsTruncation
                          ? step.content.split('\n')[0].slice(0, 80) + '...'
                          : step.content}
                      </p>
                    )}
                  </button>
                </div>
              )
            }

            if (step.type === 'read_docs') {
              return (
                <div key={i} className="px-3 py-2">
                  <div className="flex items-center gap-2">
                    <div className="flex-shrink-0">
                      {step.status === 'running' && (
                        <Loader2 className="w-4 h-4 text-accent animate-spin" />
                      )}
                      {step.status === 'completed' && (
                        <CheckCircle2 className="w-4 h-4 text-green-500" />
                      )}
                      {step.status === 'error' && (
                        <XCircle className="w-4 h-4 text-red-500" />
                      )}
                    </div>
                    <span className="text-sm text-muted-foreground">
                      Reading docs: <span className="text-foreground">{step.page}</span>
                    </span>
                  </div>
                  {step.error && (
                    <div className="text-sm text-red-500 mt-1 ml-6">{step.error}</div>
                  )}
                </div>
              )
            }

            // Query step (sql_query or cypher_query) - find its index among query steps for highlighting
            const queryIndex = querySteps.findIndex(q => q === step)
            const isHighlighted = highlightedQuery === queryIndex
            const queryCode = step.type === 'sql_query' ? step.sql : step.cypher

            return (
              <div
                key={i}
                ref={(el) => { if (el) queryRefs.current.set(queryIndex, el) }}
                className={`px-3 py-2 transition-colors ${isHighlighted ? 'bg-accent-orange-20' : ''}`}
              >
                <button
                  onClick={() => toggleQuery(queryIndex)}
                  className="w-full flex items-center gap-2 text-left"
                >
                  <div className="flex-shrink-0">
                    {step.status === 'running' && (
                      <Loader2 className="w-4 h-4 text-accent animate-spin" />
                    )}
                    {step.status === 'completed' && (
                      <CheckCircle2 className="w-4 h-4 text-green-500" />
                    )}
                    {step.status === 'error' && (
                      <XCircle className="w-4 h-4 text-red-500" />
                    )}
                  </div>
                  <span className="text-sm truncate flex-1">
                    {step.type === 'sql_query' && <span className="text-muted-foreground">[SQL] </span>}
                    {step.type === 'cypher_query' && <span className="text-muted-foreground">[Cypher] </span>}
                    {step.question}
                  </span>
                  {step.status !== 'running' && (
                    <span className={`text-xs ${step.error ? 'text-red-500' : 'text-green-600'}`}>
                      {step.error ? 'error' : `${step.rows ?? 0} rows`}
                    </span>
                  )}
                </button>

                {expandedQueries.has(queryIndex) && (
                  <div className="mt-2 ml-6">
                    <div className="relative">
                      {(() => {
                        const { formatted, language } = formatQueryForDisplay(queryCode)
                        return <CodeBlock language={language} isDark={isDark}>{formatted}</CodeBlock>
                      })()}
                      <div className="absolute right-10 top-2 flex items-center gap-1 z-10">
                        {onAskAboutQuery && step.status === 'completed' && !step.error && step.type === 'sql_query' && (
                          <button
                            onClick={() => onAskAboutQuery(step.question, step.sql, step.rows ?? 0)}
                            className="p-1.5 rounded border border-border bg-card/80 text-accent hover:bg-accent-orange-20 transition-colors flex items-center gap-1 text-xs"
                            title="Ask about this result"
                          >
                            <MessageCircle className="h-3 w-3" />
                            <span>Ask</span>
                          </button>
                        )}
                        {onOpenInQueryEditor && (
                          <button
                            onClick={() => onOpenInQueryEditor(queryCode, step.type === 'sql_query' ? 'sql' : 'cypher')}
                            className="p-1.5 rounded border border-border bg-card/80 text-muted-foreground hover:text-foreground hover:bg-accent-orange-20 transition-colors flex items-center gap-1 text-xs"
                            title="Open in Query Editor"
                          >
                            <ExternalLink className="h-3 w-3" />
                            <span>Edit</span>
                          </button>
                        )}
                      </div>
                    </div>
                    {step.error && (
                      <div className="text-sm text-red-500 mt-2">{step.error}</div>
                    )}
                    {!step.error && step.data && step.data.length > 0 && step.columns && (
                      <div className="mt-2 overflow-x-auto">
                        <table className="text-xs w-full">
                          <thead>
                            <tr className="border-b border-border">
                              {step.columns.map((col, ci) => (
                                <th key={ci} className="text-left px-2 py-1 font-medium text-muted-foreground">
                                  {col}
                                </th>
                              ))}
                            </tr>
                          </thead>
                          <tbody>
                            {step.data.slice(0, 10).map((row, ri) => (
                              <tr key={ri} className="border-b border-border/50">
                                {(row as unknown[]).map((cell, ci) => (
                                  <td key={ci} className="px-2 py-1 text-foreground">
                                    {isNeo4jValue(cell) ? (
                                      <span className="text-primary">{formatNeo4jValue(cell)}</span>
                                    ) : (
                                      String(cell ?? '')
                                    )}
                                  </td>
                                ))}
                              </tr>
                            ))}
                          </tbody>
                        </table>
                        {step.data.length > 10 && (
                          <div className="text-xs text-muted-foreground mt-1">
                            ... and {step.data.length - 10} more rows
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )
          })}
          {isStreaming && steps.length === 0 && (
            <div className="px-3 py-2 text-sm text-muted-foreground">
              Analyzing your question...
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// Renders text with clickable [Q1], [Q2] citations
function CitationText({ text, onCitationClick }: { text: string; onCitationClick?: (index: number) => void }) {
  if (!onCitationClick) return <>{text}</>

  // Parse citations like [Q1], [Q2], [Q1, Q3]
  const parts: (string | React.ReactNode)[] = []
  let lastIndex = 0
  const citationRegex = /\[Q(\d+)(?:,\s*Q(\d+))*\]/g
  let match

  while ((match = citationRegex.exec(text)) !== null) {
    // Add text before the citation
    if (match.index > lastIndex) {
      parts.push(text.slice(lastIndex, match.index))
    }

    // Parse citation numbers from the match
    const fullMatch = match[0]
    const numbers = fullMatch.match(/\d+/g)?.map(n => parseInt(n, 10) - 1) ?? [] // Convert to 0-indexed

    // Create clickable citation
    parts.push(
      <button
        key={match.index}
        onClick={() => numbers.length > 0 && onCitationClick(numbers[0])}
        className="text-accent hover:underline font-medium"
        title={`View query ${numbers.map(n => n + 1).join(', ')}`}
      >
        {fullMatch}
      </button>
    )

    lastIndex = match.index + fullMatch.length
  }

  // Add remaining text
  if (lastIndex < text.length) {
    parts.push(text.slice(lastIndex))
  }

  return <>{parts}</>
}

// Skeleton component for loading state
function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse bg-muted rounded ${className || ''}`} />
}

// Skeleton for chat loading state - shows placeholder messages
export function ChatSkeleton() {
  return (
    <div className="flex flex-col flex-1 min-h-0">
      <div className="flex-1 overflow-auto">
        <div className="max-w-3xl mx-auto min-h-full">
          <div className="px-4 py-8 space-y-6">
            {/* User message skeleton */}
            <div className="flex justify-end">
              <Skeleton className="h-10 w-48 rounded-3xl" />
            </div>
            {/* Assistant message skeleton */}
            <div className="px-1 space-y-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-5/6" />
              <Skeleton className="h-4 w-4/6" />
            </div>
            {/* Another user message */}
            <div className="flex justify-end">
              <Skeleton className="h-10 w-64 rounded-3xl" />
            </div>
            {/* Another assistant message */}
            <div className="px-1 space-y-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-3/4" />
            </div>
          </div>
        </div>
      </div>
      {/* Input area skeleton */}
      <div className="p-4 border-t border-border">
        <div className="max-w-3xl mx-auto">
          <Skeleton className="h-12 w-full rounded-2xl" />
        </div>
      </div>
    </div>
  )
}

interface ChatProps {
  messages: ChatMessage[]
  isPending: boolean
  processingSteps?: ProcessingStep[]
  onSendMessage: (message: string) => void
  onAbort: () => void
  onRetry?: () => void
  onOpenInQueryEditor?: (query: string, type: 'sql' | 'cypher') => void
}

export function Chat({ messages, isPending, processingSteps, onSendMessage, onAbort, onRetry, onOpenInQueryEditor }: ChatProps) {
  const [input, setInput] = useState('')
  const [highlightedQueries, setHighlightedQueries] = useState<Map<number, number | null>>(new Map()) // messageIndex -> queryIndex
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  // Randomly select example questions on mount (changes per session)
  const exampleQuestions = useMemo(() => selectRandom(EXAMPLE_QUESTIONS, 4), [])

  const handleAskAboutQuery = (question: string, _sql: string, rowCount: number) => {
    const prompt = `Tell me more about the "${question}" result (${rowCount} rows). What insights can you draw from this data?`
    setInput(prompt)
    inputRef.current?.focus()
  }
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const isAtBottomRef = useRef(true)

  // Track if user is at bottom of scroll container
  const handleScroll = () => {
    const container = scrollContainerRef.current
    if (!container) return
    // Consider "at bottom" if within 100px of the bottom
    const threshold = 100
    const atBottom = container.scrollHeight - container.scrollTop - container.clientHeight < threshold
    isAtBottomRef.current = atBottom
  }

  // Helper to scroll to bottom if user is at bottom
  const scrollToBottomIfNeeded = () => {
    if (isAtBottomRef.current) {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }

  // Focus input when starting a new chat (empty messages)
  useEffect(() => {
    if (messages.length === 0) {
      inputRef.current?.focus()
    }
  }, [messages.length])

  // Scroll to bottom when new messages arrive (always scroll for new messages)
  const prevMessagesLengthRef = useRef(messages.length)
  useEffect(() => {
    if (messages.length > prevMessagesLengthRef.current) {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
      isAtBottomRef.current = true // Reset to bottom after new message
    }
    prevMessagesLengthRef.current = messages.length
  }, [messages.length])

  // Scroll to bottom when a streaming message completes
  const lastMessage = messages[messages.length - 1]
  const lastMessageStatus = lastMessage?.status
  const prevLastMessageStatusRef = useRef(lastMessageStatus)
  useEffect(() => {
    // If status changed from 'streaming' to 'complete', scroll to bottom
    if (prevLastMessageStatusRef.current === 'streaming' && lastMessageStatus === 'complete') {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
      isAtBottomRef.current = true
    }
    prevLastMessageStatusRef.current = lastMessageStatus
  }, [lastMessageStatus])

  // Scroll to follow progress updates if user is at bottom
  useEffect(() => {
    if (isPending && processingSteps && processingSteps.length > 0) {
      scrollToBottomIfNeeded()
    }
  }, [isPending, processingSteps])

  // Focus input when response arrives (isPending goes from true to false)
  const prevIsPendingRef = useRef(isPending)
  useEffect(() => {
    if (prevIsPendingRef.current && !isPending) {
      inputRef.current?.focus()
    }
    prevIsPendingRef.current = isPending
  }, [isPending])

  // Disable input when pending
  const isDisabled = isPending

  const handleSend = () => {
    if (!input.trim() || isDisabled) return

    const userMessage = input.trim()
    setInput('')
    onSendMessage(userMessage)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  return (
    <div className="flex flex-col flex-1 min-h-0">
      {/* Centered chat pane */}
      <div ref={scrollContainerRef} onScroll={handleScroll} className="flex-1 overflow-auto">
        <div className="max-w-3xl mx-auto min-h-full">
          <div className="px-4 py-8 space-y-6">
            {messages.length === 0 && !isPending && (
              <div className="text-muted-foreground py-24 text-center">
                <p className="text-lg mb-2">What would you like to know?</p>
                <p className="text-sm mb-8">Ask questions about your data. I can run queries to find answers.</p>
                <div className="flex flex-wrap justify-center gap-2 max-w-xl mx-auto">
                  {exampleQuestions.map((question) => (
                    <button
                      key={question}
                      onClick={(e) => {
                        if (e.metaKey || e.ctrlKey) {
                          window.open(`/chat?q=${encodeURIComponent(question)}`, '_blank')
                        } else {
                          onSendMessage(question)
                        }
                      }}
                      className="px-3 py-1.5 text-sm border border-border rounded-full hover:bg-secondary hover:border-muted-foreground/30 transition-colors"
                    >
                      {question}
                    </button>
                  ))}
                </div>
              </div>
            )}
            {messages.map((msg, msgIndex) => {
              const handleCitationClick = (queryIndex: number) => {
                setHighlightedQueries(prev => new Map(prev).set(msgIndex, queryIndex))
              }

              const handleHighlightClear = () => {
                setHighlightedQueries(prev => {
                  const next = new Map(prev)
                  next.delete(msgIndex)
                  return next
                })
              }

              return (
                <div key={msgIndex}>
                  {msg.role === 'user' ? (
                    <div className="flex justify-end">
                      <div className="bg-secondary px-4 py-2.5 rounded-3xl max-w-[85%]">
                        <p className="text-sm whitespace-pre-wrap">{msg.content}</p>
                      </div>
                    </div>
                  ) : msg.status === 'error' ? (
                    // Error message with retry button
                    (() => {
                      const errorInfo = formatErrorMessage(msg.content)
                      return (
                        <div className="px-1">
                          <div className="flex items-start gap-3 px-4 py-3 bg-red-500/10 border border-red-500/20 rounded-lg">
                            <XCircle className="w-5 h-5 text-red-500 flex-shrink-0 mt-0.5" />
                            <div className="flex-1 min-w-0">
                              <p className="text-sm text-red-600 dark:text-red-400">{errorInfo.message}</p>
                              {errorInfo.isQuotaError && errorInfo.resetsAt && (
                                <p className="text-xs text-red-500/70 mt-1">
                                  Resets in {formatResetTime(errorInfo.resetsAt)}
                                </p>
                              )}
                              {onRetry && !errorInfo.isQuotaError && (
                                <button
                                  onClick={onRetry}
                                  disabled={isPending}
                                  className="mt-2 inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium text-red-600 dark:text-red-400 bg-red-500/10 hover:bg-red-500/20 border border-red-500/30 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                  <RotateCcw className="w-3.5 h-3.5" />
                                  Retry
                                </button>
                              )}
                            </div>
                          </div>
                        </div>
                      )
                    })()
                  ) : (
                    <div className="px-1">
                      {/* Processing timeline - shows for completed messages with processing steps */}
                      {/* Skip for streaming messages - the streaming timeline at the bottom handles those */}
                      {msg.status !== 'streaming' && msg.workflowData?.processingSteps && msg.workflowData.processingSteps.length > 0 && (
                        <div className="mb-3">
                          <ProcessingTimeline
                            steps={msg.workflowData.processingSteps}
                            isStreaming={false}
                            onOpenInQueryEditor={onOpenInQueryEditor}
                            onAskAboutQuery={handleAskAboutQuery}
                            highlightedQuery={highlightedQueries.get(msgIndex) ?? null}
                            onHighlightClear={handleHighlightClear}
                            isDark={isDark}
                          />
                        </div>
                      )}
                      <div className="prose max-w-none font-sans">
                        <ReactMarkdown
                          remarkPlugins={[remarkGfm]}
                          components={{
                            pre({ children }) {
                              // Just render children directly - CodeBlock handles all styling
                              return <>{children}</>
                            },
                            code({ className, children, ...props }) {
                              const match = /language-(\w+)/.exec(className || '')
                              const isInline = !match && !String(children).includes('\n')
                              return isInline ? (
                                <code className={className} {...props}>
                                  {children}
                                </code>
                              ) : (
                                <CodeBlock language={match ? match[1] : 'sql'} isDark={isDark}>
                                  {String(children).replace(/\n$/, '')}
                                </CodeBlock>
                              )
                            },
                            // Handle text nodes to make citations clickable
                            p({ children }) {
                              return (
                                <p>
                                  {typeof children === 'string' ? (
                                    <CitationText text={children} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                  ) : Array.isArray(children) ? (
                                    children.map((child, idx) =>
                                      typeof child === 'string' ? (
                                        <CitationText key={idx} text={child} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                      ) : (
                                        child
                                      )
                                    )
                                  ) : (
                                    children
                                  )}
                                </p>
                              )
                            },
                            li({ children }) {
                              return (
                                <li>
                                  {typeof children === 'string' ? (
                                    <CitationText text={children} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                  ) : Array.isArray(children) ? (
                                    children.map((child, idx) =>
                                      typeof child === 'string' ? (
                                        <CitationText key={idx} text={child} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                      ) : (
                                        child
                                      )
                                    )
                                  ) : (
                                    children
                                  )}
                                </li>
                              )
                            },
                            td({ children }) {
                              return (
                                <td>
                                  {typeof children === 'string' ? (
                                    <CitationText text={children} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                  ) : Array.isArray(children) ? (
                                    children.map((child, idx) =>
                                      typeof child === 'string' ? (
                                        <CitationText key={idx} text={child} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                      ) : (
                                        child
                                      )
                                    )
                                  ) : (
                                    children
                                  )}
                                </td>
                              )
                            },
                            th({ children }) {
                              return (
                                <th>
                                  {typeof children === 'string' ? (
                                    <CitationText text={children} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                  ) : Array.isArray(children) ? (
                                    children.map((child, idx) =>
                                      typeof child === 'string' ? (
                                        <CitationText key={idx} text={child} onCitationClick={msg.workflowData ? handleCitationClick : undefined} />
                                      ) : (
                                        child
                                      )
                                    )
                                  ) : (
                                    children
                                  )}
                                </th>
                              )
                            },
                          }}
                        >
                          {msg.content}
                        </ReactMarkdown>
                      </div>
                      {/* Follow-up suggestions */}
                      {msg.workflowData?.followUpQuestions && msg.workflowData.followUpQuestions.length > 0 && (
                        <div className="mt-4 flex flex-wrap gap-2">
                          {msg.workflowData.followUpQuestions.map((question, i) => (
                            <button
                              key={i}
                              onClick={(e) => {
                                if (e.metaKey || e.ctrlKey) {
                                  window.open(`/chat?q=${encodeURIComponent(question)}`, '_blank')
                                } else {
                                  onSendMessage(question)
                                }
                              }}
                              className="px-3 py-1.5 text-sm border border-border rounded-full hover:bg-secondary hover:border-muted-foreground/30 transition-colors text-muted-foreground hover:text-foreground"
                            >
                              {question}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
            {/* Streaming progress - shows ProcessingTimeline during streaming */}
            {isPending && (
              <div className="px-1 mt-3">
                <ProcessingTimeline
                  steps={processingSteps || []}
                  isStreaming={true}
                  onOpenInQueryEditor={onOpenInQueryEditor}
                  isDark={isDark}
                />
              </div>
            )}
            <div ref={messagesEndRef} />
          </div>
        </div>
      </div>

      {/* Input area */}
      <div className="pb-6 pt-2">
        <div className="max-w-3xl mx-auto px-4">
          <div className="relative rounded-[24px] border border-border bg-secondary overflow-hidden">
            <textarea
              ref={inputRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Ask about your data..."
              autoFocus
              disabled={isDisabled}
              rows={1}
              className="w-full bg-transparent px-4 pt-3.5 pb-2.5 pr-12 text-sm placeholder:text-muted-foreground focus:outline-none resize-none min-h-[44px] max-h-[200px] overflow-y-auto disabled:cursor-not-allowed disabled:opacity-50"
              style={{ height: 'auto' }}
              onInput={(e) => {
                const target = e.target as HTMLTextAreaElement
                target.style.height = 'auto'
                target.style.height = Math.min(target.scrollHeight, 200) + 'px'
              }}
            />
            {isPending ? (
              <button
                onClick={onAbort}
                className="absolute right-2 bottom-3 p-1.5 rounded-full bg-accent-orange-20 text-accent transition-colors hover:bg-accent-orange-50"
              >
                <Square className="h-4 w-4 fill-current" />
              </button>
            ) : (
              <button
                onClick={handleSend}
                disabled={!input.trim() || isDisabled}
                className="absolute right-2 bottom-3 p-1.5 rounded-full bg-accent text-white hover:bg-accent-orange-100 disabled:bg-muted-foreground/30 disabled:cursor-not-allowed transition-colors"
              >
                <ArrowUp className="h-4 w-4" />
              </button>
            )}
          </div>
          <p className="text-xs text-muted-foreground text-center mt-2">
            Enter to send, Shift+Enter for new line
          </p>
        </div>
      </div>
    </div>
  )
}
