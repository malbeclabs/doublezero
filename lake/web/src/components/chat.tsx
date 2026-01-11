import { useRef, useEffect, useCallback, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { sendChatMessage, type ChatMessage } from '@/lib/api'
import { ArrowUp, Square, Loader2, Copy, Check } from 'lucide-react'

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

interface ChatProps {
  messages: ChatMessage[]
  onMessagesChange: (messages: ChatMessage[]) => void
}

export function Chat({ messages, onMessagesChange }: ChatProps) {
  const [input, setInput] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const abortControllerRef = useRef<AbortController | null>(null)
  const messagesRef = useRef(messages)

  // Keep ref in sync with messages
  useEffect(() => {
    messagesRef.current = messages
  }, [messages])

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])


  const mutation = useMutation({
    mutationFn: ({ message, signal }: { message: string; signal: AbortSignal }) =>
      sendChatMessage(message, messagesRef.current, signal),
    onSuccess: (data) => {
      if (data.error) {
        onMessagesChange([
          ...messagesRef.current,
          { role: 'assistant', content: `Error: ${data.error}` },
        ])
      } else {
        onMessagesChange([
          ...messagesRef.current,
          { role: 'assistant', content: data.response },
        ])
      }
      // Scroll to show new message and refocus input
      setTimeout(() => {
        scrollToBottom()
        inputRef.current?.focus()
      }, 50)
    },
    onError: (err) => {
      // Don't show error message if request was aborted
      if (err instanceof Error && err.name === 'AbortError') {
        return
      }
      onMessagesChange([
        ...messagesRef.current,
        {
          role: 'assistant',
          content: `Error: ${err instanceof Error ? err.message : 'Failed to send message'}`,
        },
      ])
      // Refocus input after error
      setTimeout(() => inputRef.current?.focus(), 50)
    },
  })

  const handleSend = () => {
    if (!input.trim() || mutation.isPending) return

    const userMessage = input.trim()
    onMessagesChange([...messages, { role: 'user', content: userMessage }])
    setInput('')

    // Scroll to show user message
    setTimeout(scrollToBottom, 50)

    abortControllerRef.current = new AbortController()
    mutation.mutate({ message: userMessage, signal: abortControllerRef.current.signal })
  }

  const handleStop = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
      abortControllerRef.current = null
    }
  }, [])

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
                <p className="text-sm">Ask questions about your data. I can run queries to find answers.</p>
              </div>
            )}
            {messages.map((msg, i) => (
              <div key={i}>
                {msg.role === 'user' ? (
                  <div className="flex justify-end">
                    <div className="bg-gray-200 px-4 py-2.5 rounded-3xl max-w-[85%]">
                      <p className="text-sm whitespace-pre-wrap">{msg.content}</p>
                    </div>
                  </div>
                ) : (
                  <div className="prose max-w-none px-1 font-sans">
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
                      }}
                    >
                      {msg.content}
                    </ReactMarkdown>
                  </div>
                )}
              </div>
            ))}
            {mutation.isPending && (
              <div className="flex items-center gap-2 text-muted-foreground px-1">
                <Loader2 className="w-4 h-4 animate-spin" />
                <span className="text-sm">Thinking...</span>
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
            <input
              ref={inputRef}
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Ask about your data..."
              disabled={mutation.isPending}
              autoFocus
              className="w-full bg-transparent px-4 py-3 pr-12 text-sm placeholder:text-muted-foreground focus:outline-none disabled:opacity-50"
            />
            {mutation.isPending ? (
              <button
                onClick={handleStop}
                className="absolute right-2 bottom-2 p-1.5 rounded-full bg-accent-orange-20 text-accent transition-colors hover:bg-accent-orange-50"
              >
                <Square className="h-4 w-4 fill-current" />
              </button>
            ) : (
              <button
                onClick={handleSend}
                disabled={!input.trim()}
                className="absolute right-2 bottom-2 p-1.5 rounded-full bg-accent text-white hover:bg-accent-orange-100 disabled:bg-muted-foreground/30 disabled:cursor-not-allowed transition-colors"
              >
                <ArrowUp className="h-4 w-4" />
              </button>
            )}
          </div>
          <p className="text-xs text-muted-foreground text-center mt-2">
            Press Enter to send
          </p>
        </div>
      </div>
    </div>
  )
}
