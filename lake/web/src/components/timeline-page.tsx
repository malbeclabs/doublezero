import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
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
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Pagination } from '@/components/pagination'
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

const dzFilterOptions: { value: DZFilter; label: string }[] = [
  { value: 'on_dz', label: 'On DZ' },
  { value: 'off_dz', label: 'Off DZ' },
  { value: 'all', label: 'All' },
]

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse bg-muted rounded ${className || ''}`} />
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

function getEntityLink(entityType: string, entityPK: string): string {
  switch (entityType) {
    case 'device':
      return `/dz/devices/${encodeURIComponent(entityPK)}`
    case 'link':
      return `/dz/links/${encodeURIComponent(entityPK)}`
    case 'metro':
      return `/dz/metros/${encodeURIComponent(entityPK)}`
    case 'contributor':
      return `/dz/contributors/${encodeURIComponent(entityPK)}`
    case 'user':
      return `/dz/users/${encodeURIComponent(entityPK)}`
    case 'validator':
      return `/solana/validators/${encodeURIComponent(entityPK)}`
    case 'gossip_node':
      return `/solana/gossip-nodes/${encodeURIComponent(entityPK)}`
    default:
      return '#'
  }
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
        <div>Metros: <span className="font-medium text-foreground">{d.side_a_metro_code} → {d.side_z_metro_code}</span></div>
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
        {d.metro_code && <div>Metro: <span className="font-medium text-foreground">{d.metro_code}</span></div>}
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

  if (event.event_type.startsWith('packet_loss') && 'current_loss_pct' in details) {
    const d = details as PacketLossEventDetails
    return (
      <div className="text-xs text-muted-foreground mt-2 space-y-1">
        <div>Link: {d.link_pk ? <EntityLink to={`/dz/links/${encodeURIComponent(d.link_pk)}`}>{d.link_code}</EntityLink> : <span className="font-medium">{d.link_code}</span>} ({d.link_type})</div>
        <div>Route: {d.side_a_metro} → {d.side_z_metro}</div>
        <div>
          Loss: <span className={d.direction === 'increased' ? 'text-red-600' : 'text-green-600'}>
            {d.previous_loss_pct.toFixed(2)}% → {d.current_loss_pct.toFixed(2)}%
          </span>
        </div>
      </div>
    )
  }

  if (event.event_type.startsWith('interface_') && 'interface_name' in details) {
    const d = details as InterfaceEventDetails
    return (
      <div className="text-xs text-muted-foreground mt-2 space-y-1">
        <div>Interface: <span className="font-medium">{d.interface_name}</span></div>
        {d.link_pk && d.link_code && <div>Link: <EntityLink to={`/dz/links/${encodeURIComponent(d.link_pk)}`}>{d.link_code}</EntityLink></div>}
        {(d.in_errors || d.out_errors) && (d.in_errors! > 0 || d.out_errors! > 0) && (
          <div>Errors: {d.in_errors} in / {d.out_errors} out</div>
        )}
        {(d.in_discards || d.out_discards) && (d.in_discards! > 0 || d.out_discards! > 0) && (
          <div>Discards: {d.in_discards} in / {d.out_discards} out</div>
        )}
        {d.carrier_transitions && d.carrier_transitions > 0 && (
          <div>Carrier transitions: {d.carrier_transitions}</div>
        )}
      </div>
    )
  }

  if ((event.event_type.includes('validator') || event.event_type.includes('gossip_node')) && 'action' in details) {
    const d = details as ValidatorEventDetails
    const isValidator = d.kind === 'validator'
    return (
      <div className="text-xs text-muted-foreground mt-2 space-y-1">
        <div>Owner: <span className="font-mono break-all">{d.owner_pubkey}</span></div>
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
        {d.metro_code && <div>Metro: <span className="font-medium text-foreground">{d.metro_code}</span></div>}
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
  const hasDetails = event.details && Object.keys(event.details).length > 0

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
            <Link
              to={getEntityLink(event.entity_type, event.entity_pk)}
              className={cn(
                "font-medium hover:underline text-sm",
                // For pubkeys (long strings), truncate on mobile but show full on desktop
                event.entity_code.length > 20 && "font-mono max-w-[120px] sm:max-w-[200px] md:max-w-none truncate"
              )}
              title={event.entity_code}
            >
              {event.entity_code}
            </Link>
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

          {/* Show device connection for validator/gossip/user events */}
          {validatorDetails?.device_pk && validatorDetails?.device_code && (
            <div className="text-xs text-muted-foreground mt-1">
              Connected to <Link to={`/dz/devices/${encodeURIComponent(validatorDetails.device_pk)}`} className="font-medium text-foreground hover:underline">{validatorDetails.device_code}</Link>
            </div>
          )}
          {userEntity?.device_pk && userEntity?.device_code && (
            <div className="text-xs text-muted-foreground mt-1">
              Connected to <Link to={`/dz/devices/${encodeURIComponent(userEntity.device_pk)}`} className="font-medium text-foreground hover:underline">{userEntity.device_code}</Link>
              {userEntity.metro_code && <span className="text-muted-foreground"> in {userEntity.metro_code}</span>}
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

export function TimelinePage() {
  const [timeRange, setTimeRange] = useState<TimeRange | 'custom'>('24h')
  const [selectedCategories, setSelectedCategories] = useState<Set<Category>>(new Set(ALL_CATEGORIES))
  const [selectedEntityTypes, setSelectedEntityTypes] = useState<Set<EntityType>>(new Set(ALL_ENTITY_TYPES))
  const [selectedActions, setSelectedActions] = useState<Set<ActionFilter>>(new Set(ALL_ACTIONS))
  const [dzFilter, setDzFilter] = useState<DZFilter>('on_dz')
  const [includeInternal, setIncludeInternal] = useState(false)
  const [offset, setOffset] = useState(0)
  const limit = 50

  // Custom date range state
  const [customStart, setCustomStart] = useState<string>('')
  const [customEnd, setCustomEnd] = useState<string>('')

  // Fetch timeline data bounds
  const { data: bounds } = useQuery({
    queryKey: ['timeline-bounds'],
    queryFn: fetchTimelineBounds,
    staleTime: 60_000, // Cache for 1 minute
  })

  // Track the most recent event timestamp to identify new events
  // Use sessionStorage to persist across navigation within the same session
  const getStoredTimestamp = () => {
    try {
      return sessionStorage.getItem('timeline-last-seen-ts') || ''
    } catch {
      return ''
    }
  }
  const lastSeenTimestamp = useRef<string>(getStoredTimestamp())
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

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['timeline', timeRange, customStart, customEnd, categoryFilter, entityTypeFilter, actionFilter, dzFilterParam, includeInternal, offset],
    queryFn: () => fetchTimeline({
      range: timeRange !== 'custom' ? timeRange : undefined,
      start: timeRange === 'custom' && customStart ? customStart : undefined,
      end: timeRange === 'custom' && customEnd ? customEnd : undefined,
      category: categoryFilter,
      entity_type: entityTypeFilter,
      action: actionFilter,
      dz_filter: dzFilterParam,
      include_internal: includeInternal,
      limit,
      offset,
    }),
    refetchInterval: timeRange !== 'custom' ? 15_000 : undefined, // Only poll for relative ranges
    staleTime: 10_000,
  })

  // Track new events when data changes
  useEffect(() => {
    if (!data?.events || data.events.length === 0 || offset !== 0) return

    const newIds = new Set<string>()
    const mostRecentTimestamp = data.events[0]?.timestamp || ''

    // Only mark events as new if we have a stored timestamp (not first load)
    if (lastSeenTimestamp.current) {
      for (const event of data.events) {
        // Event is "new" if it's newer than our last seen timestamp
        if (event.timestamp > lastSeenTimestamp.current) {
          newIds.add(event.id)
        }
      }
    }

    // Update the stored timestamp to the most recent event
    if (mostRecentTimestamp && mostRecentTimestamp > lastSeenTimestamp.current) {
      lastSeenTimestamp.current = mostRecentTimestamp
      try {
        sessionStorage.setItem('timeline-last-seen-ts', mostRecentTimestamp)
      } catch {
        // Ignore storage errors
      }
    }

    if (newIds.size > 0) {
      setNewEventIds(newIds)
      // Clear the "new" highlight after 5 seconds
      setTimeout(() => {
        setNewEventIds(new Set())
      }, 5000)
    }
  }, [data, offset])

  // Reset seen events when filters change
  const resetSeenEvents = () => {
    lastSeenTimestamp.current = ''
    setNewEventIds(new Set())
    try {
      sessionStorage.removeItem('timeline-last-seen-ts')
    } catch {
      // Ignore storage errors
    }
  }

  const resetAllFilters = () => {
    setTimeRange('24h')
    setSelectedCategories(new Set(ALL_CATEGORIES))
    setSelectedEntityTypes(new Set(ALL_ENTITY_TYPES))
    setSelectedActions(new Set(ALL_ACTIONS))
    setDzFilter('on_dz')
    setIncludeInternal(false)
    setCustomStart('')
    setCustomEnd('')
    setOffset(0)
    resetSeenEvents()
  }

  // Check if any filters are non-default
  const hasActiveFilters = timeRange !== '24h' ||
    selectedCategories.size !== ALL_CATEGORIES.length ||
    selectedEntityTypes.size !== ALL_ENTITY_TYPES.length ||
    selectedActions.size !== ALL_ACTIONS.length ||
    dzFilter !== 'on_dz' ||
    includeInternal

  const handleBucketClick = (bucket: HistogramBucket, nextBucket?: HistogramBucket) => {
    const start = new Date(bucket.timestamp)
    // Use next bucket's timestamp as end, or add estimated bucket duration
    let end: Date
    if (nextBucket) {
      end = new Date(nextBucket.timestamp)
    } else {
      // Estimate bucket duration from first two buckets
      if (data?.histogram && data.histogram.length > 1) {
        const bucketDuration = new Date(data.histogram[1].timestamp).getTime() - new Date(data.histogram[0].timestamp).getTime()
        end = new Date(start.getTime() + bucketDuration)
      } else {
        end = new Date(start.getTime() + 30 * 60 * 1000) // Default 30 min
      }
    }
    setTimeRange('custom')
    setCustomStart(start.toISOString().slice(0, 16))
    setCustomEnd(end.toISOString().slice(0, 16))
    setOffset(0)
    resetSeenEvents()
  }

  const toggleCategory = (category: Category) => {
    setSelectedCategories(prev => {
      const next = new Set(prev)
      if (next.has(category)) {
        next.delete(category)
      } else {
        next.add(category)
      }
      return next
    })
    setOffset(0)
    resetSeenEvents()
  }

  const toggleEntityType = (entityType: EntityType) => {
    setSelectedEntityTypes(prev => {
      const next = new Set(prev)
      if (next.has(entityType)) {
        next.delete(entityType)
      } else {
        next.add(entityType)
      }
      return next
    })
    setOffset(0)
    resetSeenEvents()
  }

  const toggleAction = (action: ActionFilter) => {
    setSelectedActions(prev => {
      const next = new Set(prev)
      if (next.has(action)) {
        next.delete(action)
      } else {
        next.add(action)
      }
      return next
    })
    setOffset(0)
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
      setCustomStart(start.toISOString().slice(0, 16))
      setCustomEnd(end.toISOString().slice(0, 16))
    }
    setOffset(0)
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
              {data && (
                <span className="text-sm text-muted-foreground bg-muted px-2 py-0.5 rounded">
                  {data.total.toLocaleString()} events
                </span>
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

        {/* Filters */}
        <div className="flex flex-wrap items-center gap-4 mb-6">
          {/* Time range */}
          <div className="inline-flex items-center gap-2">
            <div className="inline-flex rounded-lg border border-border bg-muted/30 p-0.5">
              {timeRangeOptions.map(option => (
                <button
                  key={option.value}
                  onClick={() => handleTimeRangeChange(option.value)}
                  className={cn(
                    'px-3 py-1 text-sm rounded-md transition-colors',
                    timeRange === option.value
                      ? 'bg-background text-foreground shadow-sm border border-border'
                      : 'text-muted-foreground hover:text-foreground border border-transparent'
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
                    setOffset(0)
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
                    setOffset(0)
                    resetSeenEvents()
                  }}
                  className="px-2 py-1 text-sm border border-border rounded-md bg-background"
                />
              </div>
            )}
          </div>

          {/* Event type filters */}
          <div className="inline-flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Type:</span>
            <div className="inline-flex rounded-lg border border-border bg-muted/30 p-0.5 gap-0.5">
              <button
                onClick={() => { setSelectedCategories(new Set(ALL_CATEGORIES)); setOffset(0); resetSeenEvents() }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  selectedCategories.size === ALL_CATEGORIES.length
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                All
              </button>
              <button
                onClick={() => { setSelectedCategories(new Set()); setOffset(0); resetSeenEvents() }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  selectedCategories.size === 0
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                None
              </button>
              {categoryOptions.map(option => {
                const Icon = option.icon
                const isSelected = selectedCategories.has(option.value)
                return (
                  <button
                    key={option.value}
                    onClick={() => toggleCategory(option.value)}
                    className={cn(
                      'flex items-center gap-1 px-2 py-1 text-xs rounded-md transition-colors',
                      isSelected
                        ? 'bg-background text-foreground shadow-sm border border-border'
                        : 'text-muted-foreground hover:text-foreground border border-transparent'
                    )}
                  >
                    <Icon className="h-3 w-3" />
                    {option.label}
                  </button>
                )
              })}
            </div>
          </div>

          {/* DZ Infrastructure entity filters */}
          <div className="inline-flex items-center gap-2">
            <span className="text-sm text-muted-foreground">DZ:</span>
            <div className="inline-flex rounded-lg border border-border bg-muted/30 p-0.5 gap-0.5">
              <button
                onClick={() => {
                  setSelectedEntityTypes(prev => {
                    const next = new Set(prev)
                    ALL_DZ_ENTITIES.forEach(e => next.add(e))
                    return next
                  })
                  setOffset(0)
                  resetSeenEvents()
                }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  ALL_DZ_ENTITIES.every(e => selectedEntityTypes.has(e))
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                All
              </button>
              <button
                onClick={() => {
                  setSelectedEntityTypes(prev => {
                    const next = new Set(prev)
                    ALL_DZ_ENTITIES.forEach(e => next.delete(e))
                    return next
                  })
                  setOffset(0)
                  resetSeenEvents()
                }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  ALL_DZ_ENTITIES.every(e => !selectedEntityTypes.has(e))
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                None
              </button>
              {dzEntityOptions.map(option => {
                const Icon = option.icon
                const isSelected = selectedEntityTypes.has(option.value)
                return (
                  <button
                    key={option.value}
                    onClick={() => toggleEntityType(option.value)}
                    className={cn(
                      'flex items-center gap-1 px-2 py-1 text-xs rounded-md transition-colors',
                      isSelected
                        ? 'bg-background text-foreground shadow-sm border border-border'
                        : 'text-muted-foreground hover:text-foreground border border-transparent'
                    )}
                  >
                    <Icon className="h-3 w-3" />
                    {option.label}
                  </button>
                )
              })}
            </div>
          </div>

          {/* Solana entity filters */}
          <div className="inline-flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Solana:</span>
            <div className="inline-flex rounded-lg border border-border bg-muted/30 p-0.5 gap-0.5">
              <button
                onClick={() => {
                  setSelectedEntityTypes(prev => {
                    const next = new Set(prev)
                    ALL_SOLANA_ENTITIES.forEach(e => next.add(e))
                    return next
                  })
                  setOffset(0)
                  resetSeenEvents()
                }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  ALL_SOLANA_ENTITIES.every(e => selectedEntityTypes.has(e))
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                All
              </button>
              <button
                onClick={() => {
                  setSelectedEntityTypes(prev => {
                    const next = new Set(prev)
                    ALL_SOLANA_ENTITIES.forEach(e => next.delete(e))
                    return next
                  })
                  setOffset(0)
                  resetSeenEvents()
                }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  ALL_SOLANA_ENTITIES.every(e => !selectedEntityTypes.has(e))
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                None
              </button>
              {solanaEntityOptions.map(option => {
                const Icon = option.icon
                const isSelected = selectedEntityTypes.has(option.value)
                return (
                  <button
                    key={option.value}
                    onClick={() => toggleEntityType(option.value)}
                    className={cn(
                      'flex items-center gap-1 px-2 py-1 text-xs rounded-md transition-colors',
                      isSelected
                        ? 'bg-background text-foreground shadow-sm border border-border'
                        : 'text-muted-foreground hover:text-foreground border border-transparent'
                    )}
                  >
                    <Icon className="h-3 w-3" />
                    {option.label}
                  </button>
                )
              })}
            </div>
            {/* DZ filter for Solana entities */}
            {hasSolanaEntities && (
              <div className="inline-flex rounded-lg border border-border bg-muted/30 p-0.5 gap-0.5">
                {dzFilterOptions.map(option => (
                  <button
                    key={option.value}
                    onClick={() => {
                      setDzFilter(option.value)
                      setOffset(0)
                      resetSeenEvents()
                    }}
                    className={cn(
                      'px-2 py-1 text-xs rounded-md transition-colors',
                      dzFilter === option.value
                        ? 'bg-background text-foreground shadow-sm border border-border'
                        : 'text-muted-foreground hover:text-foreground border border-transparent'
                    )}
                  >
                    {option.label}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Action filters */}
          <div className="inline-flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Action:</span>
            <div className="inline-flex rounded-lg border border-border bg-muted/30 p-0.5 gap-0.5">
              <button
                onClick={() => { setSelectedActions(new Set(ALL_ACTIONS)); setOffset(0); resetSeenEvents() }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  selectedActions.size === ALL_ACTIONS.length
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                All
              </button>
              <button
                onClick={() => { setSelectedActions(new Set()); setOffset(0); resetSeenEvents() }}
                className={cn(
                  'px-2 py-1 text-xs rounded-md transition-colors',
                  selectedActions.size === 0
                    ? 'bg-background text-foreground shadow-sm border border-border'
                    : 'text-muted-foreground hover:text-foreground border border-transparent'
                )}
              >
                None
              </button>
              {actionOptions.map(option => {
                const Icon = option.icon
                const isSelected = selectedActions.has(option.value)
                return (
                  <button
                    key={option.value}
                    onClick={() => toggleAction(option.value)}
                    className={cn(
                      'flex items-center gap-1 px-2 py-1 text-xs rounded-md transition-colors',
                      isSelected
                        ? 'bg-background text-foreground shadow-sm border border-border'
                        : 'text-muted-foreground hover:text-foreground border border-transparent'
                    )}
                  >
                    <Icon className="h-3 w-3" />
                    {option.label}
                  </button>
                )
              })}
            </div>
          </div>

          {/* Internal users toggle */}
          <label className="inline-flex items-center gap-2 text-sm text-muted-foreground cursor-pointer">
            <input
              type="checkbox"
              checked={includeInternal}
              onChange={(e) => {
                setIncludeInternal(e.target.checked)
                setOffset(0)
              }}
              className="rounded border-border"
            />
            Show internal users
          </label>

          {/* Reset filters */}
          {hasActiveFilters && (
            <button
              onClick={resetAllFilters}
              className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <RotateCw className="h-3 w-3" />
              Reset filters
            </button>
          )}
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
        {data && data.events.length === 0 && (
          <div className="text-center py-12 border border-dashed border-border rounded-lg">
            <Clock className="h-12 w-12 text-muted-foreground/50 mx-auto mb-3" />
            <div className="text-sm text-muted-foreground">
              No events found in the selected time range
            </div>
          </div>
        )}

        {/* Histogram */}
        {data?.histogram && data.histogram.length > 0 && (
          <EventHistogram data={data.histogram} onBucketClick={handleBucketClick} />
        )}

        {/* Event list */}
        {data && data.events.length > 0 && (
          <>
            <div className="space-y-3">
              {data.events.map(event => (
                <TimelineEventCard key={event.id} event={event} isNew={newEventIds.has(event.id)} />
              ))}
            </div>

            {/* Pagination */}
            {data.total > limit && (
              <div className="mt-6">
                <Pagination
                  total={data.total}
                  limit={limit}
                  offset={offset}
                  onOffsetChange={setOffset}
                />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
