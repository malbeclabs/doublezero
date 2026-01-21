import { useState, useEffect } from 'react'
import { useWallet } from '@solana/wallet-adapter-react'
import { WalletMultiButton } from '@solana/wallet-adapter-react-ui'
import { X, Wallet, AlertCircle } from 'lucide-react'
import { useAuth } from '../../contexts/AuthContext'

interface LoginModalProps {
  isOpen: boolean
  onClose: () => void
}

export function LoginModal({ isOpen, onClose }: LoginModalProps) {
  const { loginWithGoogle, loginWithWallet, error, isLoading, isAuthenticated } = useAuth()
  const wallet = useWallet()
  const [showWalletConnect, setShowWalletConnect] = useState(false)

  // Close modal when authenticated
  useEffect(() => {
    if (isAuthenticated) {
      onClose()
    }
  }, [isAuthenticated, onClose])

  // Auto-authenticate when wallet connects
  useEffect(() => {
    if (wallet.connected && wallet.publicKey && showWalletConnect) {
      loginWithWallet()
      setShowWalletConnect(false)
    }
  }, [wallet.connected, wallet.publicKey, showWalletConnect, loginWithWallet])

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
    <div className={`fixed inset-0 z-50 flex items-center justify-center ${isOpen ? '' : 'hidden'}`}>
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Modal */}
      <div
        className="relative z-10 w-full max-w-md rounded-lg border border-neutral-700 bg-neutral-900 p-6 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
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
          {/* Google Sign-In */}
          <button
            onClick={loginWithGoogle}
            disabled={isLoading}
            className="flex w-full items-center justify-center gap-3 rounded-md border border-neutral-600 bg-white px-4 py-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 disabled:opacity-50"
          >
            <svg className="h-5 w-5" viewBox="0 0 24 24">
              <path
                fill="#4285F4"
                d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
              />
              <path
                fill="#34A853"
                d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
              />
              <path
                fill="#FBBC05"
                d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
              />
              <path
                fill="#EA4335"
                d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
              />
            </svg>
            Sign in with Google
          </button>

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
