import { useState, useRef } from 'react'
import { ChevronRight, Sparkles, Pencil, Trash2, Clock, Check, X, RefreshCw } from 'lucide-react'
import { type QuerySession, getSessionPreview, formatSessionDate } from '@/lib/sessions'
import type { GenerationRecord } from './session-history'
import { ConfirmDialog } from './confirm-dialog'

interface SessionsPageProps {
  sessions: QuerySession[]
  currentSessionId: string
  onSelectSession: (session: QuerySession) => void
  onDeleteSession: (sessionId: string) => void
  onUpdateSessionTitle?: (sessionId: string, title: string) => void
  onGenerateTitle?: (sessionId: string) => Promise<void>
}

export function SessionsPage({
  sessions,
  currentSessionId,
  onSelectSession,
  onDeleteSession,
  onUpdateSessionTitle,
  onGenerateTitle,
}: SessionsPageProps) {
  const [expandedSessionId, setExpandedSessionId] = useState<string | null>(null)
  const [deleteSessionId, setDeleteSessionId] = useState<string | null>(null)

  // Sort sessions by updatedAt, most recent first, and filter out empty sessions
  // Use id as tiebreaker for stable ordering when timestamps are equal
  const sortedSessions = [...sessions]
    .filter(s => s.history.length > 0)
    .sort((a, b) => {
      const timeDiff = b.updatedAt.getTime() - a.updatedAt.getTime()
      return timeDiff !== 0 ? timeDiff : a.id.localeCompare(b.id)
    })

  const sessionToDelete = deleteSessionId ? sortedSessions.find(s => s.id === deleteSessionId) : null

  return (
    <div className="flex-1 flex flex-col px-8 pb-8 overflow-hidden">
      <div className="flex-1 overflow-y-auto py-4">
        {sortedSessions.length === 0 ? (
          <div className="text-center text-muted-foreground text-sm italic py-12">
            No sessions yet. Start querying to create your first session.
          </div>
        ) : (
          <div className="space-y-3">
            {sortedSessions.map(session => (
              <SessionCard
                key={session.id}
                session={session}
                isCurrent={session.id === currentSessionId}
                isExpanded={expandedSessionId === session.id}
                onToggleExpand={() => setExpandedSessionId(
                  expandedSessionId === session.id ? null : session.id
                )}
                onSelect={() => onSelectSession(session)}
                onDelete={() => setDeleteSessionId(session.id)}
                onUpdateTitle={onUpdateSessionTitle ? (title) => onUpdateSessionTitle(session.id, title) : undefined}
                onGenerateTitle={onGenerateTitle ? () => onGenerateTitle(session.id) : undefined}
              />
            ))}
          </div>
        )}
      </div>

      {sessionToDelete && (
        <ConfirmDialog
          isOpen={true}
          title="Delete session"
          message={`Delete "${sessionToDelete.name || getSessionPreview(sessionToDelete)}"? This cannot be undone.`}
          onConfirm={() => {
            onDeleteSession(sessionToDelete.id)
            setDeleteSessionId(null)
          }}
          onCancel={() => setDeleteSessionId(null)}
        />
      )}
    </div>
  )
}

interface SessionCardProps {
  session: QuerySession
  isCurrent: boolean
  isExpanded: boolean
  onToggleExpand: () => void
  onSelect: () => void
  onDelete: () => void
  onUpdateTitle?: (title: string) => void
  onGenerateTitle?: () => Promise<void>
}

function SessionCard({
  session,
  isCurrent,
  isExpanded,
  onToggleExpand,
  onSelect,
  onDelete,
  onUpdateTitle,
  onGenerateTitle,
}: SessionCardProps) {
  const [isEditing, setIsEditing] = useState(false)
  const [editValue, setEditValue] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const generationCount = session.history.filter(h => h.type === 'generation').length
  const manualCount = session.history.filter(h => h.type === 'manual').length

  const displayTitle = session.name || getSessionPreview(session)
  const hasCustomTitle = !!session.name

  const startEditing = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (!onUpdateTitle) return
    setEditValue(session.name || '')
    setIsEditing(true)
    setTimeout(() => inputRef.current?.focus(), 0)
  }

  const saveEdit = () => {
    const newTitle = editValue.trim()
    if (onUpdateTitle && newTitle !== session.name) {
      onUpdateTitle(newTitle)
    }
    setIsEditing(false)
  }

  const cancelEdit = (e?: React.MouseEvent) => {
    e?.stopPropagation()
    setIsEditing(false)
    setEditValue('')
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      saveEdit()
    } else if (e.key === 'Escape') {
      cancelEdit()
    }
  }

  const handleGenerateTitle = async () => {
    if (!onGenerateTitle || isGenerating) return
    setIsGenerating(true)
    try {
      await onGenerateTitle()
    } finally {
      setIsGenerating(false)
    }
  }

  return (
    <div className={`border ${isCurrent ? 'border-accent bg-accent/5' : 'bg-secondary'}`}>
      <div className="flex items-start gap-3 p-4">
        <button
          onClick={onToggleExpand}
          className="mt-0.5 text-muted-foreground hover:text-foreground transition-colors"
        >
          <ChevronRight className={`h-4 w-4 transition-transform ${isExpanded ? 'rotate-90' : ''}`} />
        </button>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            {isEditing ? (
              <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
                <input
                  ref={inputRef}
                  type="text"
                  value={editValue}
                  onChange={(e) => setEditValue(e.target.value)}
                  onKeyDown={handleKeyDown}
                  onBlur={saveEdit}
                  placeholder="Session title..."
                  className="text-sm font-medium bg-transparent border-b border-foreground/30 focus:border-foreground outline-none px-1 py-0.5 min-w-[200px]"
                />
                <button
                  onClick={(e) => { e.stopPropagation(); saveEdit() }}
                  className="p-1 text-muted-foreground hover:text-foreground transition-colors"
                >
                  <Check className="h-3.5 w-3.5" />
                </button>
                <button
                  onClick={cancelEdit}
                  className="p-1 text-muted-foreground hover:text-foreground transition-colors"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            ) : (
              <span
                className={`font-medium truncate cursor-pointer hover:text-accent ${!hasCustomTitle ? 'text-muted-foreground' : ''}`}
                onClick={(e) => {
                  if (e.metaKey || e.ctrlKey) {
                    window.open(`/query/${session.id}`, '_blank')
                  } else {
                    onSelect()
                  }
                }}
                title="Click to open session"
              >
                {displayTitle}
              </span>
            )}
          </div>

          <div className="flex items-center gap-4 mt-1 text-xs text-muted-foreground">
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {formatSessionDate(session.updatedAt)}
            </span>
            {generationCount > 0 && (
              <span className="flex items-center gap-1">
                <Sparkles className="h-3 w-3" />
                {generationCount} generated
              </span>
            )}
            {manualCount > 0 && (
              <span className="flex items-center gap-1">
                <Pencil className="h-3 w-3" />
                {manualCount} manual
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2">
          {isCurrent && (
            <span className="px-3 py-1 text-xs bg-accent text-white">
              Current
            </span>
          )}
          {onGenerateTitle && !isEditing && (
            <button
              onClick={handleGenerateTitle}
              disabled={isGenerating}
              className="p-1 text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
              title="Generate title with AI"
            >
              <RefreshCw className={`h-4 w-4 ${isGenerating ? 'animate-spin' : ''}`} />
            </button>
          )}
          {onUpdateTitle && !isEditing && (
            <button
              onClick={startEditing}
              className="p-1 text-muted-foreground hover:text-foreground transition-colors"
              title="Rename session"
            >
              <Pencil className="h-4 w-4" />
            </button>
          )}
          <button
            onClick={onDelete}
            className="p-1 text-muted-foreground hover:text-destructive transition-colors"
            title="Delete session"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {isExpanded && session.history.length > 0 && (
        <div className="border-t px-4 py-3 space-y-2 max-h-64 overflow-y-auto">
          <div className="text-xs text-muted-foreground mb-2">Query History</div>
          {session.history.map((record, index) => (
            <HistoryItem key={record.id} record={record} index={session.history.length - index} />
          ))}
        </div>
      )}

      {isExpanded && session.history.length === 0 && (
        <div className="border-t px-4 py-4 text-xs text-muted-foreground italic text-center">
          No queries in this session yet
        </div>
      )}
    </div>
  )
}

function HistoryItem({ record, index }: { record: GenerationRecord; index: number }) {
  return (
    <div className="flex items-start gap-2 py-1.5 border-b border-dashed last:border-b-0">
      <span className="text-xs text-muted-foreground w-5 text-right flex-shrink-0">
        {index}.
      </span>
      {record.type === 'generation' ? (
        <Sparkles className="h-3.5 w-3.5 mt-0.5 text-muted-foreground flex-shrink-0" />
      ) : (
        <Pencil className="h-3.5 w-3.5 mt-0.5 text-muted-foreground flex-shrink-0" />
      )}
      <div className="flex-1 min-w-0">
        <div className="text-xs truncate">
          {record.type === 'generation' ? record.prompt : 'Manual edit'}
        </div>
        <div className="font-mono text-xs text-muted-foreground truncate mt-0.5">
          {record.sql.slice(0, 80)}{record.sql.length > 80 ? '...' : ''}
        </div>
      </div>
      <span className="text-xs text-muted-foreground flex-shrink-0">
        {formatTime(record.timestamp)}
      </span>
    </div>
  )
}

function formatTime(date: Date): string {
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
