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
import { Download } from 'lucide-react'
import type { QueryResponse } from '@/lib/api'

interface ResultsTableProps {
  results: QueryResponse | null
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

export function ResultsTable({ results }: ResultsTableProps) {
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
        if (typeof value === 'object') {
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

  return (
    <div className="border bg-white">
      <div className="flex items-center justify-between px-3 py-2 border-b bg-accent-orange-10">
        <span className="text-xs text-muted-foreground">
          {results.row_count.toLocaleString()} rows
        </span>
        <button
          onClick={() => downloadCSV(results.columns, results.rows)}
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <Download className="h-3.5 w-3.5" />
          Download CSV
        </button>
      </div>
      <div className="overflow-x-auto">
        <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id} className="border-b bg-accent-orange-10 hover:bg-accent-orange-10">
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id} className="font-medium text-xs text-muted-foreground h-9 whitespace-nowrap">
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
          {table.getRowModel().rows.map((row, i) => (
            <TableRow
              key={row.id}
              className={`border-b last:border-b-0 hover:bg-muted ${
                i % 2 === 1 ? 'bg-muted/50' : 'bg-white'
              }`}
            >
              {row.getVisibleCells().map((cell) => (
                <TableCell key={cell.id} className="font-mono text-sm py-2.5 whitespace-nowrap">
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
