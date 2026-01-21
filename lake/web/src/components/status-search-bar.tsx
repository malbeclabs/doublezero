import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Search, X, Server, Link2, MapPin, Building2 } from 'lucide-react'
import type { SearchEntityType } from '@/lib/api'

// Only show entity types relevant to status filtering
const statusEntityTypes: SearchEntityType[] = ['device', 'link', 'metro', 'contributor']

const entityIcons: Record<string, React.ElementType> = {
  device: Server,
  link: Link2,
  metro: MapPin,
  contributor: Building2,
}

export interface StatusFilter {
  type: SearchEntityType
  value: string
  label: string
}

export function parseStatusFilters(searchParam: string): StatusFilter[] {
  if (!searchParam) return []
  return searchParam.split(',').map(f => f.trim()).filter(Boolean).map(f => {
    const [type, ...rest] = f.split(':')
    const value = rest.join(':')
    if (type && value && statusEntityTypes.includes(type as SearchEntityType)) {
      return { type: type as SearchEntityType, value, label: value }
    }
    // Plain value without type prefix - treat as device code
    return { type: 'device' as SearchEntityType, value: f, label: f }
  })
}

export function serializeStatusFilters(filters: StatusFilter[]): string {
  return filters.map(f => `${f.type}:${f.value}`).join(',')
}

// Hook to get current filters for use in other components
export function useStatusFilters(): StatusFilter[] {
  const [searchParams] = useSearchParams()
  const searchParam = searchParams.get('filter') || ''
  return parseStatusFilters(searchParam)
}

// Hook to add a filter (used by search spotlight or other components)
export function useAddStatusFilter() {
  const [searchParams, setSearchParams] = useSearchParams()

  return useCallback((type: SearchEntityType, value: string, label?: string) => {
    const currentParam = searchParams.get('filter') || ''
    const filters = parseStatusFilters(currentParam)

    const newFilter: StatusFilter = { type, value, label: label || value }
    const exists = filters.some(f => f.type === newFilter.type && f.value === newFilter.value)

    if (!exists) {
      const newFilters = [...filters, newFilter]
      setSearchParams(prev => {
        prev.set('filter', serializeStatusFilters(newFilters))
        return prev
      })
    }
  }, [searchParams, setSearchParams])
}

interface StatusFiltersProps {
  className?: string
}

// Compact filter display with search button and filter tags
export function StatusFilters({ className }: StatusFiltersProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const searchParam = searchParams.get('filter') || ''
  const filters = parseStatusFilters(searchParam)

  const removeFilter = useCallback((filterToRemove: StatusFilter) => {
    const newFilters = filters.filter(f => !(f.type === filterToRemove.type && f.value === filterToRemove.value))
    setSearchParams(prev => {
      if (newFilters.length === 0) {
        prev.delete('filter')
      } else {
        prev.set('filter', serializeStatusFilters(newFilters))
      }
      return prev
    })
  }, [filters, setSearchParams])

  const clearAllFilters = useCallback(() => {
    setSearchParams(prev => {
      prev.delete('filter')
      return prev
    })
  }, [setSearchParams])

  const openSearch = useCallback(() => {
    window.dispatchEvent(new CustomEvent('open-search'))
  }, [])

  return (
    <div className={className}>
      <div className="flex items-center gap-2 flex-wrap">
        {/* Search button */}
        <button
          onClick={openSearch}
          className="flex items-center gap-1.5 px-2 py-1 text-xs text-muted-foreground hover:text-foreground border border-border rounded-md bg-background hover:bg-muted transition-colors"
          title="Search (Cmd+K)"
        >
          <Search className="h-3 w-3" />
          <span>Filter</span>
          <kbd className="ml-0.5 font-mono text-[10px] text-muted-foreground/70">⌘K</kbd>
        </button>

        {/* Filter tags */}
        {filters.map((filter, idx) => {
          const Icon = entityIcons[filter.type] || Server
          return (
            <button
              key={`${filter.type}-${filter.value}-${idx}`}
              onClick={() => removeFilter(filter)}
              className="inline-flex items-center gap-1 text-xs px-2 py-1 rounded-md bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20 hover:bg-blue-500/20 transition-colors"
            >
              <Icon className="h-3 w-3" />
              {filter.label}
              <X className="h-3 w-3" />
            </button>
          )
        })}

        {/* Clear all */}
        {filters.length > 1 && (
          <button
            onClick={clearAllFilters}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Clear all
          </button>
        )}
      </div>
    </div>
  )
}
