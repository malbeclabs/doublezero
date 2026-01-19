import { useEffect, useState, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { useTopology } from './TopologyContext'

interface TopologyPanelProps {
  children: ReactNode
  title?: string
  subtitle?: ReactNode
}

export function TopologyPanel({ children, title, subtitle }: TopologyPanelProps) {
  const { panel, closePanel, setPanelWidth } = useTopology()
  const [isResizing, setIsResizing] = useState(false)

  // Handle resize drag
  useEffect(() => {
    if (!isResizing) return

    const handleMouseMove = (e: MouseEvent) => {
      // Calculate width from right edge of screen
      const newWidth = window.innerWidth - e.clientX
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
  }, [isResizing, setPanelWidth])

  // Don't render if panel is closed
  if (!panel.isOpen) return null

  return (
    <div
      className="absolute top-0 bottom-0 right-0 z-[1000] bg-[var(--card)] border-l border-[var(--border)] shadow-xl flex flex-col"
      style={{ width: panel.width }}
    >
      {/* Resize handle on the left edge */}
      <div
        className="absolute top-0 bottom-0 left-0 w-1 cursor-ew-resize hover:bg-blue-500/50 transition-colors"
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

      {/* Content */}
      <div className="flex-1 overflow-y-auto">
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
