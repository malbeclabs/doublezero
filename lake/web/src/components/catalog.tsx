import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCatalog, type TableInfo } from '@/lib/api'
import { ChevronDown, ChevronRight, Database } from 'lucide-react'

interface CatalogProps {
  onSelectTable: (table: TableInfo) => void
}

export function Catalog({ onSelectTable }: CatalogProps) {
  const [isOpen, setIsOpen] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['catalog'],
    queryFn: fetchCatalog,
  })

  const tables = data?.tables.filter(t => t.type === 'table') ?? []
  const views = data?.tables.filter(t => t.type === 'view') ?? []

  return (
    <div className="border bg-grey-10">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full px-3 py-2 flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors bg-accent-orange-10"
      >
        <Database className="h-3.5 w-3.5" />
        <span>Schema</span>
        {!isLoading && (
          <span className="opacity-60">
            — {tables.length} tables, {views.length} views
          </span>
        )}
        {isLoading && (
          <span className="opacity-60">— loading</span>
        )}
        {isOpen ? (
          <ChevronDown className="h-3.5 w-3.5 ml-auto" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 ml-auto" />
        )}
      </button>

      {isOpen && (
        <div className="border-t px-3 py-3">
          {error ? (
            <div className="text-sm text-destructive">Failed to load catalog</div>
          ) : (
            <div className="flex flex-wrap gap-2">
              {tables.map(table => (
                <button
                  key={table.name}
                  onClick={() => onSelectTable(table)}
                  className="text-xs px-2 py-1 border bg-grey-10 text-muted-foreground hover:text-foreground hover:border-foreground transition-colors"
                >
                  {table.name}
                </button>
              ))}
              {views.map(view => (
                <button
                  key={view.name}
                  onClick={() => onSelectTable(view)}
                  className="text-xs px-2 py-1 border bg-grey-10 text-muted-foreground hover:text-foreground hover:border-foreground transition-colors italic"
                >
                  {view.name}
                </button>
              ))}
              {tables.length === 0 && views.length === 0 && (
                <span className="text-sm text-muted-foreground italic">No tables found</span>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
