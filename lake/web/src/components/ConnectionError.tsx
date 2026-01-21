import { RefreshCw, WifiOff } from 'lucide-react'

interface ConnectionErrorProps {
  onRetry: () => void
  isRetrying?: boolean
}

export function ConnectionError({ onRetry, isRetrying }: ConnectionErrorProps) {
  // Use inline styles for critical colors to ensure visibility even if CSS fails to load
  return (
    <div
      className="flex min-h-screen items-center justify-center"
      style={{ backgroundColor: '#0a0a0a' }}
    >
      <div className="flex flex-col items-center gap-6 text-center">
        <div
          className="rounded-full p-4"
          style={{ backgroundColor: '#27272a' }}
        >
          <WifiOff className="h-12 w-12" style={{ color: '#a1a1aa' }} />
        </div>

        <div className="space-y-2">
          <h1 className="text-2xl font-semibold" style={{ color: '#fafafa' }}>
            Unable to connect
          </h1>
          <p style={{ color: '#a1a1aa' }}>
            The server is currently unavailable.
            {isRetrying ? ' Retrying...' : ' Please try again.'}
          </p>
        </div>

        <button
          onClick={onRetry}
          disabled={isRetrying}
          className="flex items-center gap-2 rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
          style={{ backgroundColor: '#fafafa', color: '#0a0a0a' }}
        >
          <RefreshCw className={`h-4 w-4 ${isRetrying ? 'animate-spin' : ''}`} />
          {isRetrying ? 'Connecting...' : 'Retry'}
        </button>
      </div>
    </div>
  )
}
