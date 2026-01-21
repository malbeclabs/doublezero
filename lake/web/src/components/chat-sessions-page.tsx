import { useState, useRef } from 'react'
import { ChevronRight, MessageSquare, Trash2, Clock, Pencil, Check, X, RefreshCw, Sparkles, User } from 'lucide-react'
import { type ChatSession, getChatSessionPreview, formatSessionDate } from '@/lib/sessions'
import type { ChatMessage } from '@/lib/api'
import { ConfirmDialog } from './confirm-dialog'

interface ChatSessionsPageProps {
  sessions: ChatSession[]
  currentSessionId: string
  onSelectSession: (session: ChatSession) => void
  onDeleteSession: (sessionId: string) => void
  onUpdateSessionTitle?: (sessionId: string, title: string) => void
  onGenerateTitle?: (sessionId: string) => Promise<void>
}

export function ChatSessionsPage({
  sessions,
  currentSessionId,
  onSelectSession,
  onDeleteSession,
  onUpdateSessionTitle,
  onGenerateTitle,
}: ChatSessionsPageProps) {
  const [expandedSessionId, setExpandedSessionId] = useState<string | null>(null)
  const [deleteSessionId, setDeleteSessionId] = useState<string | null>(null)

  // Sort sessions by updatedAt, most recent first, and filter out empty sessions
  // Use id as tiebreaker for stable ordering when timestamps are equal
  const sortedSessions = [...sessions]
    .filter(s => s.messages.length > 0)
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
            No chat sessions yet. Start a conversation to create your first chat.
          </div>
        ) : (
          <div className="space-y-3">
            {sortedSessions.map(session => (
              <ChatSessionCard
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
          title="Delete chat"
          message={`Delete "${sessionToDelete.name || getChatSessionPreview(sessionToDelete)}"? This cannot be undone.`}
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

interface ChatSessionCardProps {
  session: ChatSession
  isCurrent: boolean
  isExpanded: boolean
  onToggleExpand: () => void
  onSelect: () => void
  onDelete: () => void
  onUpdateTitle?: (title: string) => void
  onGenerateTitle?: () => Promise<void>
}

function ChatSessionCard({
  session,
  isCurrent,
  isExpanded,
  onToggleExpand,
  onSelect,
  onDelete,
  onUpdateTitle,
  onGenerateTitle,
}: ChatSessionCardProps) {
  const [isEditing, setIsEditing] = useState(false)
  const [editValue, setEditValue] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const messageCount = session.messages.length
  const displayTitle = session.name || getChatSessionPreview(session)
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
                  placeholder="Chat title..."
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
                    window.open(`/chat/${session.id}`, '_blank')
                  } else {
                    onSelect()
                  }
                }}
                title="Click to open chat"
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
            <span className="flex items-center gap-1">
              <MessageSquare className="h-3 w-3" />
              {messageCount} messages
            </span>
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
              title="Rename chat"
            >
              <Pencil className="h-4 w-4" />
            </button>
          )}
          <button
            onClick={onDelete}
            className="p-1 text-muted-foreground hover:text-destructive transition-colors"
            title="Delete chat"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {isExpanded && session.messages.length > 0 && (
        <div className="border-t px-4 py-3 space-y-2 max-h-64 overflow-y-auto">
          <div className="text-xs text-muted-foreground mb-2">Conversation</div>
          {session.messages.map((message, index) => (
            <MessageItem key={index} message={message} />
          ))}
        </div>
      )}

      {isExpanded && session.messages.length === 0 && (
        <div className="border-t px-4 py-4 text-xs text-muted-foreground italic text-center">
          No messages in this chat yet
        </div>
      )}
    </div>
  )
}

function MessageItem({ message }: { message: ChatMessage }) {
  return (
    <div className="flex items-start gap-2 py-1.5 border-b border-dashed last:border-b-0">
      <span className={`flex-shrink-0 ${
        message.role === 'user' ? 'text-muted-foreground' : 'text-accent'
      }`}>
        {message.role === 'user' ? (
          <User className="h-3.5 w-3.5" />
        ) : (
          <Sparkles className="h-3.5 w-3.5" />
        )}
      </span>
      <div className="flex-1 min-w-0 text-xs truncate">
        {message.content.slice(0, 100)}{message.content.length > 100 ? '...' : ''}
      </div>
    </div>
  )
}
