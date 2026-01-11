import { cn } from '@/lib/utils'

interface TabsProps {
  value: string
  onValueChange: (value: string) => void
  children: React.ReactNode
}

interface TabsListProps {
  children: React.ReactNode
  className?: string
}

interface TabsTriggerProps {
  value: string
  children: React.ReactNode
  className?: string
}

interface TabsContentProps {
  value: string
  children: React.ReactNode
  className?: string
}

export function Tabs({ value, onValueChange: _onValueChange, children }: TabsProps) {
  return (
    <div data-state={value} className="flex flex-col h-full">
      {Array.isArray(children)
        ? children.map((child) => {
            if (!child) return null
            if (child.type === TabsList) {
              return child
            }
            if (child.type === TabsContent) {
              return child.props.value === value ? child : null
            }
            return child
          })
        : children}
    </div>
  )
}

export function TabsList({ children, className }: TabsListProps) {
  return (
    <div
      className={cn(
        'inline-flex h-9 items-center justify-start gap-1 rounded-lg bg-muted p-1',
        className
      )}
    >
      {children}
    </div>
  )
}

export function TabsTrigger({
  value,
  children,
  className,
}: TabsTriggerProps & { onClick?: () => void; 'data-state'?: string }) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium ring-offset-background transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow',
        className
      )}
      data-value={value}
    >
      {children}
    </button>
  )
}

export function TabsContent({ value, children, className }: TabsContentProps) {
  return (
    <div className={cn('flex-1 overflow-hidden', className)} data-value={value}>
      {children}
    </div>
  )
}
