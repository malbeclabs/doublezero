import { Sun, Moon, Monitor, Wallet, Infinity } from 'lucide-react'
import { useTheme } from '@/hooks/use-theme'
import { useAuth } from '@/contexts/AuthContext'

const themeOptions = [
  { value: 'light' as const, label: 'Light', icon: Sun, description: 'Always use light mode' },
  { value: 'dark' as const, label: 'Dark', icon: Moon, description: 'Always use dark mode' },
  { value: 'system' as const, label: 'System', icon: Monitor, description: 'Follow your system preference' },
]

// Constants matching backend defaults
const MIN_SOL_THRESHOLD = 1.0
const WALLET_PREMIUM_LIMIT = 25
const LAMPORTS_PER_SOL = 1_000_000_000

function formatSOL(lamports: number): string {
  return (lamports / LAMPORTS_PER_SOL).toFixed(2)
}

export function SettingsPage() {
  const { theme, setTheme } = useTheme()
  const { user, quota } = useAuth()

  const isWalletUser = user?.account_type === 'wallet'
  const solBalance = user?.sol_balance ?? 0
  const solBalanceSOL = solBalance / LAMPORTS_PER_SOL
  const isPremiumWallet = isWalletUser && solBalanceSOL >= MIN_SOL_THRESHOLD
  const isUnlimited = quota?.limit === null

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-2xl mx-auto px-6 py-8">
        <h1 className="text-2xl font-semibold text-foreground mb-8">Settings</h1>

        {/* Quota Section */}
        <section className="mb-10">
          <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide mb-4">
            Usage
          </h2>
          <div className="bg-card border border-border rounded-lg overflow-hidden">
            {/* Current Quota */}
            <div className="px-4 py-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm font-medium text-foreground">Daily Questions</div>
                  <div className="text-xs text-muted-foreground">
                    {isUnlimited ? 'Unlimited access' : `Resets at midnight UTC`}
                  </div>
                </div>
                <div className="text-right">
                  {isUnlimited ? (
                    <div className="flex items-center gap-1 text-sm font-medium text-foreground">
                      <Infinity className="h-4 w-4" />
                      <span>Unlimited</span>
                    </div>
                  ) : (
                    <div className="text-sm font-medium text-foreground">
                      {quota?.remaining ?? 0} / {quota?.limit ?? 0} remaining
                    </div>
                  )}
                </div>
              </div>

              {/* Progress bar for limited users */}
              {!isUnlimited && quota?.limit && (
                <div className="mt-2">
                  <div className="h-2 bg-muted rounded-full overflow-hidden">
                    <div
                      className="h-full bg-primary transition-all"
                      style={{ width: `${((quota.remaining ?? 0) / quota.limit) * 100}%` }}
                    />
                  </div>
                </div>
              )}
            </div>

            {/* Wallet Balance (for wallet users) */}
            {isWalletUser && (
              <div className="px-4 py-3 border-t border-border">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Wallet className="h-4 w-4 text-muted-foreground" />
                    <div>
                      <div className="text-sm font-medium text-foreground">SOL Balance</div>
                      <div className="text-xs text-muted-foreground">
                        {user?.wallet_address?.slice(0, 4)}...{user?.wallet_address?.slice(-4)}
                      </div>
                    </div>
                  </div>
                  <div className="text-sm font-medium text-foreground">
                    {formatSOL(solBalance)} SOL
                  </div>
                </div>

                {/* Upgrade hint for non-premium wallet users */}
                {!isPremiumWallet && (
                  <div className="mt-3 p-3 bg-muted/50 rounded-md">
                    <p className="text-xs text-muted-foreground">
                      Hold at least {MIN_SOL_THRESHOLD} SOL to unlock {WALLET_PREMIUM_LIMIT} questions per day.
                    </p>
                  </div>
                )}
              </div>
            )}
          </div>
        </section>

        {/* Theme Section */}
        <section className="mb-10">
          <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide mb-4">
            Appearance
          </h2>
          <div className="bg-card border border-border rounded-lg overflow-hidden">
            {themeOptions.map((option, idx) => (
              <button
                key={option.value}
                onClick={() => setTheme(option.value)}
                className={`w-full flex items-center gap-4 px-4 py-3 text-left transition-colors hover:bg-muted/50 ${
                  idx !== 0 ? 'border-t border-border' : ''
                } ${theme === option.value ? 'bg-muted/30' : ''}`}
              >
                <div className={`p-2 rounded-md ${theme === option.value ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}>
                  <option.icon className="h-4 w-4" />
                </div>
                <div className="flex-1">
                  <div className="text-sm font-medium text-foreground">{option.label}</div>
                  <div className="text-xs text-muted-foreground">{option.description}</div>
                </div>
                {theme === option.value && (
                  <div className="w-2 h-2 rounded-full bg-primary" />
                )}
              </button>
            ))}
          </div>
        </section>
      </div>
    </div>
  )
}
