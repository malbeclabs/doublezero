import { AlertCircle, RefreshCw, WifiOff } from 'lucide-react'

export interface ErrorStateProps {
  title?: string
  message: string
  onRetry?: () => void
  retrying?: boolean
}

// Check if error message indicates a connectivity issue
export function isConnectivityError(message: string): boolean {
  const lowerMessage = message.toLowerCase()
  return (
    lowerMessage.includes('database temporarily unavailable') ||
    lowerMessage.includes('connection') ||
    lowerMessage.includes('network') ||
    lowerMessage.includes('timeout') ||
    lowerMessage.includes('fetch') ||
    lowerMessage.includes('unavailable') ||
    lowerMessage.includes('failed to load')
  )
}

// User-friendly error messages based on error type
export function getUserFriendlyMessage(error: string): string {
  // Already user-friendly messages from our API
  if (
    error.includes('Database temporarily unavailable') ||
    error.includes('Request timed out') ||
    error.includes('Please try again')
  ) {
    return error
  }

  const lowerError = error.toLowerCase()

  // Network/fetch errors
  if (lowerError.includes('failed to fetch') || lowerError.includes('networkerror')) {
    return 'Unable to connect to the server. Please check your network connection and try again.'
  }

  // Connection errors
  if (lowerError.includes('connection refused') || lowerError.includes('connectivityerror')) {
    return 'The database is temporarily unavailable. Please try again in a moment.'
  }

  // Timeout errors
  if (lowerError.includes('timeout') || lowerError.includes('timed out')) {
    return 'The request took too long. Please try again.'
  }

  // Generic server errors
  if (lowerError.includes('500') || lowerError.includes('internal server error')) {
    return 'Something went wrong on our end. Please try again.'
  }

  // If we don't recognize the error, return a generic message
  // but only if it looks like an internal/technical error
  if (
    lowerError.includes('dial tcp') ||
    lowerError.includes('eof') ||
    lowerError.includes('broken pipe')
  ) {
    return 'A temporary error occurred. Please try again.'
  }

  return error
}

export function ErrorState({ title, message, onRetry, retrying }: ErrorStateProps) {
  const isConnectivity = isConnectivityError(message)
  const friendlyMessage = getUserFriendlyMessage(message)
  const Icon = isConnectivity ? WifiOff : AlertCircle

  return (
    <div className="flex flex-col items-center justify-center py-12 px-4 text-center">
      <div className="flex items-center justify-center w-12 h-12 rounded-full bg-destructive/10 mb-4">
        <Icon className="h-6 w-6 text-destructive" />
      </div>
      <h3 className="text-lg font-semibold text-foreground mb-2">
        {title || (isConnectivity ? 'Connection Issue' : 'Error')}
      </h3>
      <p className="text-sm text-muted-foreground max-w-md mb-6">
        {friendlyMessage}
      </p>
      {onRetry && (
        <button
          onClick={onRetry}
          disabled={retrying}
          className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          <RefreshCw className={`h-4 w-4 ${retrying ? 'animate-spin' : ''}`} />
          {retrying ? 'Retrying...' : 'Try Again'}
        </button>
      )}
    </div>
  )
}

// Inline variant for smaller spaces
export function ErrorStateInline({ message, onRetry, retrying }: Omit<ErrorStateProps, 'title'>) {
  const friendlyMessage = getUserFriendlyMessage(message)

  return (
    <div className="flex items-center gap-3 p-3 rounded-md bg-destructive/10 text-destructive">
      <AlertCircle className="h-5 w-5 flex-shrink-0" />
      <p className="text-sm flex-1">{friendlyMessage}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          disabled={retrying}
          className="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded bg-destructive text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          <RefreshCw className={`h-3 w-3 ${retrying ? 'animate-spin' : ''}`} />
          {retrying ? 'Retrying' : 'Retry'}
        </button>
      )}
    </div>
  )
}
