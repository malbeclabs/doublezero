import { RefreshCw, WifiOff } from 'lucide-react'

interface ConnectionErrorProps {
  onRetry: () => void
  isRetrying?: boolean
}

// Detect theme from localStorage or system preference (works before ThemeProvider loads)
function getResolvedTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'light'
  const stored = localStorage.getItem('theme')
  if (stored === 'light' || stored === 'dark') return stored
  // 'system' or no preference - check system
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function ConnectionError({ onRetry, isRetrying }: ConnectionErrorProps) {
  const isDark = getResolvedTheme() === 'dark'

  // Use inline styles for critical colors to ensure visibility even if CSS fails to load
  const colors = isDark
    ? {
        bg: '#0a0a0a',
        iconBg: '#27272a',
        icon: '#a1a1aa',
        title: '#fafafa',
        text: '#a1a1aa',
        buttonBg: '#fafafa',
        buttonText: '#0a0a0a',
      }
    : {
        bg: '#fafafa',
        iconBg: '#e4e4e7',
        icon: '#71717a',
        title: '#09090b',
        text: '#71717a',
        buttonBg: '#09090b',
        buttonText: '#fafafa',
      }

  return (
    <div
      className="flex min-h-screen items-center justify-center"
      style={{ backgroundColor: colors.bg }}
    >
      <div className="flex flex-col items-center gap-6 text-center">
        <div
          className="rounded-full p-4"
          style={{ backgroundColor: colors.iconBg }}
        >
          <WifiOff className="h-12 w-12" style={{ color: colors.icon }} />
        </div>

        <div className="space-y-2">
          <h1 className="text-2xl font-semibold" style={{ color: colors.title }}>
            Unable to connect
          </h1>
          <p style={{ color: colors.text }}>
            The server is currently unavailable.
            {isRetrying ? ' Retrying...' : ' Please try again.'}
          </p>
        </div>

        <button
          onClick={onRetry}
          disabled={isRetrying}
          className="flex items-center gap-2 rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
          style={{ backgroundColor: colors.buttonBg, color: colors.buttonText }}
        >
          <RefreshCw className={`h-4 w-4 ${isRetrying ? 'animate-spin' : ''}`} />
          {isRetrying ? 'Connecting...' : 'Retry'}
        </button>
      </div>
    </div>
  )
}
