import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ChevronDown, ChevronRight, History, Sparkles, Pencil } from 'lucide-react'

export interface GenerationRecord {
  id: string
  type: 'generation' | 'manual'
  timestamp: Date
  prompt?: string
  thinking?: string
  sql: string
  queryType?: 'sql' | 'cypher'  // Track whether this is SQL or Cypher
  provider?: string
  attempts?: number
  error?: string
}

interface SessionHistoryProps {
  history: GenerationRecord[]
  onRestoreQuery: (sql: string, queryType?: 'sql' | 'cypher') => void
}

export function SessionHistory({ history, onRestoreQuery }: SessionHistoryProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  if (history.length === 0) return null

  return (
    <div className="border border-border rounded-lg overflow-hidden bg-card">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full px-4 py-2.5 flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
      >
        <History className="h-4 w-4" />
        <span>Session History ({history.length})</span>
        {isOpen ? (
          <ChevronDown className="h-4 w-4 ml-auto" />
        ) : (
          <ChevronRight className="h-4 w-4 ml-auto" />
        )}
      </button>

      {isOpen && (
        <div className="border-t border-border max-h-64 overflow-y-auto">
          {history.map((record) => (
            <div key={record.id} className="border-b border-border last:border-b-0">
              <button
                onClick={() => setExpandedId(expandedId === record.id ? null : record.id)}
                className="w-full px-3 py-2 flex items-start gap-2 text-left hover:bg-muted transition-colors"
              >
                {record.type === 'generation' ? (
                  <Sparkles className="h-3.5 w-3.5 mt-0.5 flex-shrink-0 text-muted-foreground" />
                ) : (
                  <Pencil className="h-3.5 w-3.5 mt-0.5 flex-shrink-0 text-muted-foreground" />
                )}
                <div className="flex-1 min-w-0">
                  <div className="text-sm truncate">
                    {record.type === 'generation' ? record.prompt : 'Manual edit'}
                  </div>
                  <div className="text-xs text-muted-foreground flex items-center gap-2 mt-0.5">
                    <span>{formatTime(record.timestamp)}</span>
                    {record.type === 'generation' && record.attempts && record.attempts > 1 && (
                      <>
                        <span className="text-border">·</span>
                        <span>{record.attempts} attempts</span>
                      </>
                    )}
                    {record.error && (
                      <>
                        <span className="text-border">·</span>
                        <span className="text-destructive">error</span>
                      </>
                    )}
                  </div>
                </div>
                {expandedId === record.id ? (
                  <ChevronDown className="h-3.5 w-3.5 mt-0.5 flex-shrink-0 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5 mt-0.5 flex-shrink-0 text-muted-foreground" />
                )}
              </button>

              {expandedId === record.id && (
                <div className="px-3 pb-3 space-y-3">
                  {record.thinking && (
                    <div>
                      <div className="text-xs text-muted-foreground mb-1">Thinking</div>
                      <div className="prose prose-sm prose-neutral dark:prose-invert max-w-none bg-muted border p-2 max-h-32 overflow-y-auto text-muted-foreground [&>*]:text-muted-foreground [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>
                          {record.thinking}
                        </ReactMarkdown>
                      </div>
                    </div>
                  )}
                  <div>
                    <div className="text-xs text-muted-foreground mb-1">
                      {record.type === 'generation'
                        ? `Generated ${record.queryType === 'cypher' ? 'Cypher' : 'SQL'}`
                        : (record.queryType === 'cypher' ? 'Cypher' : 'SQL')}
                    </div>
                    <div className="font-mono text-xs bg-muted border p-2 max-h-32 overflow-y-auto whitespace-pre-wrap">
                      {record.sql}
                    </div>
                  </div>
                  {record.error && (
                    <div className="text-xs text-destructive">{record.error}</div>
                  )}
                  <button
                    onClick={() => onRestoreQuery(record.sql, record.queryType)}
                    className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                  >
                    Restore this query
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function formatTime(date: Date): string {
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
