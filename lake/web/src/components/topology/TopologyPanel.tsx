import { useEffect, useState, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { useTopology } from './TopologyContext'

// Sidebar widths (must match sidebar.tsx)
const SIDEBAR_COLLAPSED_WIDTH = 48 // w-12
const SIDEBAR_EXPANDED_WIDTH = 256 // w-64

interface TopologyPanelProps {
  children: ReactNode
  title?: string
  subtitle?: ReactNode
}

export function TopologyPanel({ children, title, subtitle }: TopologyPanelProps) {
  const { panel, closePanel, setPanelWidth } = useTopology()
  const [isResizing, setIsResizing] = useState(false)

  // Track sidebar collapsed state
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    if (typeof window === 'undefined') return true
    return localStorage.getItem('sidebar-collapsed') !== 'false'
  })

  // Listen for sidebar state changes
  useEffect(() => {
    const handleStorage = (e: StorageEvent) => {
      if (e.key === 'sidebar-collapsed') {
        setSidebarCollapsed(e.newValue !== 'false')
      }
    }

    // Also poll for changes since storage events don't fire in same window
    const interval = setInterval(() => {
      const collapsed = localStorage.getItem('sidebar-collapsed') !== 'false'
      setSidebarCollapsed(collapsed)
    }, 100)

    window.addEventListener('storage', handleStorage)
    return () => {
      window.removeEventListener('storage', handleStorage)
      clearInterval(interval)
    }
  }, [])

  const sidebarWidth = sidebarCollapsed ? SIDEBAR_COLLAPSED_WIDTH : SIDEBAR_EXPANDED_WIDTH

  // Handle resize drag
  useEffect(() => {
    if (!isResizing) return

    const handleMouseMove = (e: MouseEvent) => {
      // Calculate width from left edge of panel (after sidebar)
      const newWidth = e.clientX - sidebarWidth
      setPanelWidth(newWidth)
    }

    const handleMouseUp = () => {
      setIsResizing(false)
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    document.body.style.cursor = 'ew-resize'
    document.body.style.userSelect = 'none'

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
  }, [isResizing, setPanelWidth, sidebarWidth])

  // Don't render if panel is closed
  if (!panel.isOpen) return null

  return (
    <div
      className="absolute top-0 bottom-0 z-[1000] bg-[var(--card)] border-r border-[var(--border)] shadow-xl flex flex-col"
      style={{ width: panel.width, left: sidebarWidth }}
    >
      {/* Resize handle on the right edge */}
      <div
        className="absolute top-0 bottom-0 right-0 w-1 cursor-ew-resize hover:bg-blue-500/50 transition-colors"
        onMouseDown={() => setIsResizing(true)}
      />

      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-[var(--border)] min-w-0">
        <div className="min-w-0 flex-1 mr-2">
          {title && (
            <div className="text-sm font-medium truncate">
              {title}
            </div>
          )}
          {subtitle && (
            <div className="text-xs text-muted-foreground mt-0.5">
              {subtitle}
            </div>
          )}
        </div>
        <button
          onClick={closePanel}
          className="p-1.5 hover:bg-[var(--muted)] rounded transition-colors"
          title="Close panel (Esc)"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Content - add borders between multiple children */}
      <div className="flex-1 overflow-y-auto [&>*+*]:border-t [&>*+*]:border-[var(--border)]">
        {children}
      </div>
    </div>
  )
}

// Empty panel content placeholder for modes that haven't been extracted yet
export function EmptyPanelContent({ modeName }: { modeName: string }) {
  return (
    <div className="p-4 text-sm text-muted-foreground">
      {modeName} panel content will be extracted here.
    </div>
  )
}
