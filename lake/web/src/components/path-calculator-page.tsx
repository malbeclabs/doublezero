import { useState, useMemo, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { Loader2, Route, AlertCircle, ArrowRight, Search, X, Copy, Check, ExternalLink, RotateCcw } from 'lucide-react'
import { fetchISISTopology, fetchISISPaths } from '@/lib/api'
import type { MultiPathResponse, SinglePath } from '@/lib/api'

// Path colors matching the graph view
const PATH_COLORS = [
  '#22c55e',  // green - primary/shortest
  '#3b82f6',  // blue - alternate 1
  '#a855f7',  // purple - alternate 2
  '#f97316',  // orange - alternate 3
  '#06b6d4',  // cyan - alternate 4
]

interface DeviceOption {
  pk: string
  code: string
  status: string
  deviceType: string
}

function DeviceSearch({
  label,
  placeholder,
  value,
  onChange,
  devices,
  excludePK,
}: {
  label: string
  placeholder: string
  value: DeviceOption | null
  onChange: (device: DeviceOption | null) => void
  devices: DeviceOption[]
  excludePK?: string
}) {
  const [search, setSearch] = useState('')
  const [isOpen, setIsOpen] = useState(false)

  const filteredDevices = useMemo(() => {
    const query = search.toLowerCase()
    return devices
      .filter(d => d.pk !== excludePK)
      .filter(d => d.code.toLowerCase().includes(query))
      .slice(0, 20)
  }, [devices, search, excludePK])

  return (
    <div className="flex-1">
      <label className="block text-sm font-medium text-muted-foreground mb-2">{label}</label>
      <div className="relative">
        {value ? (
          <div className="flex items-center gap-2 px-3 py-2 border border-border rounded-md bg-card">
            <span className="font-mono text-sm flex-1">{value.code}</span>
            <button
              onClick={() => { onChange(null); setSearch('') }}
              className="p-1 hover:bg-muted rounded"
            >
              <X className="h-4 w-4 text-muted-foreground" />
            </button>
          </div>
        ) : (
          <>
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <input
                type="text"
                value={search}
                onChange={(e) => { setSearch(e.target.value); setIsOpen(true) }}
                onFocus={() => setIsOpen(true)}
                placeholder={placeholder}
                className="w-full pl-9 pr-3 py-2 border border-border rounded-md bg-card text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
              />
            </div>
            {isOpen && search && filteredDevices.length > 0 && (
              <div className="absolute z-50 w-full mt-1 bg-card border border-border rounded-md shadow-lg max-h-60 overflow-y-auto">
                {filteredDevices.map(device => (
                  <button
                    key={device.pk}
                    onClick={() => {
                      onChange(device)
                      setSearch('')
                      setIsOpen(false)
                    }}
                    className="w-full px-3 py-2 text-left hover:bg-muted flex items-center justify-between"
                  >
                    <span className="font-mono text-sm">{device.code}</span>
                    <span className="text-xs text-muted-foreground capitalize">{device.deviceType}</span>
                  </button>
                ))}
              </div>
            )}
            {isOpen && search && filteredDevices.length === 0 && (
              <div className="absolute z-50 w-full mt-1 bg-card border border-border rounded-md shadow-lg p-3 text-sm text-muted-foreground">
                No devices found
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function PathCard({
  path,
  index,
  isSelected,
  onSelect,
}: {
  path: SinglePath
  index: number
  isSelected: boolean
  onSelect: () => void
}) {
  const [copied, setCopied] = useState(false)

  const copyPath = () => {
    const pathText = path.path.map(h => h.deviceCode).join(' → ')
    navigator.clipboard.writeText(pathText)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div
      className={`border rounded-lg p-4 cursor-pointer transition-all ${
        isSelected
          ? 'border-primary bg-primary/5 ring-2 ring-primary/20'
          : 'border-border hover:border-primary/50'
      }`}
      onClick={onSelect}
    >
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <div
            className="w-3 h-3 rounded-full"
            style={{ backgroundColor: PATH_COLORS[index % PATH_COLORS.length] }}
          />
          <span className="font-medium">Path {index + 1}</span>
          {index === 0 && (
            <span className="text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400 px-2 py-0.5 rounded">
              Shortest
            </span>
          )}
        </div>
        <button
          onClick={(e) => { e.stopPropagation(); copyPath() }}
          className="p-1.5 hover:bg-muted rounded-md"
          title="Copy path"
        >
          {copied ? (
            <Check className="h-4 w-4 text-green-500" />
          ) : (
            <Copy className="h-4 w-4 text-muted-foreground" />
          )}
        </button>
      </div>

      <div className="grid grid-cols-2 gap-4 mb-4 text-sm">
        <div>
          <span className="text-muted-foreground">Hops:</span>{' '}
          <span className="font-medium">{path.hopCount}</span>
        </div>
        <div>
          <span className="text-muted-foreground">ISIS Metric:</span>{' '}
          <span className="font-medium">{(path.totalMetric / 1000).toFixed(2)}ms</span>
        </div>
        {path.measuredLatencyMs !== undefined && path.measuredLatencyMs > 0 && (
          <>
            <div>
              <span className="text-muted-foreground">Measured:</span>{' '}
              <span className="font-medium text-primary">{path.measuredLatencyMs.toFixed(2)}ms</span>
            </div>
            {path.totalSamples !== undefined && (
              <div>
                <span className="text-muted-foreground">Samples:</span>{' '}
                <span className="font-medium">{path.totalSamples.toLocaleString()}</span>
              </div>
            )}
          </>
        )}
      </div>

      <div className="space-y-1">
        {path.path.map((hop, i) => (
          <div key={hop.devicePK} className="flex items-center gap-2 text-sm">
            <span className="w-5 text-muted-foreground">{i + 1}.</span>
            <Link
              to={`/dz/devices/${hop.devicePK}`}
              onClick={(e) => e.stopPropagation()}
              className="font-mono hover:text-primary flex items-center gap-1"
            >
              {hop.deviceCode}
              <ExternalLink className="h-3 w-3 opacity-0 hover:opacity-100" />
            </Link>
            <div className="ml-auto flex items-center gap-2">
              {hop.edgeMeasuredMs !== undefined && hop.edgeMeasuredMs > 0 && (
                <span className="text-primary text-xs" title={`Measured RTT: ${hop.edgeMeasuredMs.toFixed(2)}ms (${hop.edgeSampleCount?.toLocaleString() ?? 0} samples)`}>
                  {hop.edgeMeasuredMs.toFixed(1)}ms measured
                </span>
              )}
              {hop.edgeLossPct !== undefined && hop.edgeLossPct > 0.1 && (
                <span className={`text-xs ${hop.edgeLossPct > 1 ? 'text-red-500' : 'text-yellow-500'}`} title={`Packet loss: ${hop.edgeLossPct.toFixed(2)}%`}>
                  {hop.edgeLossPct.toFixed(1)}% loss
                </span>
              )}
              {hop.edgeMetric !== undefined && hop.edgeMetric > 0 && (
                <span className="text-muted-foreground text-xs" title="ISIS metric (configured on router)">
                  {(hop.edgeMetric / 1000).toFixed(1)}ms ISIS
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export function PathCalculatorPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [sourceDevice, setSourceDevice] = useState<DeviceOption | null>(null)
  const [targetDevice, setTargetDevice] = useState<DeviceOption | null>(null)
  const [selectedPathIndex, setSelectedPathIndex] = useState(0)
  const [initializedFromUrl, setInitializedFromUrl] = useState(false)

  // Fetch topology for device list
  const { data: topology, isLoading: topologyLoading } = useQuery({
    queryKey: ['isis-topology'],
    queryFn: fetchISISTopology,
    staleTime: 60000,
  })

  // Convert topology nodes to device options
  const devices: DeviceOption[] = useMemo(() => {
    if (!topology?.nodes) return []
    return topology.nodes.map(n => ({
      pk: n.data.id,
      code: n.data.label,
      status: n.data.status,
      deviceType: n.data.deviceType,
    })).sort((a, b) => a.code.localeCompare(b.code))
  }, [topology])

  // Initialize from URL params once devices are loaded
  useEffect(() => {
    if (initializedFromUrl || devices.length === 0) return

    const fromParam = searchParams.get('from')
    const toParam = searchParams.get('to')

    if (fromParam) {
      const device = devices.find(d => d.pk === fromParam || d.code === fromParam)
      if (device) setSourceDevice(device)
    }
    if (toParam) {
      const device = devices.find(d => d.pk === toParam || d.code === toParam)
      if (device) setTargetDevice(device)
    }

    setInitializedFromUrl(true)
  }, [devices, searchParams, initializedFromUrl])

  // Update URL when devices change
  const updateSource = (device: DeviceOption | null) => {
    setSourceDevice(device)
    setSelectedPathIndex(0)
    const newParams = new URLSearchParams(searchParams)
    if (device) {
      newParams.set('from', device.pk)
    } else {
      newParams.delete('from')
    }
    setSearchParams(newParams, { replace: true })
  }

  const updateTarget = (device: DeviceOption | null) => {
    setTargetDevice(device)
    setSelectedPathIndex(0)
    const newParams = new URLSearchParams(searchParams)
    if (device) {
      newParams.set('to', device.pk)
    } else {
      newParams.delete('to')
    }
    setSearchParams(newParams, { replace: true })
  }

  const resetSelection = () => {
    setSourceDevice(null)
    setTargetDevice(null)
    setSelectedPathIndex(0)
    setSearchParams({}, { replace: true })
  }

  // Fetch paths when both devices are selected
  const {
    data: pathsResult,
    isLoading: pathsLoading,
    error: pathsError,
  } = useQuery<MultiPathResponse>({
    queryKey: ['paths', sourceDevice?.pk, targetDevice?.pk],
    queryFn: () => fetchISISPaths(sourceDevice!.pk, targetDevice!.pk, 5),
    enabled: !!sourceDevice && !!targetDevice,
  })

  // Reset selected path when results change
  const paths = pathsResult?.paths ?? []
  if (selectedPathIndex >= paths.length && paths.length > 0) {
    setSelectedPathIndex(0)
  }

  if (topologyLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-4xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex items-center gap-3 mb-6">
          <Route className="h-6 w-6 text-muted-foreground" />
          <h1 className="text-2xl font-medium">Path Calculator</h1>
        </div>

        <p className="text-muted-foreground mb-6">
          Find and compare paths between two devices in the ISIS topology.
        </p>

        {/* Device Selection */}
        <div className="bg-card border border-border rounded-lg p-6 mb-6">
          <div className="flex items-end gap-4">
            <DeviceSearch
              label="Source Device"
              placeholder="Search source..."
              value={sourceDevice}
              onChange={updateSource}
              devices={devices}
              excludePK={targetDevice?.pk}
            />

            <div className="pb-2">
              <ArrowRight className="h-5 w-5 text-muted-foreground" />
            </div>

            <DeviceSearch
              label="Destination Device"
              placeholder="Search destination..."
              value={targetDevice}
              onChange={updateTarget}
              devices={devices}
              excludePK={sourceDevice?.pk}
            />

            {(sourceDevice || targetDevice) && (
              <button
                onClick={resetSelection}
                className="pb-2 p-2 hover:bg-muted rounded-md text-muted-foreground hover:text-foreground transition-colors"
                title="Reset selection"
              >
                <RotateCcw className="h-5 w-5" />
              </button>
            )}
          </div>

          {sourceDevice && targetDevice && (
            <div className="mt-4 pt-4 border-t border-border">
              <Link
                to={`/topology/graph?path_source=${sourceDevice.pk}&path_target=${targetDevice.pk}`}
                className="text-sm text-primary hover:underline flex items-center gap-1"
              >
                View in graph
                <ExternalLink className="h-3 w-3" />
              </Link>
            </div>
          )}
        </div>

        {/* Results */}
        {pathsLoading && (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground mr-2" />
            <span className="text-muted-foreground">Finding paths...</span>
          </div>
        )}

        {pathsError && (
          <div className="flex items-center justify-center py-12">
            <AlertCircle className="h-6 w-6 text-red-500 mr-2" />
            <span className="text-red-500">{pathsError.message}</span>
          </div>
        )}

        {pathsResult?.error && (
          <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
            <div className="flex items-center gap-2 text-red-700 dark:text-red-400">
              <AlertCircle className="h-5 w-5" />
              <span>{pathsResult.error}</span>
            </div>
          </div>
        )}

        {paths.length > 0 && (
          <div>
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-medium">
                {paths.length} {paths.length === 1 ? 'Path' : 'Paths'} Found
              </h2>
            </div>

            <div className="grid gap-4">
              {paths.map((path, index) => (
                <PathCard
                  key={index}
                  path={path}
                  index={index}
                  isSelected={index === selectedPathIndex}
                  onSelect={() => setSelectedPathIndex(index)}
                />
              ))}
            </div>
          </div>
        )}

        {sourceDevice && targetDevice && !pathsLoading && !pathsResult?.error && paths.length === 0 && (
          <div className="text-center py-12 text-muted-foreground">
            <Route className="h-12 w-12 mx-auto mb-4 opacity-50" />
            <p>No paths found between these devices.</p>
            <p className="text-sm mt-2">They may not be connected in the ISIS topology.</p>
          </div>
        )}
      </div>
    </div>
  )
}
