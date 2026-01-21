import { Sun, Moon, Monitor } from 'lucide-react'
import { useTheme } from '@/hooks/use-theme'

const themeOptions = [
  { value: 'light' as const, label: 'Light', icon: Sun, description: 'Always use light mode' },
  { value: 'dark' as const, label: 'Dark', icon: Moon, description: 'Always use dark mode' },
  { value: 'system' as const, label: 'System', icon: Monitor, description: 'Follow your system preference' },
]

export function SettingsPage() {
  const { theme, setTheme } = useTheme()

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-2xl mx-auto px-6 py-8">
        <h1 className="text-2xl font-semibold text-foreground mb-8">Settings</h1>

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
