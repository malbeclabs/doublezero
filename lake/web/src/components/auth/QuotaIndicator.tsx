import { useAuth } from '../../contexts/AuthContext'
import { MessageSquare, Infinity } from 'lucide-react'

interface QuotaIndicatorProps {
  compact?: boolean
}

export function QuotaIndicator({ compact = false }: QuotaIndicatorProps) {
  const { quota, isAuthenticated, user } = useAuth()

  if (!quota) return null

  const isUnlimited = quota.remaining === null
  const remaining = quota.remaining ?? 0
  const limit = quota.limit ?? 0
  const percentage = isUnlimited ? 100 : limit > 0 ? (remaining / limit) * 100 : 0

  // Determine color based on remaining quota
  let colorClass = 'text-green-600 dark:text-green-400'
  let bgColorClass = 'bg-green-500'
  if (!isUnlimited) {
    if (remaining === 0) {
      colorClass = 'text-red-600 dark:text-red-400'
      bgColorClass = 'bg-red-500'
    } else if (remaining <= limit * 0.2) {
      colorClass = 'text-yellow-600 dark:text-yellow-400'
      bgColorClass = 'bg-yellow-500'
    }
  }

  if (compact) {
    return (
      <div className={`flex items-center gap-1.5 text-xs ${colorClass}`}>
        <MessageSquare size={12} />
        {isUnlimited ? (
          <Infinity size={14} />
        ) : (
          <span>{remaining}</span>
        )}
      </div>
    )
  }

  return (
    <div className="rounded-md bg-muted/50 px-3 py-2">
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">Questions today</span>
        <span className={colorClass}>
          {isUnlimited ? (
            <span className="flex items-center gap-1">
              Unlimited <Infinity size={14} />
            </span>
          ) : (
            `${remaining} / ${limit}`
          )}
        </span>
      </div>

      {!isUnlimited && (
        <div className="mt-1.5">
          <div className="h-1.5 w-full rounded-full bg-border">
            <div
              className={`h-1.5 rounded-full transition-all ${bgColorClass}`}
              style={{ width: `${percentage}%` }}
            />
          </div>
        </div>
      )}

      {!isAuthenticated && remaining <= 2 && (
        <p className="mt-2 text-xs text-muted-foreground">
          Sign in for more questions
        </p>
      )}

      {user?.account_type === 'wallet' && remaining <= 10 && (
        <p className="mt-2 text-xs text-muted-foreground">
          Sign in with Google for unlimited access
        </p>
      )}
    </div>
  )
}
