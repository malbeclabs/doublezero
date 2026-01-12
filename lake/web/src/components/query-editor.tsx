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
      <div className="border bg-grey-10">
        <button
          onClick={() => setIsOpen(!isOpen)}
          className="w-full px-3 py-2 flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors bg-accent-orange-10"
        >
          <Code className="h-3.5 w-3.5" />
          <span>SQL Query</span>
          {isOpen ? (
            <ChevronDown className="h-3.5 w-3.5 ml-auto" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 ml-auto" />
          )}
        </button>

        {isOpen && (
          <div className="border-t">
            <div className="bg-grey-10">
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
            <div className="flex items-center justify-between px-3 py-2 border-t bg-grey-10">
              <div className="flex items-center gap-3">
                <button
                  onClick={() => runQuery(query)}
                  disabled={mutation.isPending || !query.trim()}
                  className="inline-flex items-center px-3 py-1.5 text-sm border border-accent text-accent hover:bg-accent hover:text-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                >
                  {mutation.isPending ? (
                    <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                  ) : (
                    <Play className="h-3.5 w-3.5 mr-1.5" />
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
                    className="px-3 py-1.5 text-sm border text-muted-foreground hover:text-foreground hover:border-foreground disabled:opacity-40 transition-colors"
                  >
                    Clear
                  </button>
                )}
              </div>
              {mutation.data && !error && (
                <span className="text-xs text-muted-foreground">
                  {mutation.data.row_count.toLocaleString()} rows · {mutation.data.elapsed_ms}ms
                </span>
              )}
            </div>
            {error && (
              <div className="py-3 px-4 border-t border-destructive/30 bg-destructive/5 text-destructive text-sm font-mono whitespace-pre-wrap">
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
