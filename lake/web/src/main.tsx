import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import * as Sentry from '@sentry/react'
import './index.css'
import App from './App.tsx'
import { ThemeProvider } from '@/hooks/use-theme'
import { fetchConfig } from '@/lib/api'

// Handle chunk loading failures (stale tabs after deploys) by reloading the page
// Uses sessionStorage to prevent infinite reload loops
window.addEventListener('error', (event) => {
  const isChunkError = event.message?.includes('Failed to fetch dynamically imported module') ||
    event.message?.includes('Loading chunk') ||
    event.message?.includes('Loading CSS chunk')

  if (isChunkError) {
    const lastReload = sessionStorage.getItem('chunk-reload-time')
    const now = Date.now()

    // Only reload if we haven't reloaded in the last 10 seconds
    if (!lastReload || now - parseInt(lastReload, 10) > 10000) {
      sessionStorage.setItem('chunk-reload-time', now.toString())
      window.location.reload()
    }
  }
})

// Also handle unhandled promise rejections (dynamic imports throw these)
window.addEventListener('unhandledrejection', (event) => {
  const message = event.reason?.message || ''
  const isChunkError = message.includes('Failed to fetch dynamically imported module') ||
    message.includes('Loading chunk') ||
    message.includes('Loading CSS chunk')

  if (isChunkError) {
    const lastReload = sessionStorage.getItem('chunk-reload-time')
    const now = Date.now()

    if (!lastReload || now - parseInt(lastReload, 10) > 10000) {
      sessionStorage.setItem('chunk-reload-time', now.toString())
      window.location.reload()
    }
  }
})

// Initialize Sentry and mount React
async function init() {
  // Fetch config from API (contains Sentry DSN)
  try {
    const config = await fetchConfig()

    // Initialize Sentry if DSN is configured
    if (config.sentryDsn) {
      Sentry.init({
        dsn: config.sentryDsn,
        environment: config.sentryEnvironment || 'development',
        integrations: [
          Sentry.browserTracingIntegration(),
          Sentry.replayIntegration(),
        ],
        // Performance monitoring: capture 10% of transactions
        tracesSampleRate: 0.1,
        // Session replay: capture 0% of sessions normally, 100% on error
        replaysSessionSampleRate: 0,
        replaysOnErrorSampleRate: 1.0,
      })
    }
  } catch (error) {
    // Config fetch failed - continue without Sentry
    console.warn('Failed to fetch config for Sentry initialization:', error)
  }

  // Mount React app
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <ThemeProvider>
        <BrowserRouter>
          <Sentry.ErrorBoundary fallback={<ErrorFallback />}>
            <App />
          </Sentry.ErrorBoundary>
        </BrowserRouter>
      </ThemeProvider>
    </StrictMode>,
  )
}

// Fallback UI for unrecoverable errors
function ErrorFallback() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
      <div className="text-center">
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100 mb-2">
          Something went wrong
        </h1>
        <p className="text-gray-600 dark:text-gray-400 mb-4">
          An unexpected error occurred. Please try refreshing the page.
        </p>
        <button
          onClick={() => window.location.reload()}
          className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors"
        >
          Refresh Page
        </button>
      </div>
    </div>
  )
}

init()
