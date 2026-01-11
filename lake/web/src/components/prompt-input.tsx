import { useState, useRef } from 'react'
import { generateSQLStream, type HistoryMessage } from '@/lib/api'
import { Sparkles, Loader2 } from 'lucide-react'
import type { GenerationRecord } from './session-history'

interface PromptInputProps {
  currentQuery: string
  conversationHistory: HistoryMessage[]
  onGenerated: (sql: string, autoRun: boolean) => void
  onGenerationComplete: (record: GenerationRecord) => void
  autoRun: boolean
  onAutoRunChange: (autoRun: boolean) => void
}

export function PromptInput({ currentQuery, conversationHistory, onGenerated, onGenerationComplete, autoRun, onAutoRunChange }: PromptInputProps) {
  const [prompt, setPrompt] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [isGenerating, setIsGenerating] = useState(false)
  const [thinking, setThinking] = useState('')
  const [status, setStatus] = useState<string | null>(null)
  const thinkingRef = useRef<HTMLDivElement>(null)
  const thinkingAccumulatorRef = useRef('')
  const providerRef = useRef('')
  const attemptsRef = useRef(1)
  const currentPromptRef = useRef('')

  const handleGenerate = async () => {
    if (!prompt.trim() || isGenerating) return

    setError(null)
    setThinking('')
    setStatus('Starting...')
    setIsGenerating(true)
    thinkingAccumulatorRef.current = ''
    providerRef.current = ''
    attemptsRef.current = 1
    currentPromptRef.current = prompt

    try {
      await generateSQLStream(prompt, currentQuery || undefined, conversationHistory, {
        onToken: (token) => {
          setThinking(prev => prev + token)
          thinkingAccumulatorRef.current += token
          // Auto-scroll thinking area
          if (thinkingRef.current) {
            thinkingRef.current.scrollTop = thinkingRef.current.scrollHeight
          }
        },
        onStatus: (s) => {
          if (s.status === 'generating') {
            setStatus('Generating...')
            if (s.provider) providerRef.current = s.provider
          } else if (s.status === 'validating') {
            setStatus('Validating query...')
          } else if (s.status === 'retrying') {
            setStatus(`Attempt ${s.attempt}: Fixing error...`)
            if (s.attempt) attemptsRef.current = s.attempt
            setThinking('')
            // Keep accumulating thinking across retries
            thinkingAccumulatorRef.current += '\n\n--- Retry ---\n\n'
          }
        },
        onDone: (result) => {
          setIsGenerating(false)
          setStatus(null)
          setThinking('')

          // Create history record
          const record: GenerationRecord = {
            id: crypto.randomUUID(),
            type: 'generation',
            timestamp: new Date(),
            prompt: currentPromptRef.current,
            thinking: thinkingAccumulatorRef.current,
            sql: result.sql,
            provider: providerRef.current,
            attempts: attemptsRef.current,
            error: result.error,
          }
          onGenerationComplete(record)

          if (result.error) {
            setError(result.error)
            if (result.sql) {
              onGenerated(result.sql, false)
            }
          } else {
            setError(null)
            onGenerated(result.sql, autoRun)
            setPrompt('')
          }
        },
        onError: (err) => {
          setIsGenerating(false)
          setStatus(null)
          setThinking('')
          setError(err)
        }
      })
    } catch (err) {
      setIsGenerating(false)
      setStatus(null)
      setThinking('')
      setError(err instanceof Error ? err.message : 'Failed to generate SQL')
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleGenerate()
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex gap-3 items-center">
        <div className="flex-1 flex items-center gap-3 border bg-secondary px-3 py-2">
          <Sparkles className="h-4 w-4 text-muted-foreground flex-shrink-0" />
          <input
            type="text"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={currentQuery ? "Modify the query..." : "Describe what you want to query..."}
            className="flex-1 bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none"
            disabled={isGenerating}
          />
        </div>
        <button
          onClick={handleGenerate}
          disabled={isGenerating || !prompt.trim()}
          className="px-3 py-2 text-sm border text-muted-foreground hover:text-foreground hover:border-foreground disabled:opacity-40 disabled:cursor-not-allowed transition-colors flex items-center gap-1.5"
        >
          {isGenerating && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Generate
        </button>
        <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer select-none border px-3 py-2">
          <input
            type="checkbox"
            checked={autoRun}
            onChange={(e) => onAutoRunChange(e.target.checked)}
            className="h-3.5 w-3.5 accent-accent"
          />
          Auto-run
        </label>
      </div>
      {(thinking || status) && (
        <div className="border bg-secondary p-3">
          {status && (
            <div className="text-xs text-muted-foreground mb-2 flex items-center gap-2">
              <Loader2 className="h-3 w-3 animate-spin" />
              {status}
            </div>
          )}
          {thinking && (
            <div
              ref={thinkingRef}
              className="font-mono text-xs text-muted-foreground max-h-24 overflow-y-auto whitespace-pre-wrap"
            >
              {thinking}
            </div>
          )}
        </div>
      )}
      {error && (
        <div className="text-sm text-destructive">{error}</div>
      )}
    </div>
  )
}
