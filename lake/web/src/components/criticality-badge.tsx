interface CriticalityBadgeProps {
  criticality: 'critical' | 'important' | 'redundant'
}

export function CriticalityBadge({ criticality }: CriticalityBadgeProps) {
  if (criticality === 'redundant') {
    return null
  }

  const config = {
    critical: {
      label: 'SPOF',
      className: 'bg-red-500/15 text-red-600 dark:text-red-400',
    },
    important: {
      label: 'Limited',
      className: 'bg-amber-500/15 text-amber-600 dark:text-amber-400',
    },
  }

  const { label, className } = config[criticality]

  return (
    <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${className}`}>
      {label}
    </span>
  )
}
