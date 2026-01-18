import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  type ColumnDef,
} from '@tanstack/react-table'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Download, MessageCircle } from 'lucide-react'
import type { QueryResponse } from '@/lib/api'
import { formatNeo4jValue, isNeo4jValue } from '@/lib/neo4j-utils'

interface ResultsTableProps {
  results: QueryResponse | null
  onAskAboutResults?: () => void
  embedded?: boolean // When true, skip outer chrome (used inside ResultsView)
}

function escapeCSV(value: unknown): string {
  if (value === null || value === undefined) return ''
  const str = typeof value === 'object' ? JSON.stringify(value) : String(value)
  // Escape quotes and wrap in quotes if contains comma, quote, or newline
  if (str.includes(',') || str.includes('"') || str.includes('\n')) {
    return `"${str.replace(/"/g, '""')}"`
  }
  return str
}

function downloadCSV(columns: string[], rows: unknown[][]) {
  const header = columns.map(escapeCSV).join(',')
  const body = rows.map(row => row.map(escapeCSV).join(',')).join('\n')
  const csv = header + '\n' + body

  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `query-results-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.csv`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

export function ResultsTable({ results, onAskAboutResults, embedded = false }: ResultsTableProps) {
  const columns: ColumnDef<unknown[]>[] = (results?.columns ?? []).map(
    (col, index) => ({
      id: col,
      header: col,
      accessorFn: (row) => row[index],
      cell: ({ getValue }) => {
        const value = getValue()
        if (value === null) {
          return <span className="text-muted-foreground italic">null</span>
        }
        // Format Neo4j objects (nodes, relationships, paths) nicely
        if (isNeo4jValue(value)) {
          return <span className="text-primary">{formatNeo4jValue(value)}</span>
        }
        if (typeof value === 'object') {
          // Check if any array items are Neo4j values
          if (Array.isArray(value) && value.some(v => isNeo4jValue(v))) {
            return <span className="text-primary">{formatNeo4jValue(value)}</span>
          }
          return JSON.stringify(value)
        }
        return String(value)
      },
    })
  )

  const table = useReactTable({
    data: results?.rows ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  if (!results) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm italic">
        Run a query to see results
      </div>
    )
  }

  if (results.rows.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm italic">
        No results
      </div>
    )
  }

  // Embedded mode: just render the table without outer chrome
  if (embedded) {
    return (
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id} className="border-b border-border hover:bg-transparent">
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id} className="px-4 py-2 font-medium text-sm whitespace-nowrap">
                    {flexRender(
                      header.column.columnDef.header,
                      header.getContext()
                    )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                className="border-b border-border last:border-b-0 hover:bg-muted/50"
              >
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id} className="px-4 py-2.5 font-mono text-sm whitespace-nowrap">
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    )
  }

  // Full mode: with header chrome
  return (
    <div className="border border-border bg-card rounded-lg overflow-hidden">
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border">
        <span className="text-sm text-muted-foreground">
          {results.row_count.toLocaleString()} rows
        </span>
        <div className="flex items-center gap-4">
          {onAskAboutResults && (
            <button
              onClick={onAskAboutResults}
              className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <MessageCircle className="h-4 w-4" />
              Ask about results
            </button>
          )}
          <button
            onClick={() => downloadCSV(results.columns, results.rows)}
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <Download className="h-4 w-4" />
            Download CSV
          </button>
        </div>
      </div>
      <div className="overflow-x-auto">
        <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id} className="border-b border-border hover:bg-transparent">
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id} className="px-4 py-2 font-medium text-sm whitespace-nowrap">
                  {flexRender(
                    header.column.columnDef.header,
                    header.getContext()
                  )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.map((row) => (
            <TableRow
              key={row.id}
              className="border-b border-border last:border-b-0 hover:bg-muted/50"
            >
              {row.getVisibleCells().map((cell) => (
                <TableCell key={cell.id} className="px-4 py-2.5 font-mono text-sm whitespace-nowrap">
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
        </Table>
      </div>
    </div>
  )
}
