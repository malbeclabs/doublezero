import { createContext, useContext, useEffect, useState, useCallback, useRef } from 'react'
import type { ReactNode } from 'react'
import { useWallet } from '@solana/wallet-adapter-react'
import type { Account, QuotaInfo } from '../lib/api'
import {
  fetchAuthMe,
  logout as apiLogout,
  getWalletNonce,
  authenticateWallet,
  authenticateGoogle,
  buildSIWSMessage,
  getAuthToken,
  clearAuthToken,
  AuthError,
} from '../lib/api'

// Retry configuration for connection errors
const CONNECTION_RETRY_INTERVAL = 3000 // 3 seconds

interface GooglePromptNotification {
  isNotDisplayed: () => boolean
  isSkippedMoment: () => boolean
  getNotDisplayedReason: () => string
  getSkippedReason: () => string
}

declare global {
  interface Window {
    google?: {
      accounts: {
        id: {
          initialize: (config: {
            client_id: string
            callback: (response: { credential: string }) => void
            auto_select?: boolean
          }) => void
          prompt: (callback?: (notification: GooglePromptNotification) => void) => void
          cancel: () => void
          renderButton: (element: HTMLElement, options: object) => void
        }
      }
    }
  }
}

interface AuthContextType {
  user: Account | null
  isLoading: boolean
  isAuthenticated: boolean
  quota: QuotaInfo | null
  loginWithGoogle: () => void
  loginWithWallet: () => Promise<void>
  logout: () => Promise<void>
  refreshAuth: () => Promise<void>
  error: string | null
  // Connection state
  connectionError: boolean
  retryConnection: () => void
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

interface AuthProviderProps {
  children: ReactNode
  googleClientId?: string
  onLoginSuccess?: () => void
  onLogoutSuccess?: () => void
}

export function AuthProvider({ children, googleClientId, onLoginSuccess, onLogoutSuccess }: AuthProviderProps) {
  const [user, setUser] = useState<Account | null>(null)
  const [quota, setQuota] = useState<QuotaInfo | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [connectionError, setConnectionError] = useState(false)
  const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const initialLoadCompleteRef = useRef(false)

  const wallet = useWallet()

  // Check if an error is a connection error (server unreachable)
  const isConnectionError = (err: unknown): boolean => {
    // 500/502/503/504 from proxy typically means backend is down
    // Note: Vite dev proxy returns 500 when target is unreachable
    if (err instanceof AuthError) {
      return err.status === 500 || err.status === 502 || err.status === 503 || err.status === 504
    }
    // TypeError with 'fetch' usually means network failure
    if (err instanceof TypeError) return true
    // Check for common network error messages
    if (err instanceof Error) {
      const msg = err.message.toLowerCase()
      return msg.includes('network') || msg.includes('fetch') || msg.includes('connection')
    }
    return false
  }

  const refreshAuth = useCallback(async () => {
    try {
      const response = await fetchAuthMe()
      setUser(response.account)
      setQuota(response.quota)
      setError(null)
      setConnectionError(false)
    } catch (err) {
      console.error('Failed to refresh auth:', err)
      // Don't clear user on refresh failure - might just be network issue
    }
  }, [])

  // Load auth state with retry on connection errors
  const loadAuth = useCallback(async (): Promise<boolean> => {
    const token = getAuthToken()
    console.log('[Auth] Loading auth state, hasToken:', !!token)
    try {
      const response = await fetchAuthMe()
      console.log('[Auth] Auth loaded successfully')
      if (token) {
        setUser(response.account)
      }
      setQuota(response.quota)
      setConnectionError(false)
      return true // success
    } catch (err) {
      console.error('[Auth] Failed to load auth:', err)
      console.log('[Auth] Error type:', err?.constructor?.name)
      if (err instanceof AuthError) {
        console.log('[Auth] AuthError status:', err.status)
      }
      console.log('[Auth] isConnectionError:', isConnectionError(err))

      if (isConnectionError(err)) {
        // Connection error - server is unreachable
        console.log('[Auth] Setting connectionError to true')
        setConnectionError(true)
        return false // will retry
      }

      // HTTP error - server is reachable but returned an error
      setConnectionError(false)
      if (token && err instanceof AuthError && err.status === 401) {
        clearAuthToken()
      }
      return true // don't retry for HTTP errors
    }
  }, [])

  // Initial load with auto-retry
  useEffect(() => {
    if (initialLoadCompleteRef.current) return

    const attemptLoad = async () => {
      const success = await loadAuth()
      if (success) {
        initialLoadCompleteRef.current = true
        setIsLoading(false)
      } else {
        // Retry after delay
        retryTimeoutRef.current = setTimeout(attemptLoad, CONNECTION_RETRY_INTERVAL)
      }
    }

    attemptLoad()

    return () => {
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current)
      }
    }
  }, [loadAuth])

  // Manual retry function
  const retryConnection = useCallback(() => {
    if (retryTimeoutRef.current) {
      clearTimeout(retryTimeoutRef.current)
    }
    setIsLoading(true)

    const attemptLoad = async () => {
      const success = await loadAuth()
      if (success) {
        initialLoadCompleteRef.current = true
        setIsLoading(false)
      } else {
        retryTimeoutRef.current = setTimeout(attemptLoad, CONNECTION_RETRY_INTERVAL)
      }
    }

    attemptLoad()
  }, [loadAuth])

  // Initialize Google Sign-In
  useEffect(() => {
    if (!googleClientId) return

    const script = document.createElement('script')
    script.src = 'https://accounts.google.com/gsi/client'
    script.async = true
    script.defer = true
    document.body.appendChild(script)

    script.onload = () => {
      if (window.google) {
        window.google.accounts.id.initialize({
          client_id: googleClientId,
          callback: handleGoogleCallback,
        })
      }
    }

    return () => {
      document.body.removeChild(script)
    }
  }, [googleClientId])

  const handleGoogleCallback = async (response: { credential: string }) => {
    setIsLoading(true)
    setError(null)
    try {
      const authResponse = await authenticateGoogle(response.credential)
      setUser(authResponse.account)
      await refreshAuth()
      onLoginSuccess?.()
    } catch (err) {
      console.error('Google auth failed:', err)
      setError(err instanceof Error ? err.message : 'Google authentication failed')
    } finally {
      setIsLoading(false)
    }
  }

  const loginWithGoogle = useCallback(() => {
    if (!window.google?.accounts?.id) {
      setError('Google Sign-In not available. Please refresh the page.')
      return
    }

    // Use notification callback to detect when prompt fails
    window.google.accounts.id.prompt((notification) => {
      if (notification.isNotDisplayed()) {
        const reason = notification.getNotDisplayedReason()
        console.log('Google One Tap not displayed:', reason)
        // Common reasons: browser_not_supported, invalid_client, opt_out_or_no_session, suppressed_by_user
        if (reason === 'opt_out_or_no_session') {
          setError('Please sign in to Google in your browser first, then try again.')
        } else if (reason === 'suppressed_by_user') {
          setError('Google sign-in was recently dismissed. Please wait a moment and try again.')
        } else {
          setError('Google Sign-In unavailable. Please try again later.')
        }
      } else if (notification.isSkippedMoment()) {
        // User closed the prompt or clicked elsewhere - not an error, just cancelled
        console.log('Google One Tap skipped:', notification.getSkippedReason())
      }
    })
  }, [])

  const loginWithWallet = useCallback(async () => {
    if (!wallet.publicKey || !wallet.signMessage) {
      setError('Please connect your wallet first')
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      // Get nonce from server
      const nonce = await getWalletNonce()

      // Build message to sign
      const message = buildSIWSMessage(nonce)
      const messageBytes = new TextEncoder().encode(message)

      // Sign the message
      const signature = await wallet.signMessage(messageBytes)
      const signatureBase64 = Buffer.from(signature).toString('base64')

      // Authenticate with backend
      const authResponse = await authenticateWallet(
        wallet.publicKey.toBase58(),
        signatureBase64,
        message
      )
      setUser(authResponse.account)
      await refreshAuth()
      onLoginSuccess?.()
    } catch (err) {
      console.error('Wallet auth failed:', err)
      setError(err instanceof Error ? err.message : 'Wallet authentication failed')
    } finally {
      setIsLoading(false)
    }
  }, [wallet.publicKey, wallet.signMessage, refreshAuth, onLoginSuccess])

  const logout = useCallback(async () => {
    setIsLoading(true)
    try {
      // Cancel any Google One Tap prompts
      if (window.google?.accounts?.id) {
        window.google.accounts.id.cancel()
      }
      await apiLogout()
      setUser(null)
      // Refresh to get anonymous quota
      await refreshAuth()
      onLogoutSuccess?.()
    } catch (err) {
      console.error('Logout failed:', err)
    } finally {
      setIsLoading(false)
    }
  }, [refreshAuth, onLogoutSuccess])

  const value: AuthContextType = {
    user,
    isLoading,
    isAuthenticated: user !== null,
    quota,
    loginWithGoogle,
    loginWithWallet,
    logout,
    refreshAuth,
    error,
    connectionError,
    retryConnection,
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
