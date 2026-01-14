import { useState, useImperativeHandle, forwardRef, useMemo, useRef } from 'react'
import { useMutation } from '@tanstack/react-query'
import { executeQuery, type QueryResponse, type TableInfo } from '@/lib/api'
import { Play, Loader2, ChevronDown, ChevronRight, Code } from 'lucide-react'
import CodeMirror from '@uiw/react-codemirror'
import { sql } from '@codemirror/lang-sql'
import { keymap } from '@codemirror/view'
import { Prec } from '@codemirror/state'
import type { GenerationRecord } from './session-history'

interface QueryEditorProps {
  query: string
  onQueryChange: (query: string) => void
  onResults: (results: QueryResponse) => void
  onClear: () => void
  onManualRun?: (record: GenerationRecord) => void
  schema?: TableInfo[]
}

export interface QueryEditorHandle {
  run: (sql?: string) => void
}

export const QueryEditor = forwardRef<QueryEditorHandle, QueryEditorProps>(
  ({ query, onQueryChange, onResults, onClear, onManualRun, schema }, ref) => {
    const [error, setError] = useState<string | null>(null)
    const [isOpen, setIsOpen] = useState(true)
    const lastRecordedSqlRef = useRef<string>('')
    const runQueryRef = useRef<(sql: string, isAutoRun?: boolean) => void>(() => {})

    const mutation = useMutation({
      mutationFn: executeQuery,
      onSuccess: (data) => {
        if (data.error) {
          setError(data.error)
        } else {
          setError(null)
          onResults(data)
        }
      },
      onError: (err) => {
        setError(err instanceof Error ? err.message : 'Query failed')
      },
    })

    const runQuery = (sql: string, isAutoRun = false) => {
      if (!sql.trim()) return
      setError(null)

      // Record manual runs (not auto-runs from generation) when SQL has changed
      if (!isAutoRun && onManualRun && sql !== lastRecordedSqlRef.current) {
        onManualRun({
          id: crypto.randomUUID(),
          type: 'manual',
          timestamp: new Date(),
          sql: sql,
        })
      }
      lastRecordedSqlRef.current = sql

      mutation.mutate(sql)
    }

    // Keep ref updated with latest runQuery
    runQueryRef.current = runQuery

    useImperativeHandle(ref, () => ({
      run: (sql?: string) => {
        // When called from parent (auto-run), mark as auto-run
        const sqlToRun = sql ?? query
        lastRecordedSqlRef.current = sqlToRun
        runQuery(sqlToRun, true)
      },
    }))

    // Build schema config for SQL autocomplete
    const sqlSchema = useMemo(() => {
      if (!schema) return undefined
      const schemaObj: Record<string, string[]> = {}
      for (const table of schema) {
        schemaObj[table.name] = table.columns ?? []
      }
      return schemaObj
    }, [schema])

    // Create stable keymap extension that uses refs
    const extensions = useMemo(() => [
      sql({ schema: sqlSchema }),
      Prec.highest(keymap.of([
        {
          key: 'Mod-Enter',
          run: (view) => {
            const currentQuery = view.state.doc.toString()
            runQueryRef.current(currentQuery)
            return true
          },
        },
      ])),
    ], [sqlSchema])

    return (
      <div className="border border-border rounded-lg overflow-hidden bg-card">
        <button
          onClick={() => setIsOpen(!isOpen)}
          className="w-full px-4 py-2.5 flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <Code className="h-4 w-4" />
          <span>SQL Query</span>
          {isOpen ? (
            <ChevronDown className="h-4 w-4 ml-auto" />
          ) : (
            <ChevronRight className="h-4 w-4 ml-auto" />
          )}
        </button>

        {isOpen && (
          <div className="border-t border-border">
            <div className="bg-muted/30">
              <CodeMirror
                value={query}
                onChange={onQueryChange}
                extensions={extensions}
                placeholder="SELECT * FROM table LIMIT 100"
                minHeight="100px"
                basicSetup={{
                  lineNumbers: false,
                  foldGutter: false,
                  highlightActiveLine: false,
                }}
              />
            </div>
            <div className="flex items-center justify-between px-4 py-2.5 border-t border-border">
              <div className="flex items-center gap-3">
                <button
                  onClick={() => runQuery(query)}
                  disabled={mutation.isPending || !query.trim()}
                  className="inline-flex items-center px-3 py-1.5 text-sm rounded border border-foreground text-foreground hover:bg-foreground hover:text-background disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                >
                  {mutation.isPending ? (
                    <Loader2 className="h-4 w-4 mr-1.5 animate-spin" />
                  ) : (
                    <Play className="h-4 w-4 mr-1.5" />
                  )}
                  Run
                  <span className="ml-1.5 text-xs opacity-60">
                    {navigator.platform.includes('Mac') ? '⌘↵' : 'Ctrl+↵'}
                  </span>
                </button>
                {query.trim() && (
                  <button
                    onClick={onClear}
                    disabled={mutation.isPending}
                    className="px-3 py-1.5 text-sm rounded border border-border text-muted-foreground hover:text-foreground hover:border-foreground disabled:opacity-40 transition-colors"
                  >
                    Clear
                  </button>
                )}
              </div>
              {mutation.data && !error && (
                <span className="text-sm text-muted-foreground">
                  {mutation.data.row_count.toLocaleString()} rows · {mutation.data.elapsed_ms}ms
                </span>
              )}
            </div>
            {error && (
              <div className="py-3 px-4 border-t border-red-500/30 bg-red-500/5 text-red-600 dark:text-red-400 text-sm font-mono whitespace-pre-wrap">
                {error}
              </div>
            )}
          </div>
        )}
      </div>
    )
  }
)

QueryEditor.displayName = 'QueryEditor'
