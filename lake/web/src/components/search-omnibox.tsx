import { useState, useRef, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, X, Clock, Server, Link2, MapPin, Building2, Users, Landmark, Radio, Loader2 } from 'lucide-react'
import { cn, handleRowClick } from '@/lib/utils'
import { useSearchAutocomplete, useRecentSearches } from '@/hooks/use-search'
import type { SearchSuggestion, SearchEntityType } from '@/lib/api'

const DEBOUNCE_MS = 150

const entityIcons: Record<SearchEntityType, React.ElementType> = {
  device: Server,
  link: Link2,
  metro: MapPin,
  contributor: Building2,
  user: Users,
  validator: Landmark,
  gossip: Radio,
}

const entityLabels: Record<SearchEntityType, string> = {
  device: 'Device',
  link: 'Link',
  metro: 'Metro',
  contributor: 'Contributor',
  user: 'User',
  validator: 'Validator',
  gossip: 'Gossip Node',
}

const fieldPrefixes = [
  { prefix: 'device:', description: 'Search devices by code or IP' },
  { prefix: 'link:', description: 'Search links by code' },
  { prefix: 'metro:', description: 'Search metros by code or name' },
  { prefix: 'contributor:', description: 'Search contributors' },
  { prefix: 'user:', description: 'Search users by pubkey or IP' },
  { prefix: 'validator:', description: 'Search validators by pubkey' },
  { prefix: 'gossip:', description: 'Search gossip nodes' },
  { prefix: 'ip:', description: 'Search by IP across entities' },
  { prefix: 'pubkey:', description: 'Search by pubkey across entities' },
]

interface SearchOmniboxProps {
  className?: string
  placeholder?: string
  onClose?: () => void
  inputRef?: React.RefObject<HTMLInputElement | null>
}

export function SearchOmnibox({ className, placeholder = 'Search...', onClose, inputRef: externalInputRef }: SearchOmniboxProps) {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [isOpen, setIsOpen] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(-1)
  const internalInputRef = useRef<HTMLInputElement>(null)
  const inputRef = externalInputRef || internalInputRef
  const dropdownRef = useRef<HTMLDivElement>(null)
  const { recentSearches, addRecentSearch, clearRecentSearches } = useRecentSearches()

  // Debounce query
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedQuery(query)
    }, DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [query])

  const { data, isLoading } = useSearchAutocomplete(debouncedQuery, isOpen && query.length >= 2)

  // Determine what to show in dropdown
  const showRecentSearches = query.length === 0 && recentSearches.length > 0
  const suggestions = data?.suggestions || []

  // Check if query matches any field prefix (e.g., "lin" matches "link:")
  const matchingPrefixes = query.length > 0 && !query.includes(':')
    ? fieldPrefixes.filter(p => p.prefix.toLowerCase().startsWith(query.toLowerCase()))
    : []
  const showPrefixSuggestions = matchingPrefixes.length > 0

  // Build items list for keyboard navigation
  const items: (SearchSuggestion | { type: 'prefix'; prefix: string; description: string } | { type: 'recent'; item: SearchSuggestion })[] = []

  if (showPrefixSuggestions) {
    items.push(...matchingPrefixes.map(p => ({ type: 'prefix' as const, prefix: p.prefix, description: p.description })))
  }

  if (showRecentSearches) {
    items.push(...recentSearches.map(item => ({ type: 'recent' as const, item })))
  }

  // Add search results (show alongside prefix suggestions)
  if (!showRecentSearches) {
    items.push(...suggestions)
  }

  // Reset selection when items change
  useEffect(() => {
    setSelectedIndex(-1)
  }, [debouncedQuery, showPrefixSuggestions, showRecentSearches])

  const handleSelect = useCallback((item: SearchSuggestion, e?: React.MouseEvent) => {
    addRecentSearch(item)
    setQuery('')
    setIsOpen(false)
    if (e) {
      handleRowClick(e, item.url, navigate)
    } else {
      navigate(item.url)
    }
    onClose?.()
  }, [navigate, addRecentSearch, onClose])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (!isOpen && e.key !== 'Escape') {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        setIsOpen(true)
        return
      }
    }

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setSelectedIndex(prev => Math.min(prev + 1, items.length - 1))
        break
      case 'ArrowUp':
        e.preventDefault()
        setSelectedIndex(prev => Math.max(prev - 1, -1))
        break
      case 'Enter':
        e.preventDefault()
        if (selectedIndex >= 0 && selectedIndex < items.length) {
          const item = items[selectedIndex]
          if ('prefix' in item && item.type === 'prefix') {
            setQuery(item.prefix)
            inputRef.current?.focus()
          } else if ('item' in item && item.type === 'recent') {
            handleSelect(item.item)
          } else if ('url' in item) {
            handleSelect(item as SearchSuggestion)
          }
        }
        break
      case 'Tab':
        if (selectedIndex >= 0 && selectedIndex < items.length) {
          const item = items[selectedIndex]
          if ('prefix' in item && item.type === 'prefix') {
            e.preventDefault()
            setQuery(item.prefix)
          }
        }
        break
      case 'Escape':
        e.preventDefault()
        if (query) {
          setQuery('')
        } else {
          setIsOpen(false)
          onClose?.()
        }
        break
    }
  }, [isOpen, items, selectedIndex, query, handleSelect, inputRef, onClose])

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(e.target as Node) &&
        inputRef.current &&
        !inputRef.current.contains(e.target as Node)
      ) {
        setIsOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [inputRef])

  // Listen for global focus event (triggered by Cmd+K)
  useEffect(() => {
    const handleFocusSearch = () => {
      inputRef.current?.focus()
      setIsOpen(true)
    }
    window.addEventListener('focus-search', handleFocusSearch)
    return () => window.removeEventListener('focus-search', handleFocusSearch)
  }, [inputRef])

  return (
    <div className={cn('relative', className)}>
      <div className="relative">
        <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setIsOpen(true)
          }}
          onFocus={() => setIsOpen(true)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          className="w-full h-8 pl-8 pr-8 text-sm bg-[var(--sidebar-active)] border-0 rounded focus:outline-none focus:ring-1 focus:ring-foreground/20 placeholder:text-muted-foreground"
        />
        {query && (
          <button
            onClick={() => {
              setQuery('')
              inputRef.current?.focus()
            }}
            className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 text-muted-foreground hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
        {isLoading && query.length >= 2 && (
          <Loader2 className="absolute right-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground animate-spin" />
        )}
      </div>

      {isOpen && items.length > 0 && (
        <div
          ref={dropdownRef}
          className="absolute top-full left-0 right-0 mt-1 bg-card border border-border rounded shadow-lg z-50 max-h-80 overflow-y-auto"
        >
          {showRecentSearches && (
            <div className="px-2 py-1.5 text-xs text-muted-foreground border-b border-border flex items-center justify-between">
              <span className="flex items-center gap-1">
                <Clock className="h-3 w-3" />
                Recent
              </span>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  clearRecentSearches()
                }}
                className="text-xs text-muted-foreground hover:text-foreground"
              >
                Clear
              </button>
            </div>
          )}

          {items.map((item, index) => {
            if ('prefix' in item && item.type === 'prefix') {
              return (
                <button
                  key={item.prefix}
                  onClick={() => {
                    setQuery(item.prefix)
                    inputRef.current?.focus()
                  }}
                  className={cn(
                    'w-full flex items-center gap-2 px-2 py-2 text-sm text-left hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">{item.prefix}</code>
                  <span className="text-muted-foreground">{item.description}</span>
                </button>
              )
            }

            const suggestion = 'item' in item && item.type === 'recent' ? item.item : item as SearchSuggestion
            const Icon = entityIcons[suggestion.type]

            return (
              <button
                key={`${suggestion.type}-${suggestion.id}`}
                onClick={(e) => handleSelect(suggestion, e as unknown as React.MouseEvent)}
                className={cn(
                  'w-full flex items-center gap-2 px-2 py-2 text-sm text-left hover:bg-muted transition-colors',
                  index === selectedIndex && 'bg-muted'
                )}
              >
                <Icon className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{suggestion.label}</span>
                    <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground flex-shrink-0">
                      {entityLabels[suggestion.type]}
                    </span>
                  </div>
                  {suggestion.sublabel && (
                    <div className="text-xs text-muted-foreground truncate">{suggestion.sublabel}</div>
                  )}
                </div>
              </button>
            )
          })}

          {!showRecentSearches && suggestions.length === 0 && matchingPrefixes.length === 0 && query.length >= 2 && !isLoading && (
            <div className="px-2 py-4 text-sm text-muted-foreground text-center">
              No results found
            </div>
          )}
        </div>
      )}
    </div>
  )
}
