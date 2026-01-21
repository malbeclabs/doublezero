import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../../contexts/AuthContext'
import { LoginModal } from './LoginModal'
import { User, LogOut, LogIn, Wallet, Settings, MessageSquare, ChevronUp } from 'lucide-react'

interface UserPopoverProps {
  collapsed?: boolean
}

export function UserPopover({ collapsed = false }: UserPopoverProps) {
  const { user, isAuthenticated, logout, isLoading, quota } = useAuth()
  const [showLoginModal, setShowLoginModal] = useState(false)
  const [showPopover, setShowPopover] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  // Reset modal/popover state when auth state changes
  useEffect(() => {
    setShowLoginModal(false)
    setShowPopover(false)
  }, [isAuthenticated])

  // Close popover when clicking outside
  useEffect(() => {
    if (!showPopover) return

    const handleClickOutside = (e: MouseEvent) => {
      if (
        popoverRef.current &&
        !popoverRef.current.contains(e.target as Node) &&
        triggerRef.current &&
        !triggerRef.current.contains(e.target as Node)
      ) {
        setShowPopover(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [showPopover])

  if (isLoading) {
    return (
      <div className={collapsed ? 'p-2' : 'rounded-md bg-muted/50 px-3 py-2'}>
        <div className={`animate-pulse rounded bg-muted ${collapsed ? 'h-4 w-4' : 'h-4 w-24'}`} />
      </div>
    )
  }

  // Unauthenticated: simple sign in button
  if (!isAuthenticated) {
    if (collapsed) {
      return (
        <>
          <button
            onClick={() => setShowLoginModal(true)}
            className="p-2 text-muted-foreground hover:text-foreground transition-colors"
            title="Sign in"
          >
            <LogIn className="h-4 w-4" />
          </button>
          <LoginModal
            isOpen={showLoginModal}
            onClose={() => setShowLoginModal(false)}
          />
        </>
      )
    }

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

  // Authenticated: show user popover
  const displayName = user?.display_name || user?.email || truncateWallet(user?.wallet_address)

  // Quota info
  const isUnlimited = quota?.remaining === null
  const remaining = quota?.remaining ?? 0
  const limit = quota?.limit ?? 0

  // Quota color
  let quotaColorClass = 'text-green-600 dark:text-green-400'
  if (!isUnlimited) {
    if (remaining === 0) {
      quotaColorClass = 'text-red-600 dark:text-red-400'
    } else if (remaining <= limit * 0.2) {
      quotaColorClass = 'text-yellow-600 dark:text-yellow-400'
    }
  }

  // Collapsed trigger
  if (collapsed) {
    return (
      <div className="relative">
        <button
          ref={triggerRef}
          onClick={() => setShowPopover(!showPopover)}
          className="p-2 text-muted-foreground hover:text-foreground transition-colors"
          title={displayName}
        >
          {user?.account_type === 'wallet' ? (
            <Wallet className="h-4 w-4" />
          ) : (
            <User className="h-4 w-4" />
          )}
        </button>

        {showPopover && (
          <div
            ref={popoverRef}
            className="absolute left-full bottom-0 ml-2 z-50 w-64 rounded-md border border-border bg-card shadow-lg"
          >
            <PopoverContent
              user={user}
              quota={quota}
              isUnlimited={isUnlimited}
              remaining={remaining}
              limit={limit}
              quotaColorClass={quotaColorClass}
              logout={logout}
              onClose={() => setShowPopover(false)}
            />
          </div>
        )}
      </div>
    )
  }

  // Expanded trigger
  return (
    <div className="relative">
      <button
        ref={triggerRef}
        onClick={() => setShowPopover(!showPopover)}
        className="flex w-full items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm text-foreground transition-colors hover:bg-muted"
      >
        {user?.account_type === 'wallet' ? (
          <Wallet size={16} className="shrink-0 text-muted-foreground" />
        ) : (
          <User size={16} className="shrink-0 text-muted-foreground" />
        )}
        <span className="flex-1 truncate text-left">{displayName}</span>
        <ChevronUp size={14} className={`shrink-0 text-muted-foreground transition-transform ${showPopover ? '' : 'rotate-180'}`} />
      </button>

      {showPopover && (
        <div
          ref={popoverRef}
          className="absolute bottom-full left-0 mb-1 z-50 w-full rounded-md border border-border bg-card shadow-lg"
        >
          <PopoverContent
            user={user}
            quota={quota}
            isUnlimited={isUnlimited}
            remaining={remaining}
            limit={limit}
            quotaColorClass={quotaColorClass}
            logout={logout}
            onClose={() => setShowPopover(false)}
          />
        </div>
      )}
    </div>
  )
}

interface PopoverContentProps {
  user: {
    account_type?: string
    email?: string
    wallet_address?: string
    display_name?: string
  } | null
  quota: { remaining: number | null; limit: number | null } | null
  isUnlimited: boolean
  remaining: number
  limit: number
  quotaColorClass: string
  logout: () => void
  onClose: () => void
}

function PopoverContent({
  user,
  quota,
  isUnlimited,
  remaining,
  limit,
  quotaColorClass,
  logout,
  onClose,
}: PopoverContentProps) {
  return (
    <div className="py-1">
      {/* Account info */}
      <div className="px-3 py-2 border-b border-border">
        <p className="text-xs text-muted-foreground">
          {user?.account_type === 'domain' ? 'Domain account' : 'Wallet account'}
        </p>
        <p className="truncate text-sm text-foreground">
          {user?.email || user?.wallet_address}
        </p>
      </div>

      {/* Quota - clickable to go to settings */}
      {quota && (
        <Link
          to="/settings"
          onClick={onClose}
          className="block px-3 py-2 border-b border-border hover:bg-muted transition-colors"
        >
          <div className="flex items-center justify-between text-xs">
            <span className="flex items-center gap-1.5 text-muted-foreground">
              <MessageSquare size={12} />
              Questions today
            </span>
            <span className={quotaColorClass}>
              {isUnlimited ? 'Unlimited' : `${remaining} / ${limit}`}
            </span>
          </div>
        </Link>
      )}

      {/* Settings link */}
      <Link
        to="/settings"
        onClick={onClose}
        className="flex w-full items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
      >
        <Settings size={16} />
        Settings
      </Link>

      {/* Sign out */}
      <button
        onClick={() => {
          onClose()
          logout()
        }}
        className="flex w-full items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
      >
        <LogOut size={16} />
        Sign out
      </button>
    </div>
  )
}

function truncateWallet(address?: string): string {
  if (!address) return 'Unknown'
  if (address.length <= 12) return address
  return `${address.slice(0, 6)}...${address.slice(-4)}`
}
