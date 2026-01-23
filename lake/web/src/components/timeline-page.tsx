import { useState, useEffect, useRef, useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery, useInfiniteQuery } from '@tanstack/react-query'
import {
  Clock,
  Server,
  Landmark,
  Link2,
  MapPin,
  Building2,
  Users,
  Radio,
  AlertTriangle,
  AlertCircle,
  Info,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  GitCommit,
  CheckCircle2,
  Wifi,
  WifiOff,
  AlertOctagon,
  Plus,
  Trash2,
  Pencil,
  Play,
  Square,
  RotateCcw,
  LogIn,
  LogOut,
  Calendar,
  TrendingUp,
  TrendingDown,
  RotateCw,
  Search,
  X,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  XAxis,
  Tooltip,
} from 'recharts'
import {
  fetchTimeline,
  fetchTimelineBounds,
  type TimelineEvent,
  type TimeRange,
  type ActionFilter,
  type EntityChangeDetails,
  type PacketLossEventDetails,
  type InterfaceEventDetails,
  type GroupedInterfaceDetails,
  type ValidatorEventDetails,
  type FieldChange,
  type DeviceEntity,
  type LinkEntity,
  type MetroEntity,
  type ContributorEntity,
  type UserEntity,
  type HistogramBucket,
} from '@/lib/api'

type Category = 'state_change' | 'packet_loss' | 'interface_carrier' | 'interface_errors' | 'interface_discards'
type EntityType = 'device' | 'link' | 'metro' | 'contributor' | 'user' | 'validator' | 'gossip_node'
type DZFilter = 'on_dz' | 'off_dz' | 'all'

const timeRangeOptions: { value: TimeRange | 'custom'; label: string }[] = [
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '12h', label: '12h' },
  { value: '24h', label: '24h' },
  { value: '3d', label: '3d' },
  { value: '7d', label: '7d' },
  { value: 'custom', label: 'Custom' },
]

const actionOptions: { value: ActionFilter; label: string; icon: typeof Plus }[] = [
  { value: 'added', label: 'Added', icon: Plus },
  { value: 'removed', label: 'Removed', icon: Trash2 },
  { value: 'changed', label: 'Changed', icon: Pencil },
  { value: 'alerting', label: 'Alerting', icon: AlertTriangle },
  { value: 'resolved', label: 'Resolved', icon: CheckCircle2 },
]

const ALL_ACTIONS: ActionFilter[] = ['added', 'removed', 'changed', 'alerting', 'resolved']

const categoryOptions: { value: Category; label: string; icon: typeof Server }[] = [
  { value: 'state_change', label: 'State Changes', icon: GitCommit },
  { value: 'packet_loss', label: 'Packet Loss', icon: Wifi },
  { value: 'interface_carrier', label: 'Carrier', icon: WifiOff },
  { value: 'interface_errors', label: 'Errors', icon: AlertOctagon },
  { value: 'interface_discards', label: 'Discards', icon: AlertTriangle },
]

// DZ Infrastructure entities
const dzEntityOptions: { value: EntityType; label: string; icon: typeof Server }[] = [
  { value: 'device', label: 'Devices', icon: Server },
  { value: 'link', label: 'Links', icon: Link2 },
  { value: 'metro', label: 'Metros', icon: MapPin },
  { value: 'contributor', label: 'Contributors', icon: Building2 },
  { value: 'user', label: 'Users', icon: Users },
]

// Solana entities
const solanaEntityOptions: { value: EntityType; label: string; icon: typeof Server }[] = [
  { value: 'validator', label: 'Validators', icon: Landmark },
  { value: 'gossip_node', label: 'Gossip Nodes', icon: Radio },
]

const ALL_DZ_ENTITIES: EntityType[] = ['device', 'link', 'metro', 'contributor', 'user']
const ALL_SOLANA_ENTITIES: EntityType[] = ['validator', 'gossip_node']
const ALL_ENTITY_TYPES: EntityType[] = [...ALL_DZ_ENTITIES, ...ALL_SOLANA_ENTITIES]
const DEFAULT_ENTITY_TYPES: EntityType[] = ALL_ENTITY_TYPES.filter(e => e !== 'gossip_node')

const dzFilterOptions: { value: DZFilter; label: string }[] = [
  { value: 'on_dz', label: 'On DZ' },
  { value: 'off_dz', label: 'Off DZ' },
  { value: 'all', label: 'All' },
]

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse bg-muted rounded ${className || ''}`} />
}

// Compact dropdown for multi-select filters
function FilterDropdown<T extends string>({
  label,
  options,
  selected,
  onToggle,
  allValues,
}: {
  label: string
  options: { value: T; label: string; icon?: typeof Server }[]
  selected: Set<T>
  onToggle: (value: T) => void
  allValues: T[]
}) {
  const [open, setOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  // Close on click outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    if (open) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [open])

  const allSelected = allValues.every(v => selected.has(v))
  const noneSelected = allValues.every(v => !selected.has(v))
  const selectedCount = allValues.filter(v => selected.has(v)).length

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setOpen(!open)}
        className={cn(
          'flex items-center gap-1.5 px-2 py-1 text-xs rounded-md border transition-colors',
          open || (!allSelected && !noneSelected)
            ? 'bg-background border-border text-foreground'
            : 'border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/50'
        )}
      >
        <span className="uppercase tracking-wide">{label}</span>
        {!allSelected && (
          <span className="bg-primary/10 text-primary px-1 rounded text-[10px] font-medium">
            {selectedCount}
          </span>
        )}
        <ChevronDown className={cn('h-3 w-3 transition-transform', open && 'rotate-180')} />
      </button>

      {open && (
        <div className="absolute top-full left-0 mt-1 z-50 min-w-[160px] bg-popover border border-border rounded-md shadow-lg py-1 whitespace-nowrap">
          {options.map(option => {
            const Icon = option.icon
            const isSelected = selected.has(option.value)
            return (
              <button
                key={option.value}
                onClick={() => onToggle(option.value)}
                className="w-full flex items-center gap-2 px-3 py-1.5 text-xs hover:bg-muted transition-colors"
              >
                <div className={cn(
                  'w-3.5 h-3.5 rounded border flex items-center justify-center',
                  isSelected ? 'bg-primary border-primary' : 'border-muted-foreground/30'
                )}>
                  {isSelected && <CheckCircle2 className="h-2.5 w-2.5 text-primary-foreground" />}
                </div>
                {Icon && <Icon className="h-3 w-3 text-muted-foreground" />}
                <span className={isSelected ? 'text-foreground' : 'text-muted-foreground'}>{option.label}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

function formatBucketTime(timestamp: string): string {
  const date = new Date(timestamp)
  if (date.getMinutes() === 0) {
    return date.toLocaleTimeString([], { hour: 'numeric', hour12: true })
  }
  return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', hour12: true })
}

function formatBucketDate(timestamp: string): string {
  const date = new Date(timestamp)
  return date.toLocaleDateString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

function EventHistogram({ data, onBucketClick }: { data: HistogramBucket[], onBucketClick?: (bucket: HistogramBucket, nextBucket?: HistogramBucket) => void }) {
  if (!data || data.length < 6) return null // Need enough bars to be useful

  const maxCount = Math.max(...data.map(d => d.count))
  if (maxCount === 0) return null

  // Determine if range spans multiple days
  const firstDate = new Date(data[0].timestamp)
  const lastDate = new Date(data[data.length - 1].timestamp)
  const spansDays = lastDate.getTime() - firstDate.getTime() > 24 * 60 * 60 * 1000

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const handleBarClick = (barData: any) => {
    if (!onBucketClick || !barData?.payload) return
    const clickedBucket = barData.payload as HistogramBucket
    const idx = data.findIndex(b => b.timestamp === clickedBucket.timestamp)
    const nextBucket = idx >= 0 ? data[idx + 1] : undefined
    onBucketClick(clickedBucket, nextBucket)
  }

  return (
    <div className="mb-4 border border-border rounded-lg p-3 bg-card">
      <div className="h-12">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} margin={{ top: 0, right: 0, left: 0, bottom: 0 }}>
            <XAxis
              dataKey="timestamp"
              axisLine={false}
              tickLine={false}
              tick={false}
              hide
            />
            <Tooltip
              cursor={{ fill: 'hsl(var(--foreground))', opacity: 0.1 }}
              content={({ active, payload }) => {
                if (!active || !payload?.[0]) return null
                const bucket = payload[0].payload as HistogramBucket
                return (
                  <div className="bg-popover border border-border rounded px-2 py-1 text-xs shadow-lg">
                    <div className="font-medium">{bucket.count} events</div>
                    <div className="text-muted-foreground">{formatBucketDate(bucket.timestamp)}</div>
                    {onBucketClick && <div className="text-muted-foreground/70 mt-0.5">Click to filter</div>}
                  </div>
                )
              }}
            />
            <Bar
              dataKey="count"
              fill="hsl(220, 70%, 50%)"
              opacity={0.6}
              radius={[2, 2, 0, 0]}
              cursor={onBucketClick ? 'pointer' : undefined}
              onClick={handleBarClick}
            />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <div className="flex justify-between text-[10px] text-muted-foreground mt-1">
        <span>{spansDays ? formatBucketDate(data[0].timestamp) : formatBucketTime(data[0].timestamp)}</span>
        <span>{spansDays ? formatBucketDate(data[data.length - 1].timestamp) : formatBucketTime(data[data.length - 1].timestamp)}</span>
      </div>
    </div>
  )
}

const severityStyles: Record<string, string> = {
  info: 'border-l-blue-500 bg-card',
  warning: 'border-l-amber-500 bg-card',
  critical: 'border-l-red-500 bg-card',
  success: 'border-l-green-500 bg-card',
}

const severityIcons: Record<string, typeof Info> = {
  info: Info,
  warning: AlertTriangle,
  critical: AlertCircle,
  success: CheckCircle2,
}

const categoryIcons: Record<string, typeof Server> = {
  state_change: GitCommit,
  packet_loss: Wifi,
  interface_carrier: WifiOff,
  interface_errors: AlertOctagon,
  interface_discards: AlertTriangle,
}

const entityIcons: Record<string, typeof Server> = {
  device: Server,
  link: Link2,
  metro: MapPin,
  contributor: Building2,
  user: Users,
  validator: Landmark,
  gossip_node: Radio,
}

// Get severity color class based on count thresholds
function getSeverityColor(value: number, thresholds: { low: number; medium: number; high: number }): string {
  if (value >= thresholds.high) return 'text-red-600 font-semibold'
  if (value >= thresholds.medium) return 'text-orange-500 font-medium'
  if (value >= thresholds.low) return 'text-amber-500'
  return 'text-muted-foreground'
}

// Get severity color for packet loss percentage
function getLossColor(pct: number): string {
  if (pct >= 5) return 'text-red-600 font-semibold'
  if (pct >= 1) return 'text-orange-500 font-medium'
  if (pct >= 0.1) return 'text-amber-500'
  return 'text-muted-foreground'
}

// Thresholds for different metric types
const errorThresholds = { low: 10, medium: 100, high: 1000 }
const discardThresholds = { low: 100, medium: 1000, high: 10000 }
const carrierThresholds = { low: 1, medium: 5, high: 10 }

function formatTimeAgo(timestamp: string): string {
  const now = new Date()
  const then = new Date(timestamp)
  const diffMs = now.getTime() - then.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  if (diffHours < 24) return `${diffHours}h ago`
  if (diffDays < 7) return `${diffDays}d ago`
  return then.toLocaleDateString()
}

// Format a field value for display
function formatValue(value: unknown, field: string): string {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'number') {
    if (field.includes('bandwidth') || field.includes('bps')) {
      if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)} Gbps`
      if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)} Mbps`
      return `${value} bps`
    }
    if (field.includes('rtt') || field.includes('jitter') || field.includes('delay')) {
      if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)} ms`
      if (value >= 1_000) return `${(value / 1_000).toFixed(2)} µs`
      return `${value} ns`
    }
    return value.toLocaleString()
  }
  if (typeof value === 'string') {
    // Truncate long strings like PKs
    if (value.length > 20) return value.slice(0, 8) + '...' + value.slice(-4)
    return value
  }
  return String(value)
}

// Human-readable field names
const fieldLabels: Record<string, string> = {
  status: 'Status',
  device_type: 'Type',
  public_ip: 'Public IP',
  contributor: 'Contributor',
  metro: 'Metro',
  max_users: 'Max Users',
  link_type: 'Type',
  tunnel_net: 'Tunnel Net',
  side_a: 'Side A',
  side_z: 'Side Z',
  committed_rtt: 'Committed RTT',
  committed_jitter: 'Committed Jitter',
  bandwidth: 'Bandwidth',
  name: 'Name',
  longitude: 'Longitude',
  latitude: 'Latitude',
  code: 'Code',
  kind: 'Kind',
  client_ip: 'Client IP',
  dz_ip: 'DZ IP',
  device: 'Device',
  tunnel_id: 'Tunnel ID',
}

// Component to show field changes prominently
function ChangeSummary({ changes }: { changes?: FieldChange[] }) {
  if (!changes || changes.length === 0) return null

  return (
    <div className="mt-2 flex flex-wrap gap-2">
      {changes.map((change, idx) => (
        <div
          key={idx}
          className="inline-flex items-center gap-1 text-xs bg-muted/50 px-2 py-1 rounded border border-border"
        >
          <span className="text-muted-foreground">{fieldLabels[change.field] || change.field}:</span>
          <span className="text-amber-600 line-through">{formatValue(change.old_value, change.field)}</span>
          <span className="text-muted-foreground">→</span>
          <span className="text-green-600 font-medium">{formatValue(change.new_value, change.field)}</span>
        </div>
      ))}
    </div>
  )
}

// Clickable filter button for timeline filtering - accumulates filters
function FilterButton({ children, value, className }: { children: React.ReactNode; value: string; className?: string }) {
  const [searchParams, setSearchParams] = useSearchParams()

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    // Get existing filters and add this one if not already present
    const currentSearch = searchParams.get('search') || ''
    const currentFilters = currentSearch ? currentSearch.split(',').map(f => f.trim()).filter(Boolean) : []
    if (!currentFilters.includes(value)) {
      currentFilters.push(value)
    }
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)
      next.set('search', currentFilters.join(','))
      return next
    })
  }

  return (
    <button
      onClick={handleClick}
      className={cn("hover:underline cursor-pointer", className)}
      title={`Filter by "${value}"`}
    >
      {children}
    </button>
  )
}

// Helper component for entity links in details
function EntityLink({ to, children }: { to: string, children: React.ReactNode }) {
  return (
    <Link to={to} className="font-medium text-foreground hover:underline">
      {children}
    </Link>
  )
}

// Component to show full entity details
function EntityDetailsView({ entity, entityType }: { entity: DeviceEntity | LinkEntity | MetroEntity | ContributorEntity | UserEntity, entityType: string }) {
  if (entityType === 'device') {
    const d = entity as DeviceEntity
    return (
      <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <div>Type: <span className="font-medium text-foreground">{d.device_type}</span></div>
        <div>Status: <span className="font-medium text-foreground">{d.status}</span></div>
        <div>Public IP: <span className="font-mono text-foreground">{d.public_ip}</span></div>
        <div>Max Users: <span className="font-medium text-foreground">{d.max_users}</span></div>
        {d.metro_pk && d.metro_code && <div>Metro: <EntityLink to={`/dz/metros/${encodeURIComponent(d.metro_pk)}`}>{d.metro_code}</EntityLink></div>}
        {d.contributor_pk && d.contributor_code && <div>Contributor: <EntityLink to={`/dz/contributors/${encodeURIComponent(d.contributor_pk)}`}>{d.contributor_code}</EntityLink></div>}
      </div>
    )
  }

  if (entityType === 'link') {
    const d = entity as LinkEntity
    return (
      <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <div>Type: <span className="font-medium text-foreground">{d.link_type}</span></div>
        <div>Status: <span className="font-medium text-foreground">{d.status}</span></div>
        <div>Route: {d.side_a_pk ? <EntityLink to={`/dz/devices/${encodeURIComponent(d.side_a_pk)}`}>{d.side_a_code}</EntityLink> : <span className="font-medium text-foreground">{d.side_a_code}</span>} → {d.side_z_pk ? <EntityLink to={`/dz/devices/${encodeURIComponent(d.side_z_pk)}`}>{d.side_z_code}</EntityLink> : <span className="font-medium text-foreground">{d.side_z_code}</span>}</div>
        <div>Metros: {d.side_a_metro_pk ? <EntityLink to={`/dz/metros/${encodeURIComponent(d.side_a_metro_pk)}`}>{d.side_a_metro_code}</EntityLink> : <span className="font-medium text-foreground">{d.side_a_metro_code}</span>} → {d.side_z_metro_pk ? <EntityLink to={`/dz/metros/${encodeURIComponent(d.side_z_metro_pk)}`}>{d.side_z_metro_code}</EntityLink> : <span className="font-medium text-foreground">{d.side_z_metro_code}</span>}</div>
        {d.bandwidth_bps > 0 && <div>Bandwidth: <span className="font-medium text-foreground">{formatValue(d.bandwidth_bps, 'bandwidth')}</span></div>}
        {d.committed_rtt_ns > 0 && <div>Committed RTT: <span className="font-medium text-foreground">{formatValue(d.committed_rtt_ns, 'rtt')}</span></div>}
        {d.contributor_pk && d.contributor_code && <div>Contributor: <EntityLink to={`/dz/contributors/${encodeURIComponent(d.contributor_pk)}`}>{d.contributor_code}</EntityLink></div>}
        {d.tunnel_net && <div>Tunnel Net: <span className="font-mono text-foreground">{d.tunnel_net}</span></div>}
      </div>
    )
  }

  if (entityType === 'metro') {
    const d = entity as MetroEntity
    return (
      <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <div>Name: <span className="font-medium text-foreground">{d.name}</span></div>
        <div>Code: <span className="font-medium text-foreground">{d.code}</span></div>
        <div>Coordinates: <span className="font-mono text-foreground">{d.latitude.toFixed(4)}, {d.longitude.toFixed(4)}</span></div>
      </div>
    )
  }

  if (entityType === 'contributor') {
    const d = entity as ContributorEntity
    return (
      <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <div>Name: <span className="font-medium text-foreground">{d.name}</span></div>
        <div>Code: <span className="font-medium text-foreground">{d.code}</span></div>
      </div>
    )
  }

  if (entityType === 'user') {
    const d = entity as UserEntity
    return (
      <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <div className="col-span-2">Owner: <span className="font-mono text-foreground break-all">{d.owner_pubkey}</span></div>
        <div>Status: <span className="font-medium text-foreground">{d.status}</span></div>
        <div>Kind: <span className="font-medium text-foreground">{d.kind}</span></div>
        {d.device_pk && d.device_code && <div>Device: <EntityLink to={`/dz/devices/${encodeURIComponent(d.device_pk)}`}>{d.device_code}</EntityLink></div>}
        {d.client_ip && <div>Client IP: <span className="font-mono text-foreground">{d.client_ip}</span></div>}
        {d.dz_ip && <div>DZ IP: <span className="font-mono text-foreground">{d.dz_ip}</span></div>}
      </div>
    )
  }

  return null
}

function EventDetails({ event }: { event: TimelineEvent }) {
  const details = event.details
  if (!details) return null

  if (event.category === 'state_change' && 'change_type' in details) {
    const d = details as EntityChangeDetails
    return (
      <div className="text-xs text-muted-foreground mt-2 space-y-2">
        {d.entity && <EntityDetailsView entity={d.entity} entityType={event.entity_type} />}
      </div>
    )
  }

  // Packet loss and interface events - details shown outside, no need for expanded details
  if (event.event_type.startsWith('packet_loss') || event.event_type.startsWith('interface_')) {
    return null
  }

  if ((event.event_type.includes('validator') || event.event_type.includes('gossip_node')) && 'action' in details) {
    const d = details as ValidatorEventDetails
    const isValidator = d.kind === 'validator'
    return (
      <div className="text-xs text-muted-foreground mt-2 space-y-1">
        <div>Owner: <FilterButton value={d.owner_pubkey} className="font-mono break-all">{d.owner_pubkey}</FilterButton></div>
        {d.user_pk && <div>User: <FilterButton value={d.user_pk} className="font-mono break-all">{d.user_pk}</FilterButton></div>}
        {d.dz_ip && <div>DZ IP: <span className="font-mono">{d.dz_ip}</span></div>}
        {isValidator && d.vote_pubkey && <div>Vote Account: <span className="font-mono break-all">{d.vote_pubkey}</span></div>}
        {!isValidator && d.node_pubkey && <div>Node Pubkey: <span className="font-mono break-all">{d.node_pubkey}</span></div>}
        {isValidator && d.stake_sol !== undefined && d.stake_sol > 0 && (
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            <span>Stake: <span className="font-medium text-foreground">{d.stake_sol.toLocaleString(undefined, { maximumFractionDigits: 0 })} SOL</span></span>
            {d.stake_share_pct !== undefined && (
              <span>Network Share: <span className="font-medium text-foreground">{d.stake_share_pct.toFixed(3)}%</span></span>
            )}
          </div>
        )}
        {d.device_pk && d.device_code && <div>Device: <EntityLink to={`/dz/devices/${encodeURIComponent(d.device_pk)}`}>{d.device_code}</EntityLink></div>}
      </div>
    )
  }

  return null
}

function TimelineEventCard({ event, isNew }: { event: TimelineEvent; isNew?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const CategoryIcon = categoryIcons[event.category] || Server
  const EntityIcon = entityIcons[event.entity_type] || Server
  const SeverityIcon = severityIcons[event.severity]
  // Interface and packet loss events show details outside, so no expandable details needed
  const hasDetails = event.details && Object.keys(event.details).length > 0
    && !event.event_type.startsWith('interface_')
    && !event.event_type.startsWith('packet_loss')

  // Extract changes and change type for state_change events
  const changeDetails = event.category === 'state_change' && event.details && 'change_type' in event.details
    ? event.details as EntityChangeDetails
    : undefined
  const changes = changeDetails?.changes
  const changeType = changeDetails?.change_type

  // Extract validator/gossip node details for showing device connection prominently
  const validatorDetails = (event.event_type.includes('validator') || event.event_type.includes('gossip_node')) && event.details && 'action' in event.details
    ? event.details as ValidatorEventDetails
    : undefined

  // Extract packet loss details for showing metros prominently
  const packetLossDetails = event.event_type.startsWith('packet_loss') && event.details && 'current_loss_pct' in event.details
    ? event.details as PacketLossEventDetails
    : undefined

  // Extract interface details (single or grouped)
  const singleInterfaceDetails = event.event_type.startsWith('interface_') && event.details && 'interface_name' in event.details
    ? event.details as InterfaceEventDetails
    : undefined
  const groupedInterfaceDetails = event.event_type.startsWith('interface_') && event.details && 'interfaces' in event.details
    ? event.details as GroupedInterfaceDetails
    : undefined

  // Compute totals for interface events
  const interfaceTotals = (() => {
    if (singleInterfaceDetails) {
      return {
        interfaces: [singleInterfaceDetails],
        carrierTransitions: singleInterfaceDetails.carrier_transitions || 0,
        errors: (singleInterfaceDetails.in_errors || 0) + (singleInterfaceDetails.out_errors || 0),
        discards: (singleInterfaceDetails.in_discards || 0) + (singleInterfaceDetails.out_discards || 0),
      }
    }
    if (groupedInterfaceDetails) {
      return {
        interfaces: groupedInterfaceDetails.interfaces,
        carrierTransitions: groupedInterfaceDetails.interfaces.reduce((sum, i) => sum + (i.carrier_transitions || 0), 0),
        errors: groupedInterfaceDetails.interfaces.reduce((sum, i) => sum + (i.in_errors || 0) + (i.out_errors || 0), 0),
        discards: groupedInterfaceDetails.interfaces.reduce((sum, i) => sum + (i.in_discards || 0) + (i.out_discards || 0), 0),
      }
    }
    return null
  })()

  // Extract device info from user entity change events
  const userEntity = event.entity_type === 'user' && changeDetails?.entity
    ? changeDetails.entity as UserEntity
    : undefined

  return (
    <div
      className={cn(
        'border border-border border-l-4 rounded-lg p-4 transition-all duration-500',
        severityStyles[event.severity],
        isNew && 'animate-slide-in shadow-[0_0_12px_rgba(59,130,246,0.3)] border-blue-400/40'
      )}
    >
      <div className="flex items-start gap-3">
        <div className="flex flex-col items-center gap-1">
          <CategoryIcon className="h-4 w-4 text-muted-foreground" />
          <EntityIcon className="h-3 w-3 text-muted-foreground/60" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1 flex-wrap">
            <FilterButton
              value={event.entity_code}
              className={cn(
                "font-medium text-sm text-left",
                // For pubkeys (long strings), truncate on mobile but show full on desktop
                event.entity_code.length > 20 && "font-mono max-w-[120px] sm:max-w-[200px] md:max-w-none truncate"
              )}
            >
              {event.entity_code}
            </FilterButton>
            {/* State change badges */}
            {changeType === 'created' && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/10 text-green-600 border border-green-500/20">
                <Plus className="h-3 w-3" />
                Created
              </span>
            )}
            {changeType === 'updated' && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-600 border border-blue-500/20">
                <Pencil className="h-3 w-3" />
                Updated
              </span>
            )}
            {changeType === 'deleted' && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-red-500/10 text-red-600 border border-red-500/20">
                <Trash2 className="h-3 w-3" />
                Deleted
              </span>
            )}
            {/* Telemetry event badges */}
            {event.event_type.endsWith('_started') && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 border border-amber-500/20">
                <Play className="h-3 w-3" />
                Started
              </span>
            )}
            {event.event_type.endsWith('_stopped') && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/10 text-green-600 border border-green-500/20">
                <Square className="h-3 w-3" />
                Stopped
              </span>
            )}
            {event.event_type.endsWith('_recovered') && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/10 text-green-600 border border-green-500/20">
                <RotateCcw className="h-3 w-3" />
                Recovered
              </span>
            )}
            {/* Validator/Gossip node badges */}
            {event.event_type.endsWith('_joined') && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/10 text-green-600 border border-green-500/20">
                <LogIn className="h-3 w-3" />
                Joined
              </span>
            )}
            {event.event_type.endsWith('_left') && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 border border-amber-500/20">
                <LogOut className="h-3 w-3" />
                Left
              </span>
            )}
            {event.event_type.endsWith('_offline') && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 border border-amber-500/20">
                <LogOut className="h-3 w-3" />
                Left
              </span>
            )}
            {event.event_type === 'stake_increased' && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-green-500/10 text-green-600 border border-green-500/20">
                <TrendingUp className="h-3 w-3" />
                Stake Up
              </span>
            )}
            {event.event_type === 'stake_decreased' && (
              <span className="inline-flex items-center gap-1 text-xs px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 border border-amber-500/20">
                <TrendingDown className="h-3 w-3" />
                Stake Down
              </span>
            )}
            <span
              className="text-xs text-muted-foreground flex items-center gap-1"
              title={new Date(event.timestamp).toLocaleString()}
            >
              <Clock className="h-3 w-3" />
              {formatTimeAgo(event.timestamp)}
            </span>
            <SeverityIcon className={cn(
              'h-3 w-3',
              event.severity === 'critical' && 'text-red-500',
              event.severity === 'warning' && 'text-amber-500',
              event.severity === 'info' && 'text-blue-500',
              event.severity === 'success' && 'text-green-500'
            )} />
          </div>
          <div className="text-sm">{event.title}</div>
          {event.description && (
            <div className="text-xs text-muted-foreground mt-1">{event.description}</div>
          )}

          {/* Show owner and user for validator/gossip events */}
          {validatorDetails?.owner_pubkey && (
            <div className="text-xs text-muted-foreground mt-1">
              Owner: <FilterButton value={validatorDetails.owner_pubkey} className="font-mono text-foreground">{validatorDetails.owner_pubkey.length > 20 ? validatorDetails.owner_pubkey.slice(0, 8) + '...' + validatorDetails.owner_pubkey.slice(-4) : validatorDetails.owner_pubkey}</FilterButton>
              {validatorDetails.user_pk && (
                <span className="ml-2">
                  User: <FilterButton value={validatorDetails.user_pk} className="font-mono text-foreground">{validatorDetails.user_pk.length > 20 ? validatorDetails.user_pk.slice(0, 8) + '...' + validatorDetails.user_pk.slice(-4) : validatorDetails.user_pk}</FilterButton>
                </span>
              )}
            </div>
          )}

          {/* Show device connection for validator/gossip/user events */}
          {validatorDetails?.device_pk && validatorDetails?.device_code && (
            <div className="text-xs text-muted-foreground mt-1">
              Connected to <FilterButton value={validatorDetails.device_code} className="font-medium text-foreground">{validatorDetails.device_code}</FilterButton>
              {validatorDetails.metro_code && <span className="text-muted-foreground"> in <FilterButton value={validatorDetails.metro_code} className="text-foreground">{validatorDetails.metro_code}</FilterButton></span>}
            </div>
          )}
          {userEntity?.device_pk && userEntity?.device_code && (
            <div className="text-xs text-muted-foreground mt-1">
              Connected to <FilterButton value={userEntity.device_code} className="font-medium text-foreground">{userEntity.device_code}</FilterButton>
              {userEntity.metro_code && <span className="text-muted-foreground"> in <FilterButton value={userEntity.metro_code} className="text-foreground">{userEntity.metro_code}</FilterButton></span>}
            </div>
          )}

          {/* Show packet loss route and percentage prominently */}
          {packetLossDetails && (
            <div className="text-xs text-muted-foreground mt-1 space-y-0.5">
              <div>
                Route: <FilterButton value={packetLossDetails.side_a_metro} className="font-medium text-foreground">{packetLossDetails.side_a_metro}</FilterButton>
                {' → '}
                <FilterButton value={packetLossDetails.side_z_metro} className="font-medium text-foreground">{packetLossDetails.side_z_metro}</FilterButton>
              </div>
              <div>
                Loss: <span className={getLossColor(packetLossDetails.current_loss_pct)}>{packetLossDetails.current_loss_pct.toFixed(2)}%</span>
                {packetLossDetails.previous_loss_pct !== undefined && (
                  <span className="text-muted-foreground"> (was {packetLossDetails.previous_loss_pct.toFixed(2)}%)</span>
                )}
              </div>
            </div>
          )}

          {/* Show interface events summary prominently */}
          {interfaceTotals && (
            <div className="text-xs text-muted-foreground mt-1 space-y-1">
              {/* Show the metric count */}
              {interfaceTotals.carrierTransitions > 0 && (
                <div>
                  Carrier transitions: <span className={getSeverityColor(interfaceTotals.carrierTransitions, carrierThresholds)}>{interfaceTotals.carrierTransitions}</span>
                </div>
              )}
              {interfaceTotals.errors > 0 && (
                <div>
                  Errors: <span className={getSeverityColor(interfaceTotals.errors, errorThresholds)}>{interfaceTotals.errors}</span>
                </div>
              )}
              {interfaceTotals.discards > 0 && (
                <div>
                  Discards: <span className={getSeverityColor(interfaceTotals.discards, discardThresholds)}>{interfaceTotals.discards}</span>
                </div>
              )}
              {/* Show interface list with per-interface counts */}
              <div className="flex flex-wrap gap-1">
                {interfaceTotals.interfaces.map((intf, i) => {
                  const carrier = intf.carrier_transitions || 0
                  const errors = (intf.in_errors || 0) + (intf.out_errors || 0)
                  const discards = (intf.in_discards || 0) + (intf.out_discards || 0)
                  // Show the relevant metric based on issue type
                  const count = carrier > 0 ? carrier : errors > 0 ? errors : discards
                  const colorClass = carrier > 0
                    ? getSeverityColor(carrier, carrierThresholds)
                    : errors > 0
                      ? getSeverityColor(errors, errorThresholds)
                      : getSeverityColor(discards, discardThresholds)
                  return (
                    <span key={i} className="bg-muted px-1.5 py-0.5 rounded text-[10px]">
                      {intf.interface_name}
                      {count > 0 && <span className={colorClass}> ({count})</span>}
                    </span>
                  )
                })}
              </div>
            </div>
          )}

          {/* Show changes prominently outside the collapsed section */}
          <ChangeSummary changes={changes} />

          {hasDetails && (
            <>
              <button
                onClick={() => setExpanded(!expanded)}
                className="flex items-center gap-1 text-xs text-muted-foreground mt-2 hover:text-foreground transition-colors"
              >
                {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                {expanded ? 'Hide' : 'Show'} details
              </button>
              {expanded && <EventDetails event={event} />}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

const ALL_CATEGORIES: Category[] = ['state_change', 'packet_loss', 'interface_carrier', 'interface_errors', 'interface_discards']

// Helper to parse a comma-separated URL param into a Set, with validation
function parseSetParam<T extends string>(param: string | null, allValues: T[], defaultValues: T[]): Set<T> {
  if (!param) return new Set(defaultValues)
  const values = param.split(',').filter((v): v is T => allValues.includes(v as T))
  return values.length > 0 ? new Set(values) : new Set(defaultValues)
}

// Helper to serialize a Set to comma-separated string, returning undefined if it matches default
function serializeSetParam<T extends string>(set: Set<T>, defaultValues: T[]): string | undefined {
  const defaultSet = new Set(defaultValues)
  const isDefault = set.size === defaultSet.size && [...set].every(v => defaultSet.has(v))
  if (isDefault) return undefined
  return Array.from(set).join(',')
}

export function TimelinePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const searchParam = searchParams.get('search') || ''
  // Parse comma-separated filters
  const searchFilters = searchParam ? searchParam.split(',').map(f => f.trim()).filter(Boolean) : []

  // Initialize state from URL params
  const initialTimeRange = (searchParams.get('range') || '24h') as TimeRange | 'custom'
  const initialCategories = parseSetParam(searchParams.get('categories'), ALL_CATEGORIES, ALL_CATEGORIES)
  const initialEntityTypes = parseSetParam(searchParams.get('entities'), ALL_ENTITY_TYPES, DEFAULT_ENTITY_TYPES)
  const initialActions = parseSetParam(searchParams.get('actions'), ALL_ACTIONS, ALL_ACTIONS)
  const initialDzFilter = (searchParams.get('dz') || 'on_dz') as DZFilter
  const initialIncludeInternal = searchParams.get('internal') === 'true'
  const initialCustomStart = searchParams.get('start') || ''
  const initialCustomEnd = searchParams.get('end') || ''

  const [timeRange, setTimeRange] = useState<TimeRange | 'custom'>(initialTimeRange)
  const [selectedCategories, setSelectedCategories] = useState<Set<Category>>(initialCategories)
  const [selectedEntityTypes, setSelectedEntityTypes] = useState<Set<EntityType>>(initialEntityTypes)
  const [selectedActions, setSelectedActions] = useState<Set<ActionFilter>>(initialActions)
  const [dzFilter, setDzFilter] = useState<DZFilter>(initialDzFilter)
  const [includeInternal, setIncludeInternal] = useState(initialIncludeInternal)
  const limit = 50

  // Ref for infinite scroll sentinel
  const loadMoreRef = useRef<HTMLDivElement>(null)

  // Custom date range state
  const [customStart, setCustomStart] = useState<string>(initialCustomStart)
  const [customEnd, setCustomEnd] = useState<string>(initialCustomEnd)

  // Sync filter state to URL params
  const updateUrlParams = (updates: {
    range?: TimeRange | 'custom'
    categories?: Set<Category>
    entities?: Set<EntityType>
    actions?: Set<ActionFilter>
    dz?: DZFilter
    internal?: boolean
    start?: string
    end?: string
    search?: string | null
  }) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)

      // Time range
      if (updates.range !== undefined) {
        if (updates.range === '24h') {
          next.delete('range')
        } else {
          next.set('range', updates.range)
        }
      }

      // Categories
      if (updates.categories !== undefined) {
        const serialized = serializeSetParam(updates.categories, ALL_CATEGORIES)
        if (serialized) {
          next.set('categories', serialized)
        } else {
          next.delete('categories')
        }
      }

      // Entity types
      if (updates.entities !== undefined) {
        const serialized = serializeSetParam(updates.entities, DEFAULT_ENTITY_TYPES)
        if (serialized) {
          next.set('entities', serialized)
        } else {
          next.delete('entities')
        }
      }

      // Actions
      if (updates.actions !== undefined) {
        const serialized = serializeSetParam(updates.actions, ALL_ACTIONS)
        if (serialized) {
          next.set('actions', serialized)
        } else {
          next.delete('actions')
        }
      }

      // DZ filter
      if (updates.dz !== undefined) {
        if (updates.dz === 'on_dz') {
          next.delete('dz')
        } else {
          next.set('dz', updates.dz)
        }
      }

      // Include internal
      if (updates.internal !== undefined) {
        if (updates.internal) {
          next.set('internal', 'true')
        } else {
          next.delete('internal')
        }
      }

      // Custom date range
      if (updates.start !== undefined) {
        if (updates.start) {
          next.set('start', updates.start)
        } else {
          next.delete('start')
        }
      }
      if (updates.end !== undefined) {
        if (updates.end) {
          next.set('end', updates.end)
        } else {
          next.delete('end')
        }
      }

      // Search
      if (updates.search !== undefined) {
        if (updates.search) {
          next.set('search', updates.search)
        } else {
          next.delete('search')
        }
      }

      return next
    })
  }

  const clearSearchFilter = () => {
    updateUrlParams({ search: null })
  }

  const removeSearchFilter = (filterToRemove: string) => {
    const newFilters = searchFilters.filter(f => f !== filterToRemove)
    updateUrlParams({ search: newFilters.length > 0 ? newFilters.join(',') : null })
  }

  // Fetch timeline data bounds
  const { data: bounds } = useQuery({
    queryKey: ['timeline-bounds'],
    queryFn: fetchTimelineBounds,
    staleTime: 60_000, // Cache for 1 minute
  })

  // Track the most recent event timestamp to identify new events
  // Only track within this component's lifecycle (not persisted) so initial load never shows "new"
  const lastSeenTimestamp = useRef<string>('')
  const [newEventIds, setNewEventIds] = useState<Set<string>>(new Set())

  const categoryFilter = selectedCategories.size === ALL_CATEGORIES.length
    ? undefined
    : Array.from(selectedCategories).join(',')

  const entityTypeFilter = selectedEntityTypes.size === ALL_ENTITY_TYPES.length
    ? undefined
    : Array.from(selectedEntityTypes).join(',')

  const actionFilter = selectedActions.size === ALL_ACTIONS.length
    ? undefined
    : Array.from(selectedActions).join(',')

  // Only pass dz_filter if we have Solana entities selected
  const hasSolanaEntities = ALL_SOLANA_ENTITIES.some(e => selectedEntityTypes.has(e))
  const dzFilterParam = hasSolanaEntities && dzFilter !== 'all' ? dzFilter : undefined

  const {
    data,
    isLoading,
    error,
    refetch,
    isFetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: ['timeline', timeRange, customStart, customEnd, categoryFilter, entityTypeFilter, actionFilter, dzFilterParam, includeInternal, searchParam],
    queryFn: ({ pageParam = 0 }) => fetchTimeline({
      range: timeRange !== 'custom' ? timeRange : undefined,
      start: timeRange === 'custom' && customStart ? customStart : undefined,
      end: timeRange === 'custom' && customEnd ? customEnd : undefined,
      category: categoryFilter,
      entity_type: entityTypeFilter,
      action: actionFilter,
      dz_filter: dzFilterParam,
      search: searchParam || undefined,
      include_internal: includeInternal,
      limit,
      offset: pageParam,
    }),
    getNextPageParam: (lastPage, allPages) => {
      const totalLoaded = allPages.reduce((acc, page) => acc + page.events.length, 0)
      return totalLoaded < lastPage.total ? totalLoaded : undefined
    },
    initialPageParam: 0,
    refetchInterval: timeRange !== 'custom' ? 15_000 : undefined, // Only poll for relative ranges
    staleTime: timeRange === '24h' ? 10_000 : 0,
  })

  // Flatten all pages into a single list of events
  const allEvents = useMemo(() => {
    return data?.pages.flatMap(page => page.events) ?? []
  }, [data])

  // Total count and histogram from first page
  const totalCount = data?.pages[0]?.total ?? 0
  const histogram = data?.pages[0]?.histogram

  // Infinite scroll - observe the sentinel element
  useEffect(() => {
    const sentinel = loadMoreRef.current
    if (!sentinel) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage()
        }
      },
      { threshold: 0.1 }
    )

    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  // Track new events when data changes
  useEffect(() => {
    if (allEvents.length === 0) return

    const newIds = new Set<string>()
    const mostRecentTimestamp = allEvents[0]?.timestamp || ''

    // Only mark events as new if we have a stored timestamp (not first load)
    if (lastSeenTimestamp.current) {
      for (const event of allEvents) {
        // Event is "new" if it's newer than our last seen timestamp
        if (event.timestamp > lastSeenTimestamp.current) {
          newIds.add(event.id)
        }
      }
    }

    // Update the stored timestamp to the most recent event
    if (mostRecentTimestamp && mostRecentTimestamp > lastSeenTimestamp.current) {
      lastSeenTimestamp.current = mostRecentTimestamp
    }

    if (newIds.size > 0) {
      setNewEventIds(newIds)
      // Clear the "new" highlight after 5 seconds
      setTimeout(() => {
        setNewEventIds(new Set())
      }, 5000)
    }
  }, [allEvents])

  // Reset seen events when filters change
  const resetSeenEvents = () => {
    lastSeenTimestamp.current = ''
    setNewEventIds(new Set())
  }

  const resetAllFilters = () => {
    setTimeRange('24h')
    setSelectedCategories(new Set(ALL_CATEGORIES))
    setSelectedEntityTypes(new Set(DEFAULT_ENTITY_TYPES))
    setSelectedActions(new Set(ALL_ACTIONS))
    setDzFilter('on_dz')
    setIncludeInternal(false)
    setCustomStart('')
    setCustomEnd('')

    resetSeenEvents()

    // Clear all filter URL params
    setSearchParams(new URLSearchParams())
  }

  // Filter events by search queries (client-side)
  // Searches through all relevant fields including related entities in details
  // Matches if event matches ANY of the filters (OR logic)
  const filteredEvents = useMemo(() => {
    if (allEvents.length === 0 || searchFilters.length === 0) return allEvents

    const matchesSearch = (value: unknown, lowerSearch: string): boolean => {
      if (!value) return false
      if (typeof value === 'string') return value.toLowerCase().includes(lowerSearch)
      if (typeof value === 'number') return String(value).includes(lowerSearch)
      return false
    }

    const eventMatchesFilter = (event: TimelineEvent, filter: string): boolean => {
      const lowerSearch = filter.toLowerCase()

      // Check main event fields
      if (matchesSearch(event.entity_code, lowerSearch)) return true
      if (matchesSearch(event.title, lowerSearch)) return true
      if (matchesSearch(event.description, lowerSearch)) return true

      // Check event details for related entities
      const details = event.details
      if (!details) return false

      // EntityChangeDetails - check the entity object for related codes
      if ('entity' in details && details.entity) {
        const entity = details.entity as unknown as Record<string, unknown>
        if (matchesSearch(entity.device_code, lowerSearch)) return true
        if (matchesSearch(entity.metro_code, lowerSearch)) return true
        if (matchesSearch(entity.contributor_code, lowerSearch)) return true
        if (matchesSearch(entity.code, lowerSearch)) return true
        if (matchesSearch(entity.name, lowerSearch)) return true
        if (matchesSearch(entity.public_ip, lowerSearch)) return true
        if (matchesSearch(entity.client_ip, lowerSearch)) return true
        if (matchesSearch(entity.dz_ip, lowerSearch)) return true
        if (matchesSearch(entity.owner_pubkey, lowerSearch)) return true
        // LinkEntity - check side_a/side_z device and metro codes
        if (matchesSearch(entity.side_a_code, lowerSearch)) return true
        if (matchesSearch(entity.side_z_code, lowerSearch)) return true
        if (matchesSearch(entity.side_a_metro_code, lowerSearch)) return true
        if (matchesSearch(entity.side_z_metro_code, lowerSearch)) return true
      }

      // ValidatorEventDetails - device, metro, pubkeys
      if ('device_code' in details && matchesSearch(details.device_code, lowerSearch)) return true
      if ('metro_code' in details && matchesSearch(details.metro_code, lowerSearch)) return true
      if ('owner_pubkey' in details && matchesSearch(details.owner_pubkey, lowerSearch)) return true
      if ('vote_pubkey' in details && matchesSearch(details.vote_pubkey, lowerSearch)) return true
      if ('node_pubkey' in details && matchesSearch(details.node_pubkey, lowerSearch)) return true
      if ('dz_ip' in details && matchesSearch(details.dz_ip, lowerSearch)) return true

      // InterfaceEventDetails - interface and link
      if ('interface_name' in details && matchesSearch(details.interface_name, lowerSearch)) return true
      if ('link_code' in details && matchesSearch(details.link_code, lowerSearch)) return true

      // GroupedInterfaceDetails - search across all interfaces
      if ('interfaces' in details && Array.isArray(details.interfaces)) {
        for (const intf of details.interfaces) {
          if (matchesSearch(intf.interface_name, lowerSearch)) return true
          if (matchesSearch(intf.link_code, lowerSearch)) return true
        }
      }

      // PacketLossEventDetails - link and metros
      if ('side_a_metro' in details && matchesSearch(details.side_a_metro, lowerSearch)) return true
      if ('side_z_metro' in details && matchesSearch(details.side_z_metro, lowerSearch)) return true

      return false
    }

    // Event matches if it matches ANY of the filters (OR logic)
    return allEvents.filter(event =>
      searchFilters.some(filter => eventMatchesFilter(event, filter))
    )
  }, [allEvents, searchFilters])

  // Check if any filters are non-default
  // Check if filters differ from defaults
  const hasActiveFilters = timeRange !== '24h' ||
    selectedCategories.size !== ALL_CATEGORIES.length ||
    selectedEntityTypes.size !== DEFAULT_ENTITY_TYPES.length ||
    !DEFAULT_ENTITY_TYPES.every(e => selectedEntityTypes.has(e)) ||
    selectedActions.size !== ALL_ACTIONS.length ||
    dzFilter !== 'on_dz' ||
    includeInternal ||
    searchFilters.length > 0

  const handleBucketClick = (bucket: HistogramBucket, nextBucket?: HistogramBucket) => {
    const start = new Date(bucket.timestamp)
    // Use next bucket's timestamp as end, or add estimated bucket duration
    let end: Date
    if (nextBucket) {
      end = new Date(nextBucket.timestamp)
    } else {
      // Estimate bucket duration from first two buckets
      if (histogram && histogram.length > 1) {
        const bucketDuration = new Date(histogram[1].timestamp).getTime() - new Date(histogram[0].timestamp).getTime()
        end = new Date(start.getTime() + bucketDuration)
      } else {
        end = new Date(start.getTime() + 30 * 60 * 1000) // Default 30 min
      }
    }
    const startStr = start.toISOString().slice(0, 16)
    const endStr = end.toISOString().slice(0, 16)
    setTimeRange('custom')
    setCustomStart(startStr)
    setCustomEnd(endStr)
    updateUrlParams({ range: 'custom', start: startStr, end: endStr })
    resetSeenEvents()
  }

  const toggleCategory = (category: Category) => {
    const next = new Set(selectedCategories)
    if (next.has(category)) {
      next.delete(category)
    } else {
      next.add(category)
    }
    setSelectedCategories(next)
    updateUrlParams({ categories: next })
    resetSeenEvents()
  }

  const toggleEntityType = (entityType: EntityType) => {
    const next = new Set(selectedEntityTypes)
    if (next.has(entityType)) {
      next.delete(entityType)
    } else {
      next.add(entityType)
    }
    setSelectedEntityTypes(next)
    updateUrlParams({ entities: next })
    resetSeenEvents()
  }

  const toggleAction = (action: ActionFilter) => {
    const next = new Set(selectedActions)
    if (next.has(action)) {
      next.delete(action)
    } else {
      next.add(action)
    }
    setSelectedActions(next)
    updateUrlParams({ actions: next })
    resetSeenEvents()
  }

  const handleTimeRangeChange = (range: TimeRange | 'custom') => {
    setTimeRange(range)
    if (range === 'custom' && bounds) {
      // Default to last 24 hours within bounds
      const end = new Date()
      const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
      const earliest = new Date(bounds.earliest_data)
      if (start < earliest) {
        start.setTime(earliest.getTime())
      }
      const startStr = start.toISOString().slice(0, 16)
      const endStr = end.toISOString().slice(0, 16)
      setCustomStart(startStr)
      setCustomEnd(endStr)
      updateUrlParams({ range, start: startStr, end: endStr })
    } else {
      // Clear custom range params when switching to relative range
      updateUrlParams({ range, start: '', end: '' })
    }
    resetSeenEvents()
  }

  // Get min/max dates for date picker based on bounds
  const minDate = bounds ? new Date(bounds.earliest_data).toISOString().slice(0, 16) : undefined
  const maxDate = new Date().toISOString().slice(0, 16)

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="mb-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Clock className="h-6 w-6 text-muted-foreground" />
              <h1 className="text-2xl font-semibold">Timeline</h1>
              {totalCount > 0 && (
                <span className="text-sm text-muted-foreground bg-muted px-2 py-0.5 rounded">
                  {searchFilters.length > 0 ? `${filteredEvents.length} of ${totalCount.toLocaleString()}` : totalCount.toLocaleString()} events
                </span>
              )}
              {searchFilters.length > 0 && (
                <div className="flex items-center gap-1.5 flex-wrap">
                  {searchFilters.map((filter, idx) => (
                    <button
                      key={idx}
                      onClick={() => removeSearchFilter(filter)}
                      className="inline-flex items-center gap-1 text-sm px-2 py-0.5 rounded bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20 hover:bg-blue-500/20 transition-colors"
                    >
                      <Search className="h-3 w-3" />
                      {filter}
                      <X className="h-3 w-3" />
                    </button>
                  ))}
                  {searchFilters.length > 1 && (
                    <button
                      onClick={clearSearchFilter}
                      className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                    >
                      Clear all
                    </button>
                  )}
                </div>
              )}
              {newEventIds.size > 0 && (
                <span className="text-xs px-2 py-0.5 rounded-full bg-primary text-primary-foreground animate-pulse">
                  +{newEventIds.size} new
                </span>
              )}
            </div>
            <div className="flex items-center gap-3">
              {isFetching && !isLoading && (
                <span className="text-xs text-muted-foreground">Updating...</span>
              )}
              <button
                onClick={() => refetch()}
                disabled={isFetching}
                className="text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
                title="Refresh"
              >
                <RefreshCw className={cn('h-4 w-4', isFetching && 'animate-spin')} />
              </button>
            </div>
          </div>
          <p className="text-muted-foreground mt-1">
            Events across the DoubleZero network
          </p>
        </div>

        {/* Filters toolbar */}
        <div className="rounded-lg border border-border bg-muted/20 p-3 mb-6 space-y-3">
          {/* Row 1: Time range + Search */}
          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              <div className="inline-flex rounded-md border border-border bg-background p-0.5">
                {timeRangeOptions.map(option => (
                  <button
                    key={option.value}
                    onClick={() => handleTimeRangeChange(option.value)}
                    className={cn(
                      'px-2.5 py-1 text-sm rounded transition-colors',
                      timeRange === option.value
                        ? 'bg-primary text-primary-foreground'
                        : 'text-muted-foreground hover:text-foreground'
                    )}
                  >
                    {option.label}
                  </button>
                ))}
              </div>

              {/* Custom date range picker */}
              {timeRange === 'custom' && (
                <div className="inline-flex items-center gap-2">
                  <Calendar className="h-4 w-4 text-muted-foreground" />
                  <input
                    type="datetime-local"
                    value={customStart}
                    min={minDate}
                    max={customEnd || maxDate}
                    onChange={(e) => {
                      setCustomStart(e.target.value)
                      updateUrlParams({ start: e.target.value })
                      resetSeenEvents()
                    }}
                    className="px-2 py-1 text-sm border border-border rounded-md bg-background"
                  />
                  <span className="text-muted-foreground">to</span>
                  <input
                    type="datetime-local"
                    value={customEnd}
                    min={customStart || minDate}
                    max={maxDate}
                    onChange={(e) => {
                      setCustomEnd(e.target.value)
                      updateUrlParams({ end: e.target.value })
                      resetSeenEvents()
                    }}
                    className="px-2 py-1 text-sm border border-border rounded-md bg-background"
                  />
                </div>
              )}
            </div>

            <button
              onClick={() => window.dispatchEvent(new CustomEvent('open-search'))}
              className="flex items-center gap-1.5 px-2.5 py-1 text-sm text-muted-foreground hover:text-foreground border border-border rounded-md bg-background hover:bg-muted transition-colors"
              title="Search (Cmd+K)"
            >
              <Search className="h-3.5 w-3.5" />
              <span>Search</span>
              <kbd className="ml-1 font-mono text-[10px] text-muted-foreground">⌘K</kbd>
            </button>
          </div>

          {/* Divider */}
          <div className="border-t border-border" />

          {/* Row 2: Filters */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <FilterDropdown
              label="Event Type"
              options={categoryOptions}
              selected={selectedCategories}
              onToggle={toggleCategory}
              allValues={ALL_CATEGORIES}
            />

            <FilterDropdown
              label="Action"
              options={actionOptions}
              selected={selectedActions}
              onToggle={toggleAction}
              allValues={ALL_ACTIONS}
            />

            <div className="h-4 w-px bg-border" />

            <FilterDropdown
              label="DoubleZero"
              options={dzEntityOptions}
              selected={selectedEntityTypes}
              onToggle={toggleEntityType}
              allValues={ALL_DZ_ENTITIES}
            />

            <FilterDropdown
              label="Solana"
              options={solanaEntityOptions}
              selected={selectedEntityTypes}
              onToggle={toggleEntityType}
              allValues={ALL_SOLANA_ENTITIES}
            />

            {/* DZ status for Solana entities */}
            {hasSolanaEntities && (
              <div className="inline-flex rounded-md border border-border bg-background p-0.5 gap-0.5">
                {dzFilterOptions.map(option => (
                  <button
                    key={option.value}
                    onClick={() => {
                      setDzFilter(option.value)
                      updateUrlParams({ dz: option.value })
                      resetSeenEvents()
                    }}
                    className={cn(
                      'px-2 py-0.5 text-xs rounded transition-colors',
                      dzFilter === option.value
                        ? 'bg-primary text-primary-foreground'
                        : 'text-muted-foreground hover:text-foreground'
                    )}
                  >
                    {option.label}
                  </button>
                ))}
              </div>
            )}

            {/* Spacer to push secondary options right */}
            <div className="flex-1" />

            {/* Internal users toggle */}
            <button
              onClick={() => {
                const newValue = !includeInternal
                setIncludeInternal(newValue)
                updateUrlParams({ internal: newValue })
                resetSeenEvents()
              }}
              className="inline-flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <span>Internal users</span>
              <div className={cn(
                'relative w-7 h-4 rounded-full transition-colors',
                includeInternal ? 'bg-primary' : 'bg-muted-foreground/30'
              )}>
                <div className={cn(
                  'absolute top-0.5 w-3 h-3 rounded-full bg-white shadow transition-transform',
                  includeInternal ? 'translate-x-3.5' : 'translate-x-0.5'
                )} />
              </div>
            </button>

            <button
              onClick={resetAllFilters}
              disabled={!hasActiveFilters}
              className={cn(
                'inline-flex items-center gap-1 text-xs transition-colors',
                hasActiveFilters
                  ? 'text-muted-foreground hover:text-foreground cursor-pointer'
                  : 'text-muted-foreground/40 cursor-not-allowed'
              )}
            >
              <RotateCw className="h-3 w-3" />
              Reset
            </button>
          </div>
        </div>

        {/* Loading state */}
        {isLoading && (
          <div className="space-y-3">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="border border-border border-l-4 border-l-muted rounded-lg p-4">
                <div className="flex items-start gap-3">
                  <div className="flex flex-col items-center gap-1">
                    <Skeleton className="h-4 w-4 rounded" />
                    <Skeleton className="h-3 w-3 rounded" />
                  </div>
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-2">
                      <Skeleton className="h-4 w-24" />
                      <Skeleton className="h-5 w-16 rounded" />
                      <Skeleton className="h-4 w-16" />
                    </div>
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-3 w-1/2 mt-2" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Error state */}
        {error && (
          <div className="text-center py-12">
            <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-3" />
            <div className="text-sm text-muted-foreground">
              {error instanceof Error ? error.message : 'Failed to load timeline'}
            </div>
          </div>
        )}

        {/* Empty state */}
        {data && filteredEvents.length === 0 && (
          <div className="text-center py-12 border border-dashed border-border rounded-lg">
            <Clock className="h-12 w-12 text-muted-foreground/50 mx-auto mb-3" />
            <div className="text-sm text-muted-foreground">
              {searchFilters.length > 0
                ? `No events matching filters: ${searchFilters.join(', ')}`
                : 'No events found in the selected time range'}
            </div>
            {searchFilters.length > 0 && (
              <button
                onClick={clearSearchFilter}
                className="mt-2 text-sm text-blue-500 hover:underline"
              >
                Clear search filters
              </button>
            )}
          </div>
        )}

        {/* Histogram */}
        {histogram && histogram.length > 0 && (
          <EventHistogram data={histogram} onBucketClick={handleBucketClick} />
        )}

        {/* Event list */}
        {data && filteredEvents.length > 0 && (
          <>
            <div className="space-y-3">
              {filteredEvents.map(event => (
                <TimelineEventCard key={event.id} event={event} isNew={newEventIds.has(event.id)} />
              ))}
            </div>

            {/* Infinite scroll sentinel */}
            {searchFilters.length === 0 && (
              <div ref={loadMoreRef} className="py-8 flex justify-center">
                {isFetchingNextPage ? (
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <RefreshCw className="h-4 w-4 animate-spin" />
                    Loading more...
                  </div>
                ) : hasNextPage ? (
                  <div className="text-sm text-muted-foreground">Scroll for more</div>
                ) : allEvents.length > limit ? (
                  <div className="text-sm text-muted-foreground">All {totalCount.toLocaleString()} events loaded</div>
                ) : null}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
