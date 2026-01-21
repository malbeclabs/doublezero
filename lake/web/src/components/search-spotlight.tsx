import { useState, useRef, useEffect, useCallback } from 'react'
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom'
import { Search, X, Clock, Server, Link2, MapPin, Building2, Users, Landmark, Radio, Loader2, MessageSquare, Filter } from 'lucide-react'
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

// Map search entity types to topology URL type param
const topologyTypeMap: Record<SearchEntityType, string | null> = {
  device: 'device',
  link: 'link',
  metro: 'metro',
  validator: 'validator',
  contributor: null, // Not on topology
  user: null, // Not on topology
  gossip: null, // Not on topology (validators are via vote accounts)
}

interface SearchSpotlightProps {
  isOpen: boolean
  onClose: () => void
}

export function SearchSpotlight({ isOpen, onClose }: SearchSpotlightProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)
  const { recentSearches, addRecentSearch, clearRecentSearches } = useRecentSearches()

  const isTopologyPage = location.pathname === '/topology/map' || location.pathname === '/topology/graph'
  const isTimelinePage = location.pathname === '/timeline'
  const isStatusPage = location.pathname.startsWith('/status')
  const isOutagesPage = location.pathname === '/outages'
  const isPerformancePage = location.pathname.startsWith('/performance')

  // Helper to add a filter to the timeline search (accumulating)
  const addTimelineFilter = useCallback((filterValue: string) => {
    const currentSearch = searchParams.get('search') || ''
    const currentFilters = currentSearch ? currentSearch.split(',').map(f => f.trim()).filter(Boolean) : []
    if (!currentFilters.includes(filterValue)) {
      currentFilters.push(filterValue)
    }
    setSearchParams({ search: currentFilters.join(',') })
  }, [searchParams, setSearchParams])

  // Helper to add a filter to the status page (accumulating)
  const addStatusFilter = useCallback((entityType: SearchEntityType, value: string) => {
    const currentFilter = searchParams.get('filter') || ''
    const filters = currentFilter ? currentFilter.split(',').map(f => f.trim()).filter(Boolean) : []
    const newFilter = `${entityType}:${value}`
    if (!filters.includes(newFilter)) {
      filters.push(newFilter)
    }
    setSearchParams(prev => {
      prev.set('filter', filters.join(','))
      return prev
    })
  }, [searchParams, setSearchParams])

  // Helper to add a metro filter to the performance page (accumulating)
  const addPerformanceFilter = useCallback((metroCode: string) => {
    const currentFilter = searchParams.get('metros') || ''
    const filters = currentFilter ? currentFilter.split(',').map(f => f.trim()).filter(Boolean) : []
    if (!filters.includes(metroCode)) {
      filters.push(metroCode)
    }
    setSearchParams(prev => {
      prev.set('metros', filters.join(','))
      // Clear route selection when filtering
      prev.delete('route')
      return prev
    })
  }, [searchParams, setSearchParams])

  // Focus input when opened
  useEffect(() => {
    if (isOpen) {
      setQuery('')
      setSelectedIndex(-1)
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [isOpen])

  // Debounce query
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedQuery(query)
    }, DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [query])

  const { data, isLoading } = useSearchAutocomplete(debouncedQuery, isOpen && query.length >= 2)

  // Determine what to show
  const showRecentSearches = query.length === 0 && recentSearches.length > 0
  // Filter suggestions to only metros when on performance page
  const suggestions = isPerformancePage
    ? (data?.suggestions || []).filter(s => s.type === 'metro')
    : (data?.suggestions || [])

  // Check if query matches any field prefix (skip on performance page - only metros)
  const matchingPrefixes = query.length > 0 && !query.includes(':') && !isPerformancePage
    ? fieldPrefixes.filter(p => p.prefix.toLowerCase().startsWith(query.toLowerCase()))
    : []

  // Filter recent searches to only metros on performance page
  const filteredRecentSearches = isPerformancePage
    ? recentSearches.filter(s => s.type === 'metro')
    : recentSearches

  // Build items list
  const items: (SearchSuggestion | { type: 'prefix'; prefix: string; description: string } | { type: 'recent'; item: SearchSuggestion } | { type: 'ask-ai' } | { type: 'filter-timeline' })[] = []

  // Add "Filter timeline" option at the top when on timeline page with a query
  if (isTimelinePage && query.length >= 1) {
    items.push({ type: 'filter-timeline' as const })
  }

  if (matchingPrefixes.length > 0) {
    items.push(...matchingPrefixes.map(p => ({ type: 'prefix' as const, prefix: p.prefix, description: p.description })))
  }

  if (showRecentSearches) {
    items.push(...filteredRecentSearches.map(item => ({ type: 'recent' as const, item })))
  }

  if (!showRecentSearches) {
    items.push(...suggestions)
  }

  // Add "Ask AI" option when there's a query (not on performance page)
  if (query.length >= 2 && !isPerformancePage) {
    items.push({ type: 'ask-ai' as const })
  }

  // Reset selection when items change
  useEffect(() => {
    setSelectedIndex(-1)
  }, [debouncedQuery, matchingPrefixes.length, showRecentSearches])

  const handleSelect = useCallback((item: SearchSuggestion, e?: React.MouseEvent) => {
    addRecentSearch(item)
    setQuery('')
    onClose()

    // Determine where to navigate
    if (isTopologyPage) {
      const topologyType = topologyTypeMap[item.type]
      if (topologyType) {
        // Stay on current topology view (map or graph) with params to select item
        const url = `${location.pathname}?type=${topologyType}&id=${encodeURIComponent(item.id)}`
        if (e && (e.metaKey || e.ctrlKey)) {
          window.open(url, '_blank')
        } else {
          navigate(url)
        }
        return
      }
    }

    // On timeline page, add filter to accumulated filters instead of navigating away
    if (isTimelinePage) {
      if (e && (e.metaKey || e.ctrlKey)) {
        // Open new tab with just this filter
        window.open(`/timeline?search=${encodeURIComponent(item.label)}`, '_blank')
      } else {
        addTimelineFilter(item.label)
      }
      return
    }

    // On status page, add filter to accumulated filters instead of navigating away
    if (isStatusPage) {
      if (e && (e.metaKey || e.ctrlKey)) {
        // Open new tab with just this filter
        window.open(`${location.pathname}?filter=${encodeURIComponent(`${item.type}:${item.label}`)}`, '_blank')
      } else {
        addStatusFilter(item.type, item.label)
      }
      return
    }

    // On outages page, add filter to accumulated filters instead of navigating away
    if (isOutagesPage) {
      if (e && (e.metaKey || e.ctrlKey)) {
        // Open new tab with just this filter
        window.open(`/outages?filter=${encodeURIComponent(`${item.type}:${item.label}`)}`, '_blank')
      } else {
        addStatusFilter(item.type, item.label)
      }
      return
    }

    // On performance page, add metro filter (only metros are shown)
    if (isPerformancePage && item.type === 'metro') {
      if (e && (e.metaKey || e.ctrlKey)) {
        // Open new tab with just this filter
        window.open(`${location.pathname}?metros=${encodeURIComponent(item.label)}`, '_blank')
      } else {
        addPerformanceFilter(item.label)
      }
      return
    }

    // Default: navigate to entity detail page
    if (e) {
      handleRowClick(e, item.url, navigate)
    } else {
      navigate(item.url)
    }
  }, [navigate, addRecentSearch, onClose, isTopologyPage, isTimelinePage, addTimelineFilter, isStatusPage, isOutagesPage, addStatusFilter, isPerformancePage, addPerformanceFilter, location.pathname])

  const handleAskAI = useCallback((e?: React.MouseEvent) => {
    if (!query.trim()) return
    const q = query.trim()
    setQuery('')
    onClose()
    if (e && (e.metaKey || e.ctrlKey)) {
      window.open(`/chat?q=${encodeURIComponent(q)}`, '_blank')
    } else {
      // Dispatch event to create new chat session with question (handled by App.tsx)
      window.dispatchEvent(new CustomEvent('new-chat-session', { detail: { question: q } }))
    }
  }, [query, onClose])

  const handleFilterTimeline = useCallback((e?: React.MouseEvent) => {
    if (!query.trim()) return
    setQuery('')
    onClose()
    if (e && (e.metaKey || e.ctrlKey)) {
      window.open(`/timeline?search=${encodeURIComponent(query.trim())}`, '_blank')
    } else {
      addTimelineFilter(query.trim())
    }
  }, [query, onClose, addTimelineFilter])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
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
          } else if (item.type === 'ask-ai') {
            handleAskAI()
          } else if (item.type === 'filter-timeline') {
            handleFilterTimeline()
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
        onClose()
        break
    }
  }, [items, selectedIndex, handleSelect, handleAskAI, onClose])

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Spotlight modal */}
      <div className="relative w-full max-w-xl mx-4 bg-card border border-border rounded-lg shadow-2xl overflow-hidden">
        {/* Search input */}
        <div className="flex items-center border-b border-border px-4">
          <Search className="h-5 w-5 text-muted-foreground flex-shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={isTopologyPage ? "Search entities (opens in map)..." : isTimelinePage ? "Filter timeline events..." : isStatusPage ? "Filter status by entity..." : isOutagesPage ? "Filter outages by entity..." : isPerformancePage ? "Filter by metro..." : "Search entities..."}
            className="flex-1 h-14 px-3 text-lg bg-transparent border-0 focus:outline-none placeholder:text-muted-foreground"
          />
          {isLoading && query.length >= 2 && (
            <Loader2 className="h-5 w-5 text-muted-foreground animate-spin mr-2" />
          )}
          {query && (
            <button
              onClick={() => {
                setQuery('')
                inputRef.current?.focus()
              }}
              className="p-1 text-muted-foreground hover:text-foreground"
            >
              <X className="h-5 w-5" />
            </button>
          )}
        </div>

        {/* Results */}
        <div className="max-h-80 overflow-y-auto">
          {showRecentSearches && (
            <div className="px-4 py-2 text-xs text-muted-foreground border-b border-border flex items-center justify-between">
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

          {items.length === 0 && query.length >= 2 && !isLoading && (
            <div className="px-4 py-8 text-sm text-muted-foreground text-center">
              No results found
            </div>
          )}

          {items.length === 0 && query.length < 2 && !showRecentSearches && (
            <div className="px-4 py-8 text-sm text-muted-foreground text-center">
              Type to search entities...
            </div>
          )}

          {items.map((item, index) => {
            if (item.type === 'filter-timeline') {
              return (
                <button
                  key="filter-timeline"
                  onClick={(e) => handleFilterTimeline(e)}
                  className={cn(
                    'w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <Filter className="h-5 w-5 text-muted-foreground flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">Filter timeline by "{query}"</span>
                    </div>
                    <div className="text-sm text-muted-foreground">Show events matching this search</div>
                  </div>
                </button>
              )
            }

            if ('prefix' in item && item.type === 'prefix') {
              return (
                <button
                  key={item.prefix}
                  onClick={() => {
                    setQuery(item.prefix)
                    inputRef.current?.focus()
                  }}
                  className={cn(
                    'w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <Search className="h-4 w-4 text-muted-foreground" />
                  <code className="text-sm bg-muted px-2 py-0.5 rounded font-mono">{item.prefix}</code>
                  <span className="text-sm text-muted-foreground">{item.description}</span>
                </button>
              )
            }

            if (item.type === 'ask-ai') {
              return (
                <button
                  key="ask-ai"
                  onClick={(e) => handleAskAI(e)}
                  className={cn(
                    'w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <MessageSquare className="h-5 w-5 text-muted-foreground flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">Ask AI about "{query}"</span>
                    </div>
                    <div className="text-sm text-muted-foreground">Get an answer from the AI assistant</div>
                  </div>
                </button>
              )
            }

            const suggestion = 'item' in item && item.type === 'recent' ? item.item : item as SearchSuggestion
            const Icon = entityIcons[suggestion.type]
            const canShowOnMap = isTopologyPage && topologyTypeMap[suggestion.type] !== null

            return (
              <button
                key={`${suggestion.type}-${suggestion.id}`}
                onClick={(e) => handleSelect(suggestion, e as unknown as React.MouseEvent)}
                className={cn(
                  'w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors',
                  index === selectedIndex && 'bg-muted'
                )}
              >
                <Icon className="h-5 w-5 text-muted-foreground flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium truncate">{suggestion.label}</span>
                    <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground flex-shrink-0">
                      {entityLabels[suggestion.type]}
                    </span>
                  </div>
                  {suggestion.sublabel && (
                    <div className="text-sm text-muted-foreground truncate">{suggestion.sublabel}</div>
                  )}
                </div>
                {canShowOnMap && (
                  <span className="text-xs text-muted-foreground flex-shrink-0">
                    Show on map
                  </span>
                )}
                {isTimelinePage && (
                  <span className="text-xs text-muted-foreground flex-shrink-0">
                    Filter timeline
                  </span>
                )}
                {isStatusPage && (
                  <span className="text-xs text-muted-foreground flex-shrink-0">
                    Filter status
                  </span>
                )}
                {isOutagesPage && (
                  <span className="text-xs text-muted-foreground flex-shrink-0">
                    Filter outages
                  </span>
                )}
                {isPerformancePage && (
                  <span className="text-xs text-muted-foreground flex-shrink-0">
                    Filter by metro
                  </span>
                )}
              </button>
            )
          })}
        </div>

        {/* Footer hint */}
        <div className="px-4 py-2 border-t border-border text-xs text-muted-foreground flex items-center justify-between">
          <div className="flex items-center gap-4">
            <span><kbd className="px-1.5 py-0.5 rounded bg-muted font-mono text-xs">↑↓</kbd> Navigate</span>
            <span><kbd className="px-1.5 py-0.5 rounded bg-muted font-mono text-xs">↵</kbd> Select</span>
            <span><kbd className="px-1.5 py-0.5 rounded bg-muted font-mono text-xs">esc</kbd> Close</span>
          </div>
          {isTopologyPage && (
            <span className="text-blue-500">On topology map</span>
          )}
          {isTimelinePage && (
            <span className="text-blue-500">On timeline</span>
          )}
          {isStatusPage && (
            <span className="text-blue-500">On status</span>
          )}
          {isOutagesPage && (
            <span className="text-blue-500">On outages</span>
          )}
          {isPerformancePage && (
            <span className="text-blue-500">On performance</span>
          )}
        </div>
      </div>
    </div>
  )
}
