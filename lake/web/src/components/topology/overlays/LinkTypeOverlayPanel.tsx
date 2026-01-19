// Link type colors
export const LINK_TYPE_COLORS: Record<string, { light: string; dark: string }> = {
  WAN: { light: '#2563eb', dark: '#60a5fa' },           // blue - inter-metro wide area
  DZX: { light: '#7c3aed', dark: '#a78bfa' },           // purple - local exchange
  'Inter-Metro': { light: '#0891b2', dark: '#22d3ee' }, // cyan - aggregated inter-metro
  default: { light: '#6b7280', dark: '#9ca3af' },       // gray
}

interface LinkTypeOverlayPanelProps {
  isDark: boolean
  linkCounts?: Record<string, number>
}

export function LinkTypeOverlayPanel({ isDark, linkCounts }: LinkTypeOverlayPanelProps) {
  // Get all link types from counts, or use defaults
  const linkTypes = linkCounts
    ? Object.keys(linkCounts).filter(type => type !== 'Inter-Metro').sort()
    : ['WAN', 'DZX']

  return (
    <div className="p-4 space-y-4">
      <div>
        <h3 className="text-sm font-medium mb-2">Link Types</h3>
        <p className="text-xs text-muted-foreground mb-3">
          Links are colored by their connection type.
        </p>
      </div>

      <div className="space-y-2">
        {linkTypes.map((type) => {
          const colors = LINK_TYPE_COLORS[type] || LINK_TYPE_COLORS.default
          const count = linkCounts?.[type] ?? 0
          return (
            <div key={type} className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <div
                  className="w-6 h-1 rounded-full"
                  style={{
                    backgroundColor: isDark ? colors.dark : colors.light,
                  }}
                />
                <span className="text-sm">{type}</span>
              </div>
              {linkCounts && (
                <span className="text-xs text-muted-foreground">{count}</span>
              )}
            </div>
          )
        })}
      </div>

      <div className="pt-3 border-t border-[var(--border)]">
        <h4 className="text-xs font-medium mb-2 text-muted-foreground">Line Style</h4>
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <div
              className="w-6 h-0.5 rounded-full"
              style={{ backgroundColor: isDark ? '#9ca3af' : '#6b7280' }}
            />
            <span className="text-sm">Standard link</span>
          </div>
          <div className="flex items-center gap-2">
            <div
              className="w-6 h-1 rounded-full"
              style={{ backgroundColor: isDark ? '#60a5fa' : '#2563eb' }}
            />
            <span className="text-sm">WAN link (thicker)</span>
          </div>
        </div>
      </div>
    </div>
  )
}
