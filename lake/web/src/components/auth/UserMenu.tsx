import { useState, useEffect } from 'react'
import { useAuth } from '../../contexts/AuthContext'
import { LoginModal } from './LoginModal'
import { User, LogOut, LogIn, ChevronDown, Wallet } from 'lucide-react'

export function UserMenu() {
  const { user, isAuthenticated, logout, isLoading } = useAuth()
  const [showLoginModal, setShowLoginModal] = useState(false)
  const [showDropdown, setShowDropdown] = useState(false)

  // Reset modal/dropdown state when auth state changes (e.g., on logout)
  useEffect(() => {
    setShowLoginModal(false)
    setShowDropdown(false)
  }, [isAuthenticated])

  if (isLoading) {
    return (
      <div className="rounded-md bg-muted/50 px-3 py-2">
        <div className="h-4 w-24 animate-pulse rounded bg-muted" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return (
      <>
        <button
          onClick={() => setShowLoginModal(true)}
          className="flex w-full items-center justify-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm text-foreground transition-colors hover:bg-muted"
        >
          <LogIn size={16} />
          Sign in
        </button>
        <LoginModal
          isOpen={showLoginModal}
          onClose={() => setShowLoginModal(false)}
        />
      </>
    )
  }

  const displayName = user?.display_name || user?.email || truncateWallet(user?.wallet_address)

  return (
    <div className="relative">
      <button
        onClick={() => setShowDropdown(!showDropdown)}
        className="flex w-full items-center justify-between gap-2 rounded-md bg-muted/50 px-3 py-2 text-sm text-foreground transition-colors hover:bg-muted"
      >
        <div className="flex items-center gap-2 overflow-hidden">
          {user?.account_type === 'wallet' ? (
            <Wallet size={16} className="shrink-0 text-muted-foreground" />
          ) : (
            <User size={16} className="shrink-0 text-muted-foreground" />
          )}
          <span className="truncate">{displayName}</span>
        </div>
        <ChevronDown
          size={16}
          className={`shrink-0 text-muted-foreground transition-transform ${showDropdown ? 'rotate-180' : ''}`}
        />
      </button>

      {showDropdown && (
        <>
          {/* Backdrop to close dropdown */}
          <div
            className="fixed inset-0 z-10"
            onClick={() => setShowDropdown(false)}
          />
          {/* Dropdown menu */}
          <div className="absolute bottom-full left-0 z-20 mb-1 w-full rounded-md border border-border bg-card py-1 shadow-lg">
            <div className="border-b border-border px-3 py-2">
              <p className="text-xs text-muted-foreground">
                {user?.account_type === 'domain' ? 'Domain account' : 'Wallet account'}
              </p>
              <p className="truncate text-sm text-foreground">
                {user?.email || user?.wallet_address}
              </p>
            </div>
            <button
              onClick={() => {
                setShowDropdown(false)
                logout()
              }}
              className="flex w-full items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <LogOut size={16} />
              Sign out
            </button>
          </div>
        </>
      )}
    </div>
  )
}

function truncateWallet(address?: string): string {
  if (!address) return 'Unknown'
  if (address.length <= 12) return address
  return `${address.slice(0, 6)}...${address.slice(-4)}`
}
