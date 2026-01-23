import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Search, X, Clock, Server, Link2, MapPin, Building2, Users, Landmark, Radio, Loader2, MessageSquare, Filter } from 'lucide-react'
import { cn, handleRowClick } from '@/lib/utils'
import { useSearchAutocomplete, useRecentSearches } from '@/hooks/use-search'
import type { SearchSuggestion, SearchEntityType } from '@/lib/api'
import { fetchFieldValues } from '@/lib/api'

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

// Field prefixes for validators page filtering
const validatorFieldPrefixes = [
  { prefix: 'vote:', description: 'Filter by vote account pubkey' },
  { prefix: 'node:', description: 'Filter by node pubkey' },
  { prefix: 'stake:', description: 'Filter by stake (e.g., >500k, >1m)' },
  { prefix: 'city:', description: 'Filter by city' },
  { prefix: 'country:', description: 'Filter by country' },
  { prefix: 'device:', description: 'Filter by device code' },
  { prefix: 'version:', description: 'Filter by version' },
  { prefix: 'dz:', description: 'Filter by DZ status (yes/no)' },
  { prefix: 'commission:', description: 'Filter by commission %' },
  { prefix: 'skip:', description: 'Filter by skip rate' },
]

// Field prefixes for gossip nodes page filtering
const gossipNodeFieldPrefixes = [
  { prefix: 'pubkey:', description: 'Filter by node pubkey' },
  { prefix: 'ip:', description: 'Filter by IP address' },
  { prefix: 'city:', description: 'Filter by city' },
  { prefix: 'country:', description: 'Filter by country' },
  { prefix: 'device:', description: 'Filter by device code' },
  { prefix: 'version:', description: 'Filter by version' },
  { prefix: 'dz:', description: 'Filter by DZ status (yes/no)' },
  { prefix: 'validator:', description: 'Filter by validator status (yes/no)' },
  { prefix: 'stake:', description: 'Filter by stake (e.g., >500k)' },
]

// Field prefixes for devices page filtering
const deviceFieldPrefixes = [
  { prefix: 'code:', description: 'Filter by device code' },
  { prefix: 'type:', description: 'Filter by device type' },
  { prefix: 'contributor:', description: 'Filter by contributor' },
  { prefix: 'metro:', description: 'Filter by metro' },
  { prefix: 'status:', description: 'Filter by status' },
  { prefix: 'users:', description: 'Filter by user count (e.g., >10)' },
  { prefix: 'in:', description: 'Filter by inbound traffic (e.g., >1gbps)' },
  { prefix: 'out:', description: 'Filter by outbound traffic (e.g., >1gbps)' },
]

// Field prefixes for links page filtering
const linkFieldPrefixes = [
  { prefix: 'code:', description: 'Filter by link code' },
  { prefix: 'type:', description: 'Filter by link type' },
  { prefix: 'contributor:', description: 'Filter by contributor' },
  { prefix: 'sideA:', description: 'Filter by side A device' },
  { prefix: 'sideZ:', description: 'Filter by side Z device' },
  { prefix: 'status:', description: 'Filter by status' },
  { prefix: 'bandwidth:', description: 'Filter by bandwidth (e.g., >10gbps)' },
  { prefix: 'in:', description: 'Filter by inbound traffic' },
  { prefix: 'out:', description: 'Filter by outbound traffic' },
  { prefix: 'utilIn:', description: 'Filter by inbound utilization % (e.g., >50)' },
  { prefix: 'utilOut:', description: 'Filter by outbound utilization % (e.g., >50)' },
]

// Field prefixes for metros page filtering
const metroFieldPrefixes = [
  { prefix: 'code:', description: 'Filter by metro code' },
  { prefix: 'name:', description: 'Filter by metro name' },
  { prefix: 'devices:', description: 'Filter by device count (e.g., >5)' },
  { prefix: 'users:', description: 'Filter by user count (e.g., >10)' },
]

// Field prefixes for contributors page filtering
const contributorFieldPrefixes = [
  { prefix: 'code:', description: 'Filter by contributor code' },
  { prefix: 'name:', description: 'Filter by contributor name' },
  { prefix: 'devices:', description: 'Filter by device count (e.g., >5)' },
  { prefix: 'links:', description: 'Filter by link count (e.g., >10)' },
]

// Field prefixes for users page filtering
const userFieldPrefixes = [
  { prefix: 'owner:', description: 'Filter by owner pubkey' },
  { prefix: 'kind:', description: 'Filter by user kind' },
  { prefix: 'ip:', description: 'Filter by DZ IP address' },
  { prefix: 'device:', description: 'Filter by device code' },
  { prefix: 'metro:', description: 'Filter by metro' },
  { prefix: 'status:', description: 'Filter by status' },
  { prefix: 'in:', description: 'Filter by inbound traffic (e.g., >1gbps)' },
  { prefix: 'out:', description: 'Filter by outbound traffic (e.g., >1gbps)' },
]

// Map page paths to API entity names for field values
const pageToEntity: Record<string, string> = {
  '/dz/devices': 'devices',
  '/dz/links': 'links',
  '/dz/metros': 'metros',
  '/dz/contributors': 'contributors',
  '/dz/users': 'users',
  '/solana/validators': 'validators',
  '/solana/gossip-nodes': 'gossip',
}

// Fields that support autocomplete for each entity
// These should match the backend field_values.go configuration
const autocompleteFields: Record<string, string[]> = {
  devices: ['status', 'type', 'metro', 'contributor'],
  links: ['status', 'type', 'contributor', 'sidea', 'sidez'],
  metros: [],
  contributors: [],
  users: ['status', 'kind', 'metro', 'device'],
  validators: ['dz', 'version', 'device', 'city', 'country'],
  gossip: ['dz', 'validator', 'version', 'city', 'country', 'device'],
}

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
  const [globalSearchMode, setGlobalSearchMode] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const { recentSearches, addRecentSearch, clearRecentSearches } = useRecentSearches()

  const isTopologyPage = location.pathname === '/topology/map' || location.pathname === '/topology/graph'
  const isTimelinePage = location.pathname === '/timeline'
  const isStatusPage = location.pathname.startsWith('/status')
  const isOutagesPage = location.pathname === '/outages'
  const isPerformancePage = location.pathname.startsWith('/performance')
  const isValidatorsPage = location.pathname === '/solana/validators'
  const isGossipNodesPage = location.pathname === '/solana/gossip-nodes'
  const isDevicesPage = location.pathname === '/dz/devices'
  const isLinksPage = location.pathname === '/dz/links'
  const isMetrosPage = location.pathname === '/dz/metros'
  const isContributorsPage = location.pathname === '/dz/contributors'
  const isUsersPage = location.pathname === '/dz/users'
  const isDZTablePage = isDevicesPage || isLinksPage || isMetrosPage || isContributorsPage || isUsersPage

  // Check if we're on a page that supports table filtering
  const isTableFilterPage = isValidatorsPage || isGossipNodesPage || isDZTablePage
  // Use table filter mode only if on a table page and not in global search mode
  const useTableFilterMode = isTableFilterPage && !globalSearchMode

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

  // Helper to add a filter to validators/gossip pages (accumulating)
  const addTableFilter = useCallback((filterValue: string) => {
    const currentSearch = searchParams.get('search') || ''
    const currentFilters = currentSearch ? currentSearch.split(',').map(f => f.trim()).filter(Boolean) : []
    if (!currentFilters.includes(filterValue)) {
      currentFilters.push(filterValue)
    }
    setSearchParams(prev => {
      prev.set('search', currentFilters.join(','))
      // Reset to first page when filter changes
      prev.delete('offset')
      return prev
    })
  }, [searchParams, setSearchParams])

  // Focus input when opened
  useEffect(() => {
    if (isOpen) {
      setQuery('')
      setSelectedIndex(-1)
      setGlobalSearchMode(false)
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

  // Parse field:value from query for autocomplete
  const fieldValueMatch = useMemo(() => {
    if (!useTableFilterMode) return null
    const colonIndex = query.indexOf(':')
    if (colonIndex <= 0) return null
    const field = query.slice(0, colonIndex).toLowerCase()
    const value = query.slice(colonIndex + 1).toLowerCase()
    const entity = pageToEntity[location.pathname]
    if (!entity) return null
    const supportedFields = autocompleteFields[entity] || []
    if (!supportedFields.includes(field)) return null
    return { entity, field, value }
  }, [query, useTableFilterMode, location.pathname])

  // Fetch field values when a valid field prefix is detected
  const { data: fieldValuesData, isLoading: fieldValuesLoading } = useQuery({
    queryKey: ['field-values', fieldValueMatch?.entity, fieldValueMatch?.field],
    queryFn: () => fetchFieldValues(fieldValueMatch!.entity, fieldValueMatch!.field),
    enabled: isOpen && fieldValueMatch !== null,
    staleTime: 60000, // Cache for 1 minute
  })

  // Filter field values based on what user has typed after the colon
  const filteredFieldValues = useMemo(() => {
    if (!fieldValueMatch || !fieldValuesData) return []
    const needle = fieldValueMatch.value
    if (!needle) return fieldValuesData // Show all if nothing typed after colon
    return fieldValuesData.filter(v => v.toLowerCase().includes(needle))
  }, [fieldValueMatch, fieldValuesData])

  // Determine what to show
  const showRecentSearches = query.length === 0 && recentSearches.length > 0
  // Filter suggestions to only metros when on performance page
  const suggestions = isPerformancePage
    ? (data?.suggestions || []).filter(s => s.type === 'metro')
    : (data?.suggestions || [])

  // Get the appropriate field prefixes based on current page
  const getFieldPrefixes = () => {
    // In global search mode, use the global field prefixes
    if (globalSearchMode) return fieldPrefixes
    if (isValidatorsPage) return validatorFieldPrefixes
    if (isGossipNodesPage) return gossipNodeFieldPrefixes
    if (isDevicesPage) return deviceFieldPrefixes
    if (isLinksPage) return linkFieldPrefixes
    if (isMetrosPage) return metroFieldPrefixes
    if (isContributorsPage) return contributorFieldPrefixes
    if (isUsersPage) return userFieldPrefixes
    if (isPerformancePage) return [] // Only metros on performance page
    return fieldPrefixes
  }

  // Check if query matches any field prefix
  const matchingPrefixes = query.length > 0 && !query.includes(':')
    ? getFieldPrefixes().filter(p => p.prefix.toLowerCase().startsWith(query.toLowerCase()))
    : []

  // Show all field prefixes when query is empty on table pages (only in filter mode)
  const showFieldHints = query.length === 0 && useTableFilterMode

  // Filter recent searches to only metros on performance page
  const filteredRecentSearches = isPerformancePage
    ? recentSearches.filter(s => s.type === 'metro')
    : recentSearches

  // Build items list
  const items: (SearchSuggestion | { type: 'prefix'; prefix: string; description: string } | { type: 'recent'; item: SearchSuggestion } | { type: 'ask-ai' } | { type: 'filter-timeline' } | { type: 'filter-table' } | { type: 'field-value'; field: string; value: string })[] = []

  // Add "Filter timeline" option at the top when on timeline page with a query
  if (isTimelinePage && query.length >= 1) {
    items.push({ type: 'filter-timeline' as const })
  }

  // Add "Filter table" option when on table pages with a query (only in filter mode)
  // But not when we're showing field value autocomplete
  if (useTableFilterMode && query.length >= 1 && filteredFieldValues.length === 0) {
    items.push({ type: 'filter-table' as const })
  }

  // Show field value autocomplete when user has typed a field prefix
  if (filteredFieldValues.length > 0 && fieldValueMatch) {
    items.push(...filteredFieldValues.map(v => ({
      type: 'field-value' as const,
      field: fieldValueMatch.field,
      value: v
    })))
  }

  // Show field hints when empty on validators/gossip pages
  if (showFieldHints) {
    items.push(...getFieldPrefixes().map(p => ({ type: 'prefix' as const, prefix: p.prefix, description: p.description })))
  } else if (matchingPrefixes.length > 0 && filteredFieldValues.length === 0) {
    items.push(...matchingPrefixes.map(p => ({ type: 'prefix' as const, prefix: p.prefix, description: p.description })))
  }

  // Show recent searches only when NOT in table filter mode (they're for navigation, not filtering)
  if (showRecentSearches && !useTableFilterMode) {
    items.push(...filteredRecentSearches.map(item => ({ type: 'recent' as const, item })))
  }

  // Only show global suggestions when NOT in table filter mode
  if (!showRecentSearches && filteredFieldValues.length === 0 && !useTableFilterMode) {
    items.push(...suggestions)
  }

  // Add "Ask AI" option when there's a query (not on performance page or table filter mode)
  if (query.length >= 2 && !isPerformancePage && !useTableFilterMode && filteredFieldValues.length === 0) {
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

    // On validators/gossip/DZ table pages, add filter to accumulated filters instead of navigating away
    if (useTableFilterMode) {
      if (e && (e.metaKey || e.ctrlKey)) {
        // Open new tab with just this filter
        window.open(`${location.pathname}?search=${encodeURIComponent(item.label)}`, '_blank')
      } else {
        addTableFilter(item.label)
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

  const handleFilterTable = useCallback((e?: React.MouseEvent) => {
    if (!query.trim()) return
    setQuery('')
    onClose()
    if (e && (e.metaKey || e.ctrlKey)) {
      window.open(`${location.pathname}?search=${encodeURIComponent(query.trim())}`, '_blank')
    } else {
      addTableFilter(query.trim())
    }
  }, [query, onClose, addTableFilter, location.pathname])

  const handleSelectFieldValue = useCallback((field: string, value: string, e?: React.MouseEvent) => {
    const filterValue = `${field}:${value}`
    setQuery('')
    onClose()
    if (e && (e.metaKey || e.ctrlKey)) {
      window.open(`${location.pathname}?search=${encodeURIComponent(filterValue)}`, '_blank')
    } else {
      addTableFilter(filterValue)
    }
  }, [onClose, addTableFilter, location.pathname])

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
        // Use selected index, or default to first item if nothing selected
        const indexToUse = selectedIndex >= 0 ? selectedIndex : 0
        if (indexToUse < items.length) {
          const item = items[indexToUse]
          if ('prefix' in item && item.type === 'prefix') {
            setQuery(item.prefix)
            inputRef.current?.focus()
          } else if ('item' in item && item.type === 'recent') {
            handleSelect(item.item)
          } else if (item.type === 'ask-ai') {
            handleAskAI()
          } else if (item.type === 'filter-timeline') {
            handleFilterTimeline()
          } else if (item.type === 'filter-table') {
            handleFilterTable()
          } else if (item.type === 'field-value') {
            handleSelectFieldValue(item.field, item.value)
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
  }, [items, selectedIndex, handleSelect, handleAskAI, handleFilterTimeline, handleFilterTable, handleSelectFieldValue, onClose])

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
            placeholder={globalSearchMode ? "Search entities..." : isTopologyPage ? "Search entities (opens in map)..." : isTimelinePage ? "Filter timeline events..." : isStatusPage ? "Filter status by entity..." : isOutagesPage ? "Filter outages by entity..." : isPerformancePage ? "Filter by metro..." : isValidatorsPage ? "Filter validators..." : isGossipNodesPage ? "Filter gossip nodes..." : isDevicesPage ? "Filter devices..." : isLinksPage ? "Filter links..." : isMetrosPage ? "Filter metros..." : isContributorsPage ? "Filter contributors..." : isUsersPage ? "Filter users..." : "Search entities..."}
            className="flex-1 h-14 px-3 text-lg bg-transparent border-0 focus:outline-none placeholder:text-muted-foreground"
          />
          {(isLoading || fieldValuesLoading) && query.length >= 2 && (
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
          {showFieldHints && (
            <div className="px-4 py-2 text-xs text-muted-foreground border-b border-border">
              <span className="flex items-center gap-1">
                <Filter className="h-3 w-3" />
                Filter by field (or type to search all)
              </span>
            </div>
          )}

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

          {items.length === 0 && query.length < 2 && !showRecentSearches && !showFieldHints && (
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

            if (item.type === 'filter-table') {
              return (
                <button
                  key="filter-table"
                  onClick={(e) => handleFilterTable(e)}
                  className={cn(
                    'w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <Filter className="h-5 w-5 text-muted-foreground flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">Filter by "{query}"</span>
                    </div>
                    <div className="text-sm text-muted-foreground">Show rows matching this search</div>
                  </div>
                </button>
              )
            }

            if (item.type === 'field-value') {
              return (
                <button
                  key={`field-value-${item.value}`}
                  onClick={(e) => handleSelectFieldValue(item.field, item.value, e)}
                  className={cn(
                    'w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-muted transition-colors',
                    index === selectedIndex && 'bg-muted'
                  )}
                >
                  <Filter className="h-5 w-5 text-muted-foreground flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium truncate">{item.value}</span>
                      <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground flex-shrink-0">
                        {item.field}
                      </span>
                    </div>
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
                  <Filter className="h-5 w-5 text-muted-foreground flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium truncate">{item.prefix.slice(0, -1)}</span>
                      <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground flex-shrink-0">
                        filter
                      </span>
                    </div>
                    <div className="text-sm text-muted-foreground truncate">{item.description}</div>
                  </div>
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
                {useTableFilterMode && (
                  <span className="text-xs text-muted-foreground flex-shrink-0">
                    Filter
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
          <div className="flex items-center gap-2">
            {isTableFilterPage && (
              <button
                onClick={() => setGlobalSearchMode(!globalSearchMode)}
                className="text-blue-500 hover:text-blue-400 hover:underline"
              >
                {globalSearchMode ? 'Filter this page' : 'Search all'}
              </button>
            )}
            {isTableFilterPage && !globalSearchMode && (
              <span className="text-muted-foreground">·</span>
            )}
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
            {isValidatorsPage && !globalSearchMode && (
              <span className="text-blue-500">Filtering validators</span>
            )}
            {isGossipNodesPage && !globalSearchMode && (
              <span className="text-blue-500">Filtering gossip nodes</span>
            )}
            {isDevicesPage && !globalSearchMode && (
              <span className="text-blue-500">Filtering devices</span>
            )}
            {isLinksPage && !globalSearchMode && (
              <span className="text-blue-500">Filtering links</span>
            )}
            {isMetrosPage && !globalSearchMode && (
              <span className="text-blue-500">Filtering metros</span>
            )}
            {isContributorsPage && !globalSearchMode && (
              <span className="text-blue-500">Filtering contributors</span>
            )}
            {isUsersPage && !globalSearchMode && (
              <span className="text-blue-500">Filtering users</span>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
