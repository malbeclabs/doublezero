import { useRef, useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import type { ChatMessage, ChatPipelineData } from '@/lib/api'
import { ArrowUp, Square, Loader2, Copy, Check, ChevronDown, ChevronRight, ExternalLink, MessageCircle } from 'lucide-react'

// Custom minimal theme matching our design
const codeTheme: { [key: string]: React.CSSProperties } = {
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
  comment: { color: 'hsl(220, 10%, 55%)' },
  prolog: { color: 'hsl(220, 10%, 55%)' },
  doctype: { color: 'hsl(220, 10%, 55%)' },
  cdata: { color: 'hsl(220, 10%, 55%)' },
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

function CodeBlock({ language, children }: { language: string; children: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(children)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="relative group not-prose my-4 bg-[hsl(40,15%,96%)] px-5 py-4 overflow-auto">
      <button
        onClick={handleCopy}
        className="absolute right-2 top-2 p-1.5 rounded border border-border bg-white/80 text-muted-foreground hover:text-foreground hover:bg-accent-orange-20 transition-colors z-10"
        title="Copy code"
      >
        {copied ? (
          <Check className="h-4 w-4 text-accent" />
        ) : (
          <Copy className="h-4 w-4" />
        )}
      </button>
      <SyntaxHighlighter
        style={codeTheme}
        language={language}
        PreTag="div"
        customStyle={{ background: 'transparent', padding: 0, margin: 0 }}
      >
        {children}
      </SyntaxHighlighter>
    </div>
  )
}

interface PipelineDetailsProps {
  data: ChatPipelineData
  onOpenInQueryEditor?: (sql: string) => void
  onAskAboutQuery?: (question: string, sql: string, rowCount: number) => void
  highlightedQuery?: number | null
  onHighlightClear?: () => void
}

function PipelineDetails({ data, onOpenInQueryEditor, onAskAboutQuery, highlightedQuery, onHighlightClear }: PipelineDetailsProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [expandedQueries, setExpandedQueries] = useState<Set<number>>(new Set())
  const queryRefs = useRef<Map<number, HTMLDivElement>>(new Map())

  // Auto-expand and scroll when a query is highlighted
  useEffect(() => {
    if (highlightedQuery !== null && highlightedQuery !== undefined) {
      setIsExpanded(true)
      setExpandedQueries(prev => new Set([...prev, highlightedQuery]))

      // Scroll to the query after a brief delay for rendering
      setTimeout(() => {
        const ref = queryRefs.current.get(highlightedQuery)
        if (ref) {
          ref.scrollIntoView({ behavior: 'smooth', block: 'center' })
        }
        // Clear highlight after scrolling
        if (onHighlightClear) {
          setTimeout(onHighlightClear, 1500)
        }
      }, 100)
    }
  }, [highlightedQuery, onHighlightClear])

  const totalQueries = data.executedQueries?.length ?? 0
  const successfulQueries = data.executedQueries?.filter(q => !q.error).length ?? 0

  if (totalQueries === 0) return null

  const toggleQuery = (index: number) => {
    setExpandedQueries(prev => {
      const next = new Set(prev)
      if (next.has(index)) {
        next.delete(index)
      } else {
        next.add(index)
      }
      return next
    })
  }

  return (
    <div className="mt-3 border border-border rounded-lg overflow-hidden">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:bg-secondary/50 transition-colors"
      >
        {isExpanded ? (
          <ChevronDown className="w-4 h-4" />
        ) : (
          <ChevronRight className="w-4 h-4" />
        )}
        <span>
          {successfulQueries}/{totalQueries} queries executed
        </span>
      </button>

      {isExpanded && (
        <div className="border-t border-border">
          {/* Data Questions */}
          {data.dataQuestions && data.dataQuestions.length > 0 && (
            <div className="px-3 py-2 border-b border-border">
              <div className="text-xs font-medium text-muted-foreground mb-1">Data Questions</div>
              <ul className="text-sm space-y-1">
                {data.dataQuestions.map((dq, i) => (
                  <li key={i} className="text-foreground">
                    {i + 1}. {dq.question}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Executed Queries */}
          {data.executedQueries && data.executedQueries.length > 0 && (
            <div className="divide-y divide-border">
              {data.executedQueries.map((eq, i) => (
                <div
                  key={i}
                  ref={(el) => { if (el) queryRefs.current.set(i, el) }}
                  className={`px-3 py-2 transition-colors ${highlightedQuery === i ? 'bg-accent-orange-20' : ''}`}
                >
                  <button
                    onClick={() => toggleQuery(i)}
                    className="w-full flex items-center gap-2 text-left"
                  >
                    {expandedQueries.has(i) ? (
                      <ChevronDown className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                    ) : (
                      <ChevronRight className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                    )}
                    <span className="text-sm truncate flex-1">
                      {eq.question}
                    </span>
                    <span className={`text-xs ${eq.error ? 'text-red-500' : 'text-green-600'}`}>
                      {eq.error ? 'error' : `${eq.count} rows`}
                    </span>
                  </button>

                  {expandedQueries.has(i) && (
                    <div className="mt-2 ml-5">
                      <div className="relative">
                        <CodeBlock language="sql">{eq.sql}</CodeBlock>
                        <div className="absolute right-10 top-2 flex items-center gap-1 z-10">
                          {onAskAboutQuery && !eq.error && (
                            <button
                              onClick={() => onAskAboutQuery(eq.question, eq.sql, eq.count)}
                              className="p-1.5 rounded border border-border bg-white/80 text-accent hover:bg-accent-orange-20 transition-colors flex items-center gap-1 text-xs"
                              title="Ask about this result"
                            >
                              <MessageCircle className="h-3 w-3" />
                              <span>Ask</span>
                            </button>
                          )}
                          {onOpenInQueryEditor && (
                            <button
                              onClick={() => onOpenInQueryEditor(eq.sql)}
                              className="p-1.5 rounded border border-border bg-white/80 text-muted-foreground hover:text-foreground hover:bg-accent-orange-20 transition-colors flex items-center gap-1 text-xs"
                              title="Open in Query Editor"
                            >
                              <ExternalLink className="h-3 w-3" />
                              <span>Edit</span>
                            </button>
                          )}
                        </div>
                      </div>
                      {eq.error && (
                        <div className="text-sm text-red-500 mt-2">{eq.error}</div>
                      )}
                      {!eq.error && eq.rows && eq.rows.length > 0 && (
                        <div className="mt-2 overflow-x-auto">
                          <table className="text-xs w-full">
                            <thead>
                              <tr className="border-b border-border">
                                {eq.columns.map((col, ci) => (
                                  <th key={ci} className="text-left px-2 py-1 font-medium text-muted-foreground">
                                    {col}
                                  </th>
                                ))}
                              </tr>
                            </thead>
                            <tbody>
                              {eq.rows.slice(0, 10).map((row, ri) => (
                                <tr key={ri} className="border-b border-border/50">
                                  {(row as unknown[]).map((cell, ci) => (
                                    <td key={ci} className="px-2 py-1 text-foreground">
                                      {String(cell ?? '')}
                                    </td>
                                  ))}
                                </tr>
                              ))}
                            </tbody>
                          </table>
                          {eq.rows.length > 10 && (
                            <div className="text-xs text-muted-foreground mt-1">
                              ... and {eq.rows.length - 10} more rows
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
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

export interface ChatProgress {
  status?: string
  step?: string
  questionsCount?: number
  queriesCompleted?: number
  queriesTotal?: number
  lastQuery?: string
}

interface ChatProps {
  messages: ChatMessage[]
  isPending: boolean
  progress?: ChatProgress
  onSendMessage: (message: string) => void
  onAbort: () => void
  onOpenInQueryEditor?: (sql: string) => void
}

export function Chat({ messages, isPending, progress, onSendMessage, onAbort, onOpenInQueryEditor }: ChatProps) {
  const [input, setInput] = useState('')
  const [highlightedQueries, setHighlightedQueries] = useState<Map<number, number | null>>(new Map()) // messageIndex -> queryIndex
  const inputRef = useRef<HTMLTextAreaElement>(null)

  const handleAskAboutQuery = (question: string, _sql: string, rowCount: number) => {
    const prompt = `Tell me more about the "${question}" result (${rowCount} rows). What insights can you draw from this data?`
    setInput(prompt)
    inputRef.current?.focus()
  }
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const prevMessagesLengthRef = useRef(messages.length)

  // Focus input when starting a new chat (empty messages)
  useEffect(() => {
    if (messages.length === 0) {
      inputRef.current?.focus()
    }
  }, [messages.length])

  // Scroll to bottom when new messages arrive
  useEffect(() => {
    if (messages.length > prevMessagesLengthRef.current) {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
    prevMessagesLengthRef.current = messages.length
  }, [messages.length])

  // Focus input when response arrives (isPending goes from true to false)
  const prevIsPendingRef = useRef(isPending)
  useEffect(() => {
    if (prevIsPendingRef.current && !isPending) {
      inputRef.current?.focus()
    }
    prevIsPendingRef.current = isPending
  }, [isPending])

  const handleSend = () => {
    if (!input.trim() || isPending) return

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
      <div className="flex-1 overflow-auto">
        <div className="max-w-3xl mx-auto min-h-full">
          <div className="px-4 py-8 space-y-6">
            {messages.length === 0 && (
              <div className="text-muted-foreground py-24 text-center">
                <p className="text-lg mb-2">What would you like to know?</p>
                <p className="text-sm mb-8">Ask questions about your data. I can run queries to find answers.</p>
                <div className="flex flex-wrap justify-center gap-2 max-w-xl mx-auto">
                  {[
                    "How is the network doing?",
                    "How many Solana validators are on DZ?",
                    "Compare DZ to the public internet",
                    "Which links have the highest utilization?",
                  ].map((question) => (
                    <button
                      key={question}
                      onClick={() => onSendMessage(question)}
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
                      <div className="bg-gray-200 px-4 py-2.5 rounded-3xl max-w-[85%]">
                        <p className="text-sm whitespace-pre-wrap">{msg.content}</p>
                      </div>
                    </div>
                  ) : (
                    <div className="px-1">
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
                                <CodeBlock language={match ? match[1] : 'sql'}>
                                  {String(children).replace(/\n$/, '')}
                                </CodeBlock>
                              )
                            },
                            // Handle text nodes to make citations clickable
                            p({ children }) {
                              return (
                                <p>
                                  {typeof children === 'string' ? (
                                    <CitationText text={children} onCitationClick={msg.pipelineData ? handleCitationClick : undefined} />
                                  ) : Array.isArray(children) ? (
                                    children.map((child, idx) =>
                                      typeof child === 'string' ? (
                                        <CitationText key={idx} text={child} onCitationClick={msg.pipelineData ? handleCitationClick : undefined} />
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
                                    <CitationText text={children} onCitationClick={msg.pipelineData ? handleCitationClick : undefined} />
                                  ) : Array.isArray(children) ? (
                                    children.map((child, idx) =>
                                      typeof child === 'string' ? (
                                        <CitationText key={idx} text={child} onCitationClick={msg.pipelineData ? handleCitationClick : undefined} />
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
                          }}
                        >
                          {msg.content}
                        </ReactMarkdown>
                      </div>
                      {msg.pipelineData && (
                        <PipelineDetails
                          data={msg.pipelineData}
                          onOpenInQueryEditor={onOpenInQueryEditor}
                          onAskAboutQuery={handleAskAboutQuery}
                          highlightedQuery={highlightedQueries.get(msgIndex) ?? null}
                          onHighlightClear={handleHighlightClear}
                        />
                      )}
                    </div>
                  )}
                </div>
              )
            })}
            {isPending && (
              <div className="px-1">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="w-4 h-4 animate-spin" />
                  <span className="text-sm">
                    {progress?.status || 'Thinking...'}
                  </span>
                </div>
                {progress?.step === 'executing' && progress.queriesTotal && progress.queriesTotal > 0 && (
                  <div className="mt-2 ml-6">
                    <div className="flex items-center gap-2 mb-1">
                      <div className="flex-1 h-1.5 bg-secondary rounded-full overflow-hidden">
                        <div
                          className="h-full bg-accent transition-all duration-300"
                          style={{ width: `${((progress.queriesCompleted || 0) / progress.queriesTotal) * 100}%` }}
                        />
                      </div>
                      <span className="text-xs text-muted-foreground">
                        {progress.queriesCompleted || 0}/{progress.queriesTotal}
                      </span>
                    </div>
                    {progress.lastQuery && (
                      <div className="text-xs text-muted-foreground truncate">
                        {progress.lastQuery}
                      </div>
                    )}
                  </div>
                )}
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
              rows={1}
              className="w-full bg-transparent px-4 pt-3.5 pb-2.5 pr-12 text-sm placeholder:text-muted-foreground focus:outline-none resize-none min-h-[44px] max-h-[200px] overflow-y-auto"
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
                disabled={!input.trim()}
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
