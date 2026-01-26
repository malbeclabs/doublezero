import { useState, useImperativeHandle, forwardRef, useMemo, useRef, useCallback } from 'react'
import { useMutation } from '@tanstack/react-query'
import {
  executeSqlQuery,
  executeCypherQuery,
  type QueryResponse,
  type CypherQueryResponse,
  type TableInfo,
  type QueryMode,
} from '@/lib/api'
import { Play, Loader2, ChevronDown, ChevronRight, Code, Sparkles } from 'lucide-react'
import CodeMirror from '@uiw/react-codemirror'
import { sql } from '@codemirror/lang-sql'
import { cypher } from '@/lib/cypher-lang'
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
  // Mode control props (optional - defaults to SQL only if not provided)
  mode?: QueryMode
  onModeChange?: (mode: QueryMode) => void
  activeMode?: 'sql' | 'cypher'
  onActiveModeChange?: (mode: 'sql' | 'cypher') => void
}

export interface QueryEditorHandle {
  run: (sql?: string) => void
  setActiveMode: (mode: 'sql' | 'cypher') => void
}

// Convert Cypher response (map rows) to SQL response format (array rows)
function cypherToQueryResponse(response: CypherQueryResponse): QueryResponse {
  const rows = (response.rows ?? []).map(row => (response.columns ?? []).map(col => row[col]))
  return {
    columns: response.columns ?? [],
    rows,
    row_count: response.row_count,
    elapsed_ms: response.elapsed_ms,
    error: response.error,
  }
}

export const QueryEditor = forwardRef<QueryEditorHandle, QueryEditorProps>(
  ({
    query,
    onQueryChange,
    onResults,
    onClear,
    onManualRun,
    schema,
    mode: externalMode,
    onModeChange,
    activeMode: externalActiveMode,
    onActiveModeChange,
  }, ref) => {
    const [error, setError] = useState<string | null>(null)
    const [isOpen, setIsOpen] = useState(true)
    const lastRecordedSqlRef = useRef<string>('')
    const runQueryRef = useRef<(sql: string, isAutoRun?: boolean) => void>(() => {})

    // Internal mode state (used if no external control provided)
    const [internalMode, setInternalMode] = useState<QueryMode>('auto')
    const [internalActiveMode, setInternalActiveMode] = useState<'sql' | 'cypher'>('sql')

    // Use external mode if provided, otherwise use internal
    const mode = externalMode ?? internalMode
    const activeMode = externalActiveMode ?? internalActiveMode

    const setMode = useCallback((newMode: QueryMode) => {
      if (onModeChange) {
        onModeChange(newMode)
      } else {
        setInternalMode(newMode)
      }
      // When explicitly selecting SQL or Cypher mode, also update active mode
      if (newMode === 'sql' || newMode === 'cypher') {
        if (onActiveModeChange) {
          onActiveModeChange(newMode)
        } else {
          setInternalActiveMode(newMode)
        }
      }
    }, [onModeChange, onActiveModeChange])

    const setActiveMode = useCallback((newActiveMode: 'sql' | 'cypher') => {
      if (onActiveModeChange) {
        onActiveModeChange(newActiveMode)
      } else {
        setInternalActiveMode(newActiveMode)
      }
    }, [onActiveModeChange])

    // SQL mutation
    const sqlMutation = useMutation({
      mutationFn: executeSqlQuery,
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

    // Cypher mutation
    const cypherMutation = useMutation({
      mutationFn: executeCypherQuery,
      onSuccess: (data) => {
        if (data.error) {
          setError(data.error)
        } else {
          setError(null)
          onResults(cypherToQueryResponse(data))
        }
      },
      onError: (err) => {
        setError(err instanceof Error ? err.message : 'Query failed')
      },
    })

    const isPending = sqlMutation.isPending || cypherMutation.isPending
    const lastResult = activeMode === 'sql' ? sqlMutation.data : (cypherMutation.data ? cypherToQueryResponse(cypherMutation.data) : undefined)

    const runQuery = (queryText: string, isAutoRun = false) => {
      if (!queryText.trim()) return
      setError(null)

      // Record manual runs (not auto-runs from generation) when query has changed
      if (!isAutoRun && onManualRun && queryText !== lastRecordedSqlRef.current) {
        onManualRun({
          id: crypto.randomUUID(),
          type: 'manual',
          timestamp: new Date(),
          sql: queryText,
          queryType: activeMode,
        })
      }
      lastRecordedSqlRef.current = queryText

      // Execute based on active mode
      if (activeMode === 'cypher') {
        cypherMutation.mutate(queryText)
      } else {
        sqlMutation.mutate(queryText)
      }
    }

    // Keep ref updated with latest runQuery
    runQueryRef.current = runQuery

    useImperativeHandle(ref, () => ({
      run: (sql?: string) => {
        // When called from parent (auto-run), mark as auto-run
        const queryToRun = sql ?? query
        lastRecordedSqlRef.current = queryToRun
        runQuery(queryToRun, true)
      },
      setActiveMode,
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
      activeMode === 'sql' ? sql({ schema: sqlSchema }) : cypher(),
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
    ], [sqlSchema, activeMode])

    // Mode selector button styling
    const getModeButtonClass = (buttonMode: QueryMode) => {
      const isSelected = mode === buttonMode
      return `px-2 py-1 text-xs rounded transition-colors ${
        isSelected
          ? 'bg-background text-foreground shadow-sm'
          : 'text-muted-foreground hover:text-foreground'
      }`
    }

    // Determine header text based on mode
    const headerText = activeMode === 'cypher' ? 'Cypher Query' : 'SQL Query'
    const placeholder = activeMode === 'cypher'
      ? 'MATCH (n) RETURN n LIMIT 100'
      : 'SELECT * FROM table LIMIT 100'

    return (
      <div className="border border-border rounded-lg overflow-hidden bg-card">
        <div className="w-full px-4 py-2.5 flex items-center gap-2 text-sm text-muted-foreground">
          <button
            onClick={() => setIsOpen(!isOpen)}
            className="flex items-center gap-2 hover:text-foreground transition-colors"
          >
            <Code className="h-4 w-4" />
            <span>{headerText}</span>
            {isOpen ? (
              <ChevronDown className="h-4 w-4" />
            ) : (
              <ChevronRight className="h-4 w-4" />
            )}
          </button>

          {/* Mode selector - shown when onModeChange is provided */}
          {onModeChange && (
            <div className="ml-auto flex items-center gap-1 bg-muted rounded-md p-0.5">
              <button
                onClick={() => setMode('auto')}
                className={`${getModeButtonClass('auto')} flex items-center gap-1`}
              >
                Auto
                <Sparkles className="h-3 w-3" />
              </button>
              <button
                onClick={() => setMode('sql')}
                className={getModeButtonClass('sql')}
              >
                SQL
              </button>
              <button
                onClick={() => setMode('cypher')}
                className={getModeButtonClass('cypher')}
              >
                Cypher
              </button>
            </div>
          )}
        </div>

        {isOpen && (
          <div className="border-t border-border">
            <div className="bg-muted/30">
              <CodeMirror
                value={query}
                onChange={onQueryChange}
                extensions={extensions}
                placeholder={placeholder}
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
                  disabled={isPending || !query.trim()}
                  className="inline-flex items-center px-3 py-1.5 text-sm rounded border border-foreground text-foreground hover:bg-foreground hover:text-background disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                >
                  {isPending ? (
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
                    disabled={isPending}
                    className="px-3 py-1.5 text-sm rounded border border-border text-muted-foreground hover:text-foreground hover:border-foreground disabled:opacity-40 transition-colors"
                  >
                    Clear
                  </button>
                )}
              </div>
              {lastResult && !error && (
                <span className="text-sm text-muted-foreground">
                  {lastResult.row_count.toLocaleString()} rows · {lastResult.elapsed_ms}ms
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
