import { useState, useEffect, useRef } from 'react'
import { useWallet } from '@solana/wallet-adapter-react'
import { WalletMultiButton } from '@solana/wallet-adapter-react-ui'
import { X, Wallet, AlertCircle } from 'lucide-react'
import { useAuth } from '../../contexts/AuthContext'

interface LoginModalProps {
  isOpen: boolean
  onClose: () => void
}

export function LoginModal({ isOpen, onClose }: LoginModalProps) {
  const { loginWithWallet, error, isLoading, isAuthenticated } = useAuth()
  const wallet = useWallet()
  const [showWalletConnect, setShowWalletConnect] = useState(false)
  const googleButtonRef = useRef<HTMLDivElement>(null)

  // Close modal when authenticated
  useEffect(() => {
    if (isAuthenticated) {
      onClose()
    }
  }, [isAuthenticated, onClose])

  // Render Google Sign-In button when modal opens
  useEffect(() => {
    if (isOpen && googleButtonRef.current && window.google?.accounts?.id) {
      // Clear any existing button
      googleButtonRef.current.innerHTML = ''
      // Render the Google button
      window.google.accounts.id.renderButton(googleButtonRef.current, {
        type: 'standard',
        theme: 'outline',
        size: 'large',
        text: 'signin_with',
        width: 400,
      })
    }
  }, [isOpen])

  // Auto-authenticate when wallet connects
  useEffect(() => {
    if (wallet.connected && wallet.publicKey && showWalletConnect) {
      loginWithWallet()
      setShowWalletConnect(false)
    }
  }, [wallet.connected, wallet.publicKey, showWalletConnect, loginWithWallet])

  if (!isOpen) return null

  const handleWalletClick = () => {
    if (wallet.connected && wallet.publicKey) {
      // Already connected, authenticate directly
      loginWithWallet()
    } else {
      // Show wallet selection
      setShowWalletConnect(true)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Modal */}
      <div className="relative z-10 w-full max-w-md rounded-lg border border-neutral-700 bg-neutral-900 p-6 shadow-xl">
        {/* Close button */}
        <button
          onClick={onClose}
          className="absolute right-4 top-4 text-neutral-400 hover:text-white"
        >
          <X size={20} />
        </button>

        {/* Header */}
        <div className="mb-6 text-center">
          <h2 className="text-xl font-semibold text-white">Sign In</h2>
          <p className="mt-2 text-sm text-neutral-400">
            Sign in to get more questions per day
          </p>
        </div>

        {/* Error message */}
        {error && (
          <div className="mb-4 flex items-center gap-2 rounded-md bg-red-500/10 p-3 text-sm text-red-400">
            <AlertCircle size={16} />
            <span>{error}</span>
          </div>
        )}

        {/* Auth options */}
        <div className="space-y-3">
          {/* Google Sign-In button - rendered by Google */}
          <div
            ref={googleButtonRef}
            className="flex justify-center [&>div]:w-full [&_iframe]:w-full"
          />

          {/* Divider */}
          <div className="relative">
            <div className="absolute inset-0 flex items-center">
              <div className="w-full border-t border-neutral-700" />
            </div>
            <div className="relative flex justify-center text-sm">
              <span className="bg-neutral-900 px-2 text-neutral-500">or</span>
            </div>
          </div>

          {/* Wallet Sign-In */}
          {showWalletConnect ? (
            <div className="flex justify-center">
              <WalletMultiButton />
            </div>
          ) : (
            <button
              onClick={handleWalletClick}
              disabled={isLoading}
              className="flex w-full items-center justify-center gap-3 rounded-md border border-neutral-600 bg-neutral-800 px-4 py-3 text-sm font-medium text-white transition-colors hover:bg-neutral-700 disabled:opacity-50"
            >
              <Wallet size={20} />
              Sign in with Wallet
            </button>
          )}
        </div>

      </div>
    </div>
  )
}
