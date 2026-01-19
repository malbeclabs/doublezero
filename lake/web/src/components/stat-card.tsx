import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

interface StatCardProps {
  label: string
  value: number | undefined
  format: 'number' | 'stake' | 'bandwidth' | 'percent'
  delta?: number // Optional delta value to show change (percentage points for percent format)
  href?: string // Optional link to entity listing page
}

function useAnimatedNumber(target: number | undefined, duration = 500) {
  const [current, setCurrent] = useState<number | undefined>(undefined)
  const prevRef = useRef<number | undefined>(undefined)

  useEffect(() => {
    if (target === undefined) return

    const start = prevRef.current ?? target
    const startTime = performance.now()

    const animate = (time: number) => {
      const elapsed = time - startTime
      const progress = Math.min(elapsed / duration, 1)
      // Ease-out cubic
      const eased = 1 - Math.pow(1 - progress, 3)
      const value = start + (target - start) * eased
      setCurrent(value)

      if (progress < 1) {
        requestAnimationFrame(animate)
      } else {
        prevRef.current = target
      }
    }

    requestAnimationFrame(animate)
  }, [target, duration])

  return current
}

function formatValue(
  value: number | undefined,
  format: 'number' | 'stake' | 'bandwidth' | 'percent'
): string {
  if (value === undefined) return '—'

  switch (format) {
    case 'stake': {
      // Convert to millions of SOL
      const millions = value / 1_000_000
      if (millions >= 1) {
        return `${millions.toFixed(1)}M`
      }
      // Less than 1M, show in K
      const thousands = value / 1_000
      return `${thousands.toFixed(0)}K`
    }
    case 'bandwidth': {
      // Convert bps to Mbps, Gbps, or Tbps
      const gbps = value / 1_000_000_000
      if (gbps >= 1000) {
        return `${(gbps / 1000).toFixed(1)} Tbps`
      }
      if (gbps >= 1) {
        return `${gbps.toFixed(0)} Gbps`
      }
      // Less than 1 Gbps, show in Mbps
      const mbps = value / 1_000_000
      return `${mbps.toFixed(0)} Mbps`
    }
    case 'percent':
      return `${value.toFixed(1)}%`
    case 'number':
    default:
      return value.toLocaleString('en-US', { maximumFractionDigits: 0 })
  }
}

function formatDelta(delta: number): string {
  const sign = delta >= 0 ? '+' : ''
  return `${sign}${delta.toFixed(2)}%`
}

export function StatCard({ label, value, format, delta, href }: StatCardProps) {
  const animatedValue = useAnimatedNumber(value)
  const isLoading = value === undefined
  const [showSkeleton, setShowSkeleton] = useState(false)

  // Only show skeleton after a delay - avoids flash for fast loads
  useEffect(() => {
    if (isLoading) {
      const timer = setTimeout(() => setShowSkeleton(true), 150)
      return () => clearTimeout(timer)
    } else {
      setShowSkeleton(false)
    }
  }, [isLoading])

  const showDelta = delta !== undefined && delta !== 0

  const content = (
    <>
      <div className="text-4xl font-medium tabular-nums tracking-tight mb-1">
        {isLoading ? (
          showSkeleton ? (
            <span className="inline-block h-10 w-16 rounded bg-muted animate-pulse" />
          ) : (
            <span className="inline-block h-10 w-16" /> // Invisible placeholder
          )
        ) : (
          <span className="inline-flex items-baseline gap-2">
            {formatValue(animatedValue, format)}
            {showDelta && (
              <span className={`text-sm font-normal ${delta > 0 ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                {formatDelta(delta)}
              </span>
            )}
          </span>
        )}
      </div>
      <div className="text-sm text-muted-foreground">{label}</div>
    </>
  )

  if (href) {
    return (
      <Link to={href} className="text-center block transition-opacity hover:opacity-70">
        {content}
      </Link>
    )
  }

  return <div className="text-center">{content}</div>
}
