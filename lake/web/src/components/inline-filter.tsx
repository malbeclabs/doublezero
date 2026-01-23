import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Search, Filter, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { fetchFieldValues } from '@/lib/api'

const DEBOUNCE_MS = 150
const LIVE_FILTER_DEBOUNCE_MS = 300

interface FieldPrefix {
  prefix: string
  description: string
}

interface InlineFilterProps {
  fieldPrefixes: FieldPrefix[]
  entity: string
  autocompleteFields: string[]
  placeholder?: string
  onLiveFilterChange?: (filter: string) => void
}

export function InlineFilter({
  fieldPrefixes,
  entity,
  autocompleteFields,
  placeholder = 'Filter...',
  onLiveFilterChange,
}: InlineFilterProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [isFocused, setIsFocused] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  // Debounce query for autocomplete
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedQuery(query)
    }, DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [query])

  // Live filter: notify parent of filter changes (debounced, not persisted to URL)
  // Only apply filter when there's actual content (not just a field prefix like "kind:")
  useEffect(() => {
    if (!onLiveFilterChange) return
    const timer = setTimeout(() => {
      const trimmed = query.trim()
      // Check if it's just a field prefix with no value (ends with ":" or has ":" but nothing after)
      const colonIndex = trimmed.indexOf(':')
      const isEmptyFieldPrefix = colonIndex >= 0 && trimmed.slice(colonIndex + 1).trim() === ''

      // Only apply filter if there's actual content
      onLiveFilterChange(isEmptyFieldPrefix ? '' : trimmed)
    }, LIVE_FILTER_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [query, onLiveFilterChange])

  // Parse field:value from query for autocomplete
  const fieldValueMatch = useMemo(() => {
    const colonIndex = query.indexOf(':')
    if (colonIndex <= 0) return null
    const field = query.slice(0, colonIndex).toLowerCase()
    const value = query.slice(colonIndex + 1).toLowerCase()
    if (!autocompleteFields.includes(field)) return null
    return { field, value }
  }, [query, autocompleteFields])

  // Fetch field values when a valid field prefix is detected
  const { data: fieldValuesData, isLoading: fieldValuesLoading } = useQuery({
    queryKey: ['field-values', entity, fieldValueMatch?.field],
    queryFn: () => fetchFieldValues(entity, fieldValueMatch!.field),
    enabled: fieldValueMatch !== null,
    staleTime: 60000,
  })

  // Filter field values based on what user has typed after the colon
  const filteredFieldValues = useMemo(() => {
    if (!fieldValueMatch || !fieldValuesData) return []
    const needle = fieldValueMatch.value
    if (!needle) return fieldValuesData
    return fieldValuesData.filter(v => v.toLowerCase().includes(needle))
  }, [fieldValueMatch, fieldValuesData])

  // Check if query matches any field prefix
  const matchingPrefixes = useMemo(() => {
    if (query.length === 0 || query.includes(':')) return []
    return fieldPrefixes.filter(p =>
      p.prefix.toLowerCase().startsWith(query.toLowerCase())
    )
  }, [query, fieldPrefixes])

  // Show all field prefixes when query is empty
  const showAllPrefixes = query.length === 0 && isFocused

  // Helper to commit a filter (persist to URL)
  const commitFilter = useCallback((filterValue: string) => {
    const currentSearch = searchParams.get('search') || ''
    const currentFilters = currentSearch ? currentSearch.split(',').map(f => f.trim()).filter(Boolean) : []

    // Add the committed filter if not already present
    if (!currentFilters.includes(filterValue)) {
      currentFilters.push(filterValue)
    }

    setSearchParams(prev => {
      prev.set('search', currentFilters.join(','))
      prev.delete('offset')
      return prev
    })
    setQuery('')
    // Notify parent that live filter is cleared
    onLiveFilterChange?.('')
    inputRef.current?.focus()
  }, [searchParams, setSearchParams, onLiveFilterChange])

  // Helper to clear the live filter without committing
  const clearLiveFilter = useCallback(() => {
    setQuery('')
    onLiveFilterChange?.('')
  }, [onLiveFilterChange])

  // Build items list for dropdown
  type DropdownItem =
    | { type: 'prefix'; prefix: string; description: string }
    | { type: 'field-value'; field: string; value: string }
    | { type: 'apply-filter' }

  const items: DropdownItem[] = useMemo(() => {
    const result: DropdownItem[] = []

    // Show "Apply filter" option when there's a query and we're not showing field values
    if (query.length >= 1 && filteredFieldValues.length === 0) {
      result.push({ type: 'apply-filter' })
    }

    // Show field value autocomplete when user has typed a field prefix
    if (filteredFieldValues.length > 0 && fieldValueMatch) {
      result.push(...filteredFieldValues.map(v => ({
        type: 'field-value' as const,
        field: fieldValueMatch.field,
        value: v
      })))
    }

    // Show field hints when empty
    if (showAllPrefixes) {
      result.push(...fieldPrefixes.map(p => ({
        type: 'prefix' as const,
        prefix: p.prefix,
        description: p.description
      })))
    } else if (matchingPrefixes.length > 0 && filteredFieldValues.length === 0) {
      result.push(...matchingPrefixes.map(p => ({
        type: 'prefix' as const,
        prefix: p.prefix,
        description: p.description
      })))
    }

    return result
  }, [query, filteredFieldValues, fieldValueMatch, showAllPrefixes, matchingPrefixes, fieldPrefixes])

  // Reset selection when items change
  useEffect(() => {
    setSelectedIndex(-1)
  }, [debouncedQuery, matchingPrefixes.length, showAllPrefixes])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    const isDropdownOpen = isFocused && items.length > 0

    switch (e.key) {
      case 'ArrowDown':
        if (isDropdownOpen) {
          e.preventDefault()
          setSelectedIndex(prev => Math.min(prev + 1, items.length - 1))
        }
        break
      case 'ArrowUp':
        if (isDropdownOpen) {
          e.preventDefault()
          setSelectedIndex(prev => Math.max(prev - 1, -1))
        }
        break
      case 'Enter':
        e.preventDefault()
        const indexToUse = selectedIndex >= 0 ? selectedIndex : 0
        if (indexToUse < items.length) {
          const item = items[indexToUse]
          if (item.type === 'prefix') {
            setQuery(item.prefix)
          } else if (item.type === 'field-value') {
            commitFilter(`${item.field}:${item.value}`)
          } else if (item.type === 'apply-filter' && query.trim()) {
            commitFilter(query.trim())
          }
        } else if (query.trim()) {
          commitFilter(query.trim())
        }
        break
      case 'Tab':
        if (selectedIndex >= 0 && selectedIndex < items.length) {
          const item = items[selectedIndex]
          if (item.type === 'prefix') {
            e.preventDefault()
            setQuery(item.prefix)
          }
        }
        break
      case 'Escape':
        e.preventDefault()
        clearLiveFilter()
        inputRef.current?.blur()
        break
    }
  }, [items, selectedIndex, query, commitFilter, clearLiveFilter, isFocused])

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsFocused(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  const showDropdown = isFocused && items.length > 0

  return (
    <div ref={containerRef} className="relative">
      {/* Input */}
      <div className="flex items-center gap-1.5 px-2 py-1 text-sm border border-border rounded-md bg-background hover:bg-muted/50 focus-within:ring-1 focus-within:ring-ring transition-colors">
        <Search className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => setIsFocused(true)}
          placeholder={placeholder}
          className="w-32 bg-transparent border-0 focus:outline-none placeholder:text-muted-foreground text-sm"
        />
        {fieldValuesLoading && (
          <Loader2 className="h-3.5 w-3.5 text-muted-foreground animate-spin" />
        )}
      </div>

      {/* Dropdown */}
      {showDropdown && (
        <div className="absolute top-full left-0 mt-1 w-64 max-h-64 overflow-y-auto bg-card border border-border rounded-lg shadow-lg z-40">
          {showAllPrefixes && (
            <div className="px-3 py-1.5 text-xs text-muted-foreground border-b border-border flex items-center gap-1">
              <Filter className="h-3 w-3" />
              Filter by field
            </div>
          )}

          {items.map((item, index) => {
            if (item.type === 'apply-filter') {
              return (
                <button
                  key="apply-filter"
                  onClick={() => commitFilter(query.trim())}
                  className={cn(
                    'w-full flex items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <Filter className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                  <span>Filter by "<span className="font-medium">{query}</span>"</span>
                </button>
              )
            }

            if (item.type === 'field-value') {
              return (
                <button
                  key={`field-value-${item.value}`}
                  onClick={() => commitFilter(`${item.field}:${item.value}`)}
                  className={cn(
                    'w-full flex items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <span className="flex-1 truncate">{item.value}</span>
                  <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
                    {item.field}
                  </span>
                </button>
              )
            }

            if (item.type === 'prefix') {
              return (
                <button
                  key={item.prefix}
                  onClick={() => {
                    setQuery(item.prefix)
                    inputRef.current?.focus()
                  }}
                  className={cn(
                    'w-full flex flex-col gap-0.5 px-3 py-2 text-left text-sm hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{item.prefix.slice(0, -1)}</span>
                    <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
                      filter
                    </span>
                  </div>
                  <span className="text-xs text-muted-foreground">{item.description}</span>
                </button>
              )
            }

            return null
          })}
        </div>
      )}
    </div>
  )
}
