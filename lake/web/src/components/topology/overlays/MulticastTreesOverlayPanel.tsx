import { useState, useEffect, useCallback } from 'react'
import { Radio, X, ChevronDown, ChevronRight } from 'lucide-react'
import { useTopology } from '../TopologyContext'
import {
  fetchMulticastGroups,
  type MulticastGroupListItem,
  type MulticastGroupDetail,
} from '@/lib/api'

// Colors for different multicast groups
const GROUP_COLORS = [
  { light: '#9333ea', dark: '#a855f7' },  // purple
  { light: '#2563eb', dark: '#3b82f6' },  // blue
  { light: '#16a34a', dark: '#22c55e' },  // green
  { light: '#ea580c', dark: '#f97316' },  // orange
  { light: '#0891b2', dark: '#06b6d4' },  // cyan
  { light: '#dc2626', dark: '#ef4444' },  // red
  { light: '#ca8a04', dark: '#eab308' },  // yellow
  { light: '#db2777', dark: '#ec4899' },  // pink
]

interface MulticastTreesOverlayPanelProps {
  isDark: boolean
  selectedGroups: string[]  // Group codes that are currently selected
  onToggleGroup: (code: string) => void
  onClearGroups: () => void
  groupDetails: Map<string, MulticastGroupDetail>  // Cached group details
  onLoadGroupDetail: (code: string) => void
  // Publisher/subscriber filtering
  enabledPublishers: Set<string>  // device PKs of enabled publishers
  enabledSubscribers: Set<string>  // device PKs of enabled subscribers
  onTogglePublisher: (devicePK: string) => void
  onToggleSubscriber: (devicePK: string) => void
  // Publisher color map for consistent colors
  publisherColorMap: Map<string, number>
}

export function MulticastTreesOverlayPanel({
  isDark,
  selectedGroups,
  onToggleGroup,
  onClearGroups,
  groupDetails,
  onLoadGroupDetail,
  enabledPublishers,
  enabledSubscribers,
  onTogglePublisher,
  onToggleSubscriber,
  publisherColorMap,
}: MulticastTreesOverlayPanelProps) {
  const { toggleOverlay } = useTopology()
  const [groups, setGroups] = useState<MulticastGroupListItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())

  // Fetch groups on mount
  useEffect(() => {
    setError(null)
    fetchMulticastGroups()
      .then(setGroups)
      .catch(err => {
        console.error('Failed to fetch multicast groups:', err)
        setError('Failed to load multicast groups. The database table may not exist yet.')
      })
      .finally(() => setLoading(false))
  }, [])

  // Load group details when expanded
  const handleToggleExpand = useCallback((code: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev)
      if (next.has(code)) {
        next.delete(code)
      } else {
        next.add(code)
        // Load details if not already loaded
        if (!groupDetails.has(code)) {
          onLoadGroupDetail(code)
        }
      }
      return next
    })
  }, [groupDetails, onLoadGroupDetail])

  const getGroupColor = (index: number) => {
    if (index < 0) return isDark ? '#6b7280' : '#9ca3af' // default gray for unselected
    const colorSet = GROUP_COLORS[index % GROUP_COLORS.length]
    return isDark ? colorSet.dark : colorSet.light
  }

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Radio className="h-3.5 w-3.5 text-purple-500" />
          Multicast Trees
        </span>
        <button
          onClick={() => toggleOverlay('multicastTrees')}
          className="p-1 hover:bg-[var(--muted)] rounded"
          title="Close"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      {loading && (
        <div className="text-muted-foreground">Loading groups...</div>
      )}

      {!loading && error && (
        <div className="text-red-500 text-xs">{error}</div>
      )}

      {!loading && !error && groups.length === 0 && (
        <div className="text-muted-foreground">No multicast groups found</div>
      )}

      {!loading && !error && groups.length > 0 && (
        <div className="space-y-3">
          {/* Summary */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Groups</span>
              <span className="font-medium">{groups.length}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Selected</span>
              <span className="font-medium">{selectedGroups.length}</span>
            </div>
            {selectedGroups.length > 0 && (
              <button
                onClick={onClearGroups}
                className="text-[10px] text-muted-foreground hover:text-foreground"
              >
                Clear all
              </button>
            )}
          </div>

          {/* Groups list */}
          <div className="pt-2 border-t border-[var(--border)] space-y-1">
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-1.5">
              Available Groups
            </div>
            <div className="max-h-64 overflow-y-auto space-y-0.5">
              {groups.map((group) => {
                const isSelected = selectedGroups.includes(group.code)
                const isExpanded = expandedGroups.has(group.code)
                const color = getGroupColor(selectedGroups.indexOf(group.code))
                const detail = groupDetails.get(group.code)

                return (
                  <div key={group.pk}>
                    <div
                      className={`flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer transition-colors ${
                        isSelected ? 'bg-[var(--muted)]' : 'hover:bg-[var(--muted)]'
                      }`}
                      style={isSelected ? { borderLeft: `3px solid ${color}` } : undefined}
                    >
                      <button
                        onClick={() => handleToggleExpand(group.code)}
                        className="p-0.5 hover:bg-[var(--muted)] rounded"
                      >
                        {isExpanded ? (
                          <ChevronDown className="h-3 w-3" />
                        ) : (
                          <ChevronRight className="h-3 w-3" />
                        )}
                      </button>
                      <button
                        onClick={() => onToggleGroup(group.code)}
                        className="flex-1 text-left flex items-center gap-2"
                      >
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => {}}
                          className="h-3 w-3 rounded border-[var(--border)]"
                        />
                        <span className="font-medium">{group.code}</span>
                        <span className="text-muted-foreground text-[10px] ml-auto">
                          {group.publisher_count}P / {group.subscriber_count}S
                        </span>
                      </button>
                    </div>

                    {/* Expanded details */}
                    {isExpanded && (
                      <div className="ml-6 mt-1 mb-2 pl-2 border-l border-[var(--border)] text-[10px] space-y-1">
                        <div className="text-muted-foreground">
                          IP: {group.multicast_ip}
                        </div>
                        {detail ? (
                          <>
                            {detail.members.filter(m => m.mode === 'P' || m.mode === 'P+S').length > 0 && (
                              <div>
                                <div className="text-muted-foreground uppercase tracking-wider mb-0.5">Publishers</div>
                                {detail.members
                                  .filter(m => m.mode === 'P' || m.mode === 'P+S')
                                  .map(m => {
                                    const pubColorIndex = publisherColorMap.get(m.device_pk) ?? 0
                                    const pubColor = GROUP_COLORS[pubColorIndex % GROUP_COLORS.length]
                                    const colorStyle = isDark ? pubColor.dark : pubColor.light
                                    const isEnabled = enabledPublishers.has(m.device_pk)
                                    return (
                                      <div
                                        key={m.user_pk}
                                        className={`flex items-center gap-1 py-0.5 cursor-pointer hover:bg-[var(--muted)] rounded px-1 -mx-1 ${!isEnabled ? 'opacity-40' : ''}`}
                                        onClick={() => onTogglePublisher(m.device_pk)}
                                      >
                                        <input
                                          type="checkbox"
                                          checked={isEnabled}
                                          onChange={() => {}}
                                          className="h-2.5 w-2.5 rounded border-[var(--border)] flex-shrink-0"
                                        />
                                        <div
                                          className="w-3 h-3 rounded-full flex-shrink-0"
                                          style={{ backgroundColor: colorStyle }}
                                        />
                                        <span className="font-medium">P</span>
                                        <span>{m.device_code}</span>
                                        <span className="text-muted-foreground">({m.metro_code})</span>
                                      </div>
                                    )
                                  })}
                              </div>
                            )}
                            {detail.members.filter(m => m.mode === 'S' || m.mode === 'P+S').length > 0 && (
                              <div>
                                <div className="text-muted-foreground uppercase tracking-wider mb-0.5">Subscribers</div>
                                {detail.members
                                  .filter(m => m.mode === 'S' || m.mode === 'P+S')
                                  .map(m => {
                                    const isEnabled = enabledSubscribers.has(m.device_pk)
                                    return (
                                      <div
                                        key={m.user_pk + '-sub'}
                                        className={`flex items-center gap-1 py-0.5 cursor-pointer hover:bg-[var(--muted)] rounded px-1 -mx-1 ${!isEnabled ? 'opacity-40' : ''}`}
                                        onClick={() => onToggleSubscriber(m.device_pk)}
                                      >
                                        <input
                                          type="checkbox"
                                          checked={isEnabled}
                                          onChange={() => {}}
                                          className="h-2.5 w-2.5 rounded border-[var(--border)] flex-shrink-0"
                                        />
                                        <div className="w-3 h-3 rounded-full bg-red-500 flex-shrink-0" />
                                        <span className="font-medium">S</span>
                                        <span>{m.device_code}</span>
                                        <span className="text-muted-foreground">({m.metro_code})</span>
                                      </div>
                                    )
                                  })}
                              </div>
                            )}
                          </>
                        ) : (
                          <div className="text-muted-foreground">Loading members...</div>
                        )}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          </div>

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-1.5">
              Legend
            </div>
            <div className="space-y-1.5 text-[10px]">
              <div className="flex items-center gap-2">
                <div className="flex gap-0.5">
                  {GROUP_COLORS.slice(0, 4).map((c, i) => (
                    <div
                      key={i}
                      className="w-2 h-2 rounded-full"
                      style={{ backgroundColor: isDark ? c.dark : c.light }}
                    />
                  ))}
                </div>
                <span>Publisher (each has unique color)</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="w-3 h-3 rounded-full bg-red-500 flex-shrink-0" />
                <span>Subscriber (destination)</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="w-6 h-0.5 bg-purple-500 rounded" />
                <span>Tree path (color matches publisher)</span>
              </div>
              <div className="flex items-center gap-2">
                <div
                  className="w-6 h-1 rounded"
                  style={{
                    background: `repeating-linear-gradient(90deg, ${isDark ? '#a855f7' : '#9333ea'} 0 3px, transparent 3px 6px)`,
                  }}
                />
                <span>Shared path (multiple publishers)</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
