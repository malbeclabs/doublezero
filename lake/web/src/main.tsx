import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import './index.css'
import App from './App.tsx'
import { ThemeProvider } from '@/hooks/use-theme'

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

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ThemeProvider>
  </StrictMode>,
)
