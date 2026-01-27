import { useEffect, useRef, useState } from 'react'
import type { SelectionType } from './TopologyContext'
import type { DeviceInfo, LinkInfo, MetroInfo, ValidatorInfo } from './types'
import { formatBandwidth, formatStake } from './utils'

// Hover popover data (simplified for quick display)
export interface HoverData {
  type: SelectionType
  data: DeviceInfo | LinkInfo | MetroInfo | ValidatorInfo
}

interface TopologyHoverPopoverProps {
  hoverData: HoverData | null
  position: { x: number; y: number } | null
  offset?: { x: number; y: number }
}

export function TopologyHoverPopover({
  hoverData,
  position,
  offset = { x: 12, y: 12 },
}: TopologyHoverPopoverProps) {
  const popoverRef = useRef<HTMLDivElement>(null)
  const [adjustedPosition, setAdjustedPosition] = useState<{ x: number; y: number } | null>(null)

  // Adjust position to keep popover in viewport
  useEffect(() => {
    if (!position || !popoverRef.current) {
      setAdjustedPosition(null)
      return
    }

    const popover = popoverRef.current
    const rect = popover.getBoundingClientRect()
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight

    let x = position.x + offset.x
    let y = position.y + offset.y

    // Flip to left if going off right edge
    if (x + rect.width > viewportWidth - 16) {
      x = position.x - rect.width - offset.x
    }

    // Flip to top if going off bottom edge
    if (y + rect.height > viewportHeight - 16) {
      y = position.y - rect.height - offset.y
    }

    // Clamp to viewport
    x = Math.max(16, Math.min(x, viewportWidth - rect.width - 16))
    y = Math.max(16, Math.min(y, viewportHeight - rect.height - 16))

    setAdjustedPosition({ x, y })
  }, [position, offset])

  if (!hoverData || !position) return null

  const finalPosition = adjustedPosition || { x: position.x + offset.x, y: position.y + offset.y }

  return (
    <div
      ref={popoverRef}
      className="fixed z-[2000] pointer-events-none bg-[var(--card)] border border-[var(--border)] rounded-md shadow-lg px-3 py-2 text-xs"
      style={{
        left: finalPosition.x,
        top: finalPosition.y,
        maxWidth: 280,
      }}
    >
      {hoverData.type === 'device' && <DeviceHoverContent device={hoverData.data as DeviceInfo} />}
      {hoverData.type === 'link' && <LinkHoverContent link={hoverData.data as LinkInfo} />}
      {hoverData.type === 'metro' && <MetroHoverContent metro={hoverData.data as MetroInfo} />}
      {hoverData.type === 'validator' && <ValidatorHoverContent validator={hoverData.data as ValidatorInfo} />}
    </div>
  )
}

function DeviceHoverContent({ device }: { device: DeviceInfo }) {
  const stakeDisplay = device.stakeSol > 0 ? formatStake(device.stakeSol) : null
  return (
    <div className="space-y-1">
      <div className="font-medium">{device.code}</div>
      <div className="text-muted-foreground space-y-0.5">
        <div className="flex justify-between gap-4">
          <span>Type:</span>
          <span className="text-foreground">{device.deviceType}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span>Metro:</span>
          <span className="text-foreground">{device.metroName}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span>Status:</span>
          <span className={device.status === 'active' || device.status === 'activated' ? 'text-green-500' : 'text-red-500'}>
            {device.status}
          </span>
        </div>
        {device.validatorCount > 0 && (
          <div className="flex justify-between gap-4">
            <span>Validators:</span>
            <span className="text-foreground">{device.validatorCount}</span>
          </div>
        )}
        {stakeDisplay && (
          <div className="flex justify-between gap-4">
            <span>Stake:</span>
            <span className="text-amber-500">{stakeDisplay}</span>
          </div>
        )}
      </div>
    </div>
  )
}

function LinkHoverContent({ link }: { link: LinkInfo }) {
  const bandwidthDisplay = link.bandwidthBps > 0 ? formatBandwidth(link.bandwidthBps) : 'N/A'
  const latencyDisplay = link.latencyUs > 0 ? `${(link.latencyUs / 1000).toFixed(2)}ms` : 'N/A'
  const lossDisplay = link.lossPercent > 0 ? `${link.lossPercent.toFixed(2)}%` : null

  return (
    <div className="space-y-1">
      <div className="font-medium">{link.code}</div>
      <div className="text-[10px] text-muted-foreground">
        {link.deviceACode}{link.interfaceAName && <span className="font-mono"> ({link.interfaceAName})</span>}
        {' ↔ '}
        {link.deviceZCode}{link.interfaceZName && <span className="font-mono"> ({link.interfaceZName})</span>}
      </div>
      <div className="text-muted-foreground space-y-0.5">
        <div className="flex justify-between gap-4">
          <span>Bandwidth:</span>
          <span className="text-foreground">{bandwidthDisplay}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span>Latency:</span>
          <span className="text-foreground">{latencyDisplay}</span>
        </div>
        {lossDisplay && (
          <div className="flex justify-between gap-4">
            <span>Loss:</span>
            <span className="text-amber-500">{lossDisplay}</span>
          </div>
        )}
        {link.health && (
          <div className="flex justify-between gap-4">
            <span>SLA:</span>
            <span className={
              link.health.status === 'healthy' ? 'text-green-500' :
              link.health.status === 'warning' ? 'text-yellow-500' :
              link.health.status === 'critical' ? 'text-red-500' : 'text-muted-foreground'
            }>
              {link.health.status}
            </span>
          </div>
        )}
        {link.isInterMetro && (
          <div className="flex justify-between gap-4">
            <span>Links:</span>
            <span className="text-foreground">{link.linkCount}</span>
          </div>
        )}
      </div>
    </div>
  )
}

function MetroHoverContent({ metro }: { metro: MetroInfo }) {
  return (
    <div className="space-y-1">
      <div className="font-medium">{metro.name}</div>
      <div className="text-muted-foreground space-y-0.5">
        <div className="flex justify-between gap-4">
          <span>Code:</span>
          <span className="text-foreground">{metro.code}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span>Devices:</span>
          <span className="text-foreground">{metro.deviceCount}</span>
        </div>
      </div>
    </div>
  )
}

function ValidatorHoverContent({ validator }: { validator: ValidatorInfo }) {
  return (
    <div className="space-y-1">
      <div className="font-medium font-mono truncate" title={validator.votePubkey}>
        {validator.votePubkey.substring(0, 12)}...
      </div>
      <div className="text-muted-foreground space-y-0.5">
        <div className="flex justify-between gap-4">
          <span>Location:</span>
          <span className="text-foreground">{validator.city}, {validator.country}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span>Device:</span>
          <span className="text-foreground font-mono">{validator.deviceCode}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span>Stake:</span>
          <span className="text-amber-500">{validator.stakeSol} SOL</span>
        </div>
        <div className="flex justify-between gap-4">
          <span>Commission:</span>
          <span className="text-foreground">{validator.commission}%</span>
        </div>
      </div>
    </div>
  )
}
