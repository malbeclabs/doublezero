import { useState } from 'react'
import { Sun, Moon, Monitor, Trash2 } from 'lucide-react'
import { useTheme } from '@/hooks/use-theme'
import { loadSessions, loadChatSessions } from '@/lib/sessions'
import { ConfirmDialog } from './confirm-dialog'

const themeOptions = [
  { value: 'light' as const, label: 'Light', icon: Sun, description: 'Always use light mode' },
  { value: 'dark' as const, label: 'Dark', icon: Moon, description: 'Always use dark mode' },
  { value: 'system' as const, label: 'System', icon: Monitor, description: 'Follow your system preference' },
]

export function SettingsPage() {
  const { theme, setTheme } = useTheme()
  const [refreshKey, setRefreshKey] = useState(0)
  const [confirmDialog, setConfirmDialog] = useState<{
    type: 'chat' | 'query'
  } | null>(null)

  // Get local storage stats (refreshKey forces re-read after clear)
  const querySessions = loadSessions()
  const chatSessions = loadChatSessions()
  const queryCount = querySessions.reduce((acc, s) => acc + s.history.length, 0)
  const chatCount = chatSessions.reduce((acc, s) => acc + s.messages.length, 0)

  const clearChatSessions = () => {
    localStorage.removeItem('lake-chat-sessions')
    localStorage.removeItem('lake-current-chat-session-id')
    setConfirmDialog(null)
    setRefreshKey(k => k + 1)
  }

  const clearQuerySessions = () => {
    localStorage.removeItem('lake-query-sessions')
    localStorage.removeItem('lake-current-session-id')
    setConfirmDialog(null)
    setRefreshKey(k => k + 1)
  }

  return (
    <div className="flex-1 overflow-auto" key={refreshKey}>
      <div className="max-w-2xl mx-auto px-6 py-8">
        <h1 className="text-2xl font-semibold text-foreground mb-8">Settings</h1>

        {/* Theme Section */}
        <section className="mb-10">
          <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide mb-4">
            Appearance
          </h2>
          <div className="bg-card border border-border rounded-lg overflow-hidden">
            {themeOptions.map((option, idx) => (
              <button
                key={option.value}
                onClick={() => setTheme(option.value)}
                className={`w-full flex items-center gap-4 px-4 py-3 text-left transition-colors hover:bg-muted/50 ${
                  idx !== 0 ? 'border-t border-border' : ''
                } ${theme === option.value ? 'bg-muted/30' : ''}`}
              >
                <div className={`p-2 rounded-md ${theme === option.value ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}>
                  <option.icon className="h-4 w-4" />
                </div>
                <div className="flex-1">
                  <div className="text-sm font-medium text-foreground">{option.label}</div>
                  <div className="text-xs text-muted-foreground">{option.description}</div>
                </div>
                {theme === option.value && (
                  <div className="w-2 h-2 rounded-full bg-primary" />
                )}
              </button>
            ))}
          </div>
        </section>

        {/* Storage Section */}
        <section className="mb-10">
          <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide mb-4">
            Local Storage
          </h2>
          <div className="bg-card border border-border rounded-lg overflow-hidden">
            <div className="px-4 py-3 flex items-center justify-between">
              <div>
                <div className="text-sm font-medium text-foreground">Chat Sessions</div>
                <div className="text-xs text-muted-foreground">{chatSessions.length} sessions, {chatCount} messages</div>
              </div>
              <button
                onClick={() => setConfirmDialog({ type: 'chat' })}
                disabled={chatSessions.length === 0}
                className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs text-muted-foreground hover:text-red-500 hover:bg-red-500/10 disabled:opacity-50 disabled:hover:text-muted-foreground disabled:hover:bg-transparent rounded transition-colors"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Clear
              </button>
            </div>
            <div className="px-4 py-3 flex items-center justify-between border-t border-border">
              <div>
                <div className="text-sm font-medium text-foreground">Query Sessions</div>
                <div className="text-xs text-muted-foreground">{querySessions.length} sessions, {queryCount} queries</div>
              </div>
              <button
                onClick={() => setConfirmDialog({ type: 'query' })}
                disabled={querySessions.length === 0}
                className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs text-muted-foreground hover:text-red-500 hover:bg-red-500/10 disabled:opacity-50 disabled:hover:text-muted-foreground disabled:hover:bg-transparent rounded transition-colors"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Clear
              </button>
            </div>
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            Stored in your browser's local storage.
          </p>
        </section>
      </div>

      <ConfirmDialog
        isOpen={confirmDialog?.type === 'chat'}
        title="Clear chat sessions"
        message={`This will delete ${chatSessions.length} chat session${chatSessions.length === 1 ? '' : 's'} and ${chatCount} message${chatCount === 1 ? '' : 's'}. This cannot be undone.`}
        confirmLabel="Clear"
        onConfirm={clearChatSessions}
        onCancel={() => setConfirmDialog(null)}
      />

      <ConfirmDialog
        isOpen={confirmDialog?.type === 'query'}
        title="Clear query sessions"
        message={`This will delete ${querySessions.length} query session${querySessions.length === 1 ? '' : 's'} and ${queryCount} quer${queryCount === 1 ? 'y' : 'ies'}. This cannot be undone.`}
        confirmLabel="Clear"
        onConfirm={clearQuerySessions}
        onCancel={() => setConfirmDialog(null)}
      />
    </div>
  )
}
