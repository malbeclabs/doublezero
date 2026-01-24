import { useState, useRef, useEffect } from 'react'
import { Search, X } from 'lucide-react'

export interface DeviceOption {
  pk: string
  code: string
  deviceType?: string
  metro?: string
}

interface DeviceSelectorProps {
  devices: DeviceOption[]
  value: string | null
  onChange: (pk: string | null) => void
  placeholder?: string
  label?: string
  labelColor?: string
  disabled?: boolean
}

export function DeviceSelector({
  devices,
  value,
  onChange,
  placeholder = 'Search devices...',
  label,
  labelColor,
  disabled = false,
}: DeviceSelectorProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [highlightedIndex, setHighlightedIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Find selected device
  const selectedDevice = value ? devices.find(d => d.pk === value) : null

  // Filter devices by search
  const filteredDevices = search
    ? devices.filter(d =>
        d.code.toLowerCase().includes(search.toLowerCase()) ||
        d.metro?.toLowerCase().includes(search.toLowerCase())
      )
    : devices

  // Sort filtered devices: exact match first, then by code
  const sortedDevices = [...filteredDevices].sort((a, b) => {
    const aExact = a.code.toLowerCase() === search.toLowerCase()
    const bExact = b.code.toLowerCase() === search.toLowerCase()
    if (aExact && !bExact) return -1
    if (!aExact && bExact) return 1
    return a.code.localeCompare(b.code)
  })

  // Limit to 50 items for display
  const displayedDevices = sortedDevices.slice(0, 50)

  // Reset highlighted index when search changes or dropdown opens
  useEffect(() => {
    setHighlightedIndex(0)
  }, [search, isOpen])

  // Scroll highlighted item into view
  useEffect(() => {
    if (isOpen && listRef.current) {
      const highlightedElement = listRef.current.children[highlightedIndex] as HTMLElement
      if (highlightedElement) {
        highlightedElement.scrollIntoView({ block: 'nearest' })
      }
    }
  }, [highlightedIndex, isOpen])

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false)
        setSearch('')
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Handle keyboard navigation
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      setIsOpen(false)
      setSearch('')
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (!isOpen) {
        setIsOpen(true)
      } else {
        setHighlightedIndex(prev => Math.min(prev + 1, displayedDevices.length - 1))
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setHighlightedIndex(prev => Math.max(prev - 1, 0))
    } else if (e.key === 'Enter' && displayedDevices.length > 0) {
      e.preventDefault()
      onChange(displayedDevices[highlightedIndex].pk)
      setIsOpen(false)
      setSearch('')
    }
  }

  const handleSelect = (pk: string) => {
    onChange(pk)
    setIsOpen(false)
    setSearch('')
  }

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
    onChange(null)
    setSearch('')
  }

  return (
    <div ref={containerRef} className="relative">
      {label && (
        <div className="text-xs mb-1" style={{ color: labelColor }}>
          {label}
        </div>
      )}
      <div
        className={`flex items-center gap-1.5 px-2 py-1.5 rounded border border-[var(--border)] bg-[var(--card)] text-xs ${
          disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-text'
        }`}
        onClick={() => {
          if (!disabled) {
            setIsOpen(true)
            inputRef.current?.focus()
          }
        }}
      >
        <Search className="h-3 w-3 text-muted-foreground flex-shrink-0" />
        {selectedDevice && !isOpen ? (
          <div className="flex items-center gap-1 flex-1 min-w-0">
            <span className="font-medium truncate">{selectedDevice.code}</span>
            {selectedDevice.metro && (
              <span className="text-muted-foreground text-[10px]">({selectedDevice.metro})</span>
            )}
          </div>
        ) : (
          <input
            ref={inputRef}
            type="text"
            value={search}
            onChange={e => {
              setSearch(e.target.value)
              setIsOpen(true)
            }}
            onFocus={() => setIsOpen(true)}
            onKeyDown={handleKeyDown}
            placeholder={selectedDevice ? selectedDevice.code : placeholder}
            className="bg-transparent outline-none flex-1 min-w-0 placeholder:text-muted-foreground"
            disabled={disabled}
          />
        )}
        {(selectedDevice || search) && !disabled && (
          <button
            onClick={handleClear}
            className="p-0.5 hover:bg-[var(--muted)] rounded flex-shrink-0"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {isOpen && !disabled && (
        <div className="absolute z-50 top-full left-0 right-0 mt-1 max-h-48 overflow-y-auto rounded border border-[var(--border)] bg-[var(--card)] shadow-lg">
          {displayedDevices.length === 0 ? (
            <div className="px-2 py-2 text-xs text-muted-foreground">No devices found</div>
          ) : (
            <div ref={listRef}>
              {displayedDevices.map((device, index) => (
                <button
                  key={device.pk}
                  onClick={() => handleSelect(device.pk)}
                  onMouseEnter={() => setHighlightedIndex(index)}
                  className={`w-full text-left px-2 py-1.5 text-xs flex items-center gap-2 ${
                    index === highlightedIndex ? 'bg-[var(--muted)]' : ''
                  } ${device.pk === value ? 'font-semibold' : ''}`}
                >
                  <span className="font-medium">{device.code}</span>
                  {device.deviceType && (
                    <span className="text-[10px] text-muted-foreground capitalize">{device.deviceType}</span>
                  )}
                  {device.metro && (
                    <span className="text-[10px] text-muted-foreground ml-auto">{device.metro}</span>
                  )}
                </button>
              ))}
            </div>
          )}
          {sortedDevices.length > 50 && (
            <div className="px-2 py-1.5 text-[10px] text-muted-foreground border-t border-[var(--border)]">
              {sortedDevices.length - 50} more devices...
            </div>
          )}
        </div>
      )}
    </div>
  )
}
