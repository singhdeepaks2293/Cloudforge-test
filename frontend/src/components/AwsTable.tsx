import type { ReactNode } from 'react'

/**
 * AWS-console-style table: light-grey header row, row borders,
 * 12px text, checkbox column support, filter input above the table
 * and a pagination footer. Generic over the row type; columns are
 * render functions so pages keep their own cell content.
 */
export interface AwsColumn<T> {
  /** Column header text, or a ReactNode (e.g. a select-all checkbox). */
  header: string | ReactNode
  render: (row: T) => ReactNode
  /** Optional stable key for the <th>. Defaults to the header text. */
  key?: string
}

export function AwsTable<T extends { id: string }>({
  columns,
  rows,
  emptyMessage,
  filter,
  onFilterChange,
  filterPlaceholder = 'Filter',
  totalCount,
  caption,
  selectable = false,
  selectedIds,
  onToggleRow,
  onToggleAll,
}: {
  columns: AwsColumn<T>[]
  rows: T[]
  emptyMessage: string
  filter?: string
  onFilterChange?: (value: string) => void
  filterPlaceholder?: string
  /** Total rows before filtering (shown in the footer). */
  totalCount?: number
  caption?: string
  /** Show the AWS checkbox column. */
  selectable?: boolean
  selectedIds?: string[]
  onToggleRow?: (id: string) => void
  onToggleAll?: (checked: boolean) => void
}) {
  const selected = new Set(selectedIds ?? [])
  const allSelected = rows.length > 0 && rows.every((r) => selected.has(r.id))
  const colCount = columns.length + (selectable ? 1 : 0)
  return (
    <div>
      {onFilterChange !== undefined && (
        <div className="mb-3 flex items-center gap-2">
          <input
            type="search"
            value={filter ?? ''}
            onChange={(e) => onFilterChange(e.target.value)}
            placeholder={filterPlaceholder}
            aria-label={filterPlaceholder}
            className="w-72 max-w-full rounded-sm border border-aws-border-dark bg-white px-2.5 py-1.5 text-sm focus:border-aws-link focus:outline-none"
          />
          <span className="text-xs text-aws-muted">
            {rows.length} of {totalCount ?? rows.length} shown
          </span>
        </div>
      )}
      <div className="overflow-x-auto border border-aws-border">
        <table className="w-full border-collapse bg-white text-left text-xs">
          {caption && <caption className="sr-only">{caption}</caption>}
          <thead>
            <tr className="bg-[#f2f3f3]">
              {selectable && (
                <th scope="col" className="w-8 border-b border-aws-border px-2 py-2">
                  <input
                    type="checkbox"
                    aria-label="Select all rows"
                    checked={allSelected}
                    onChange={(e) => onToggleAll?.(e.target.checked)}
                  />
                </th>
              )}
              {columns.map((c, i) => (
                <th
                  key={c.key ?? (typeof c.header === 'string' ? c.header : `col-${i}`)}
                  scope="col"
                  className="border-b border-aws-border px-3 py-2 font-semibold whitespace-nowrap"
                >
                  {c.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={colCount} className="px-3 py-4 text-center text-sm text-aws-muted">
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              rows.map((row) => (
                <tr
                  key={row.id}
                  className={`border-b border-aws-border last:border-b-0 hover:bg-[#f9f9f9] ${selected.has(row.id) ? 'bg-[#f2f8fd]' : ''}`}
                >
                  {selectable && (
                    <td className="px-2 py-2 align-top">
                      <input
                        type="checkbox"
                        aria-label={`Select ${row.id}`}
                        checked={selected.has(row.id)}
                        onChange={() => onToggleRow?.(row.id)}
                      />
                    </td>
                  )}
                  {columns.map((c, i) => (
                    <td key={c.key ?? (typeof c.header === 'string' ? c.header : `col-${i}`)} className="px-3 py-2 align-top">
                      {c.render(row)}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <p className="mt-2 text-xs text-aws-muted">
        {rows.length} item{rows.length === 1 ? '' : 's'}
        {totalCount !== undefined && totalCount !== rows.length ? ` (filtered from ${totalCount})` : ''}
      </p>
    </div>
  )
}
