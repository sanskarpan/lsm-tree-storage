import * as React from "react";
import {
  type ColumnDef,
  type SortingState,
  type VisibilityState,
  type ColumnFiltersState,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";

import { cn } from "@/lib/utils";
import { Input } from "./input";
import { Skeleton } from "./skeleton";

const DEFAULT_ROW_HEIGHT = 36;
const DEFAULT_OVERSCAN = 8;
const DEFAULT_VIRTUALIZE_THRESHOLD = 100;

export interface DataTableProps<TData> {
  columns: ColumnDef<TData, unknown>[];
  data: TData[];
  searchPlaceholder?: string;
  searchColumnId?: string;
  virtualizeThreshold?: number;
  rowHeight?: number;
  emptyState?: React.ReactNode;
  loading?: boolean;
  loadingRowCount?: number;
  className?: string;
  "aria-label"?: string;
}

export function DataTable<TData>({
  columns,
  data,
  searchPlaceholder = "Filter…",
  searchColumnId,
  virtualizeThreshold = DEFAULT_VIRTUALIZE_THRESHOLD,
  rowHeight = DEFAULT_ROW_HEIGHT,
  emptyState,
  loading = false,
  loadingRowCount = 6,
  className,
  "aria-label": ariaLabel,
}: DataTableProps<TData>) {
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    [],
  );
  const [columnVisibility, setColumnVisibility] =
    React.useState<VisibilityState>({});
  const [globalFilter, setGlobalFilter] = React.useState("");

  const table = useReactTable({
    data,
    columns,
    state: { sorting, columnFilters, columnVisibility, globalFilter },
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onColumnVisibilityChange: setColumnVisibility,
    onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    globalFilterFn: "includesString",
  });

  const rows = table.getRowModel().rows;
  const shouldVirtualize = rows.length >= virtualizeThreshold;
  const parentRef = React.useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowHeight,
    overscan: DEFAULT_OVERSCAN,
    enabled: shouldVirtualize,
  });

  const virtualRows = shouldVirtualize
    ? virtualizer.getVirtualItems()
    : rows.map((r) => ({ index: r.index, start: r.index * rowHeight, size: rowHeight }));
  const totalSize = shouldVirtualize ? virtualizer.getTotalSize() : rows.length * rowHeight;

  if (loading) {
    return (
      <div className={cn("flex flex-col gap-2 p-2", className)} aria-busy="true">
        {Array.from({ length: loadingRowCount }, (_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    );
  }

  if (rows.length === 0) {
    return (
      <div
        className={cn(
          "flex items-center justify-center rounded-md border border-dashed border-[var(--border)] p-8 text-sm text-[var(--fg-muted)]",
          className,
        )}
      >
        {emptyState ?? "No results."}
      </div>
    );
  }

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <Input
        value={globalFilter}
        onChange={(event) => setGlobalFilter(event.target.value)}
        placeholder={searchPlaceholder}
        className="max-w-sm"
        aria-label="Filter rows"
      />
      <div
        ref={parentRef}
        className={cn(
          "relative overflow-auto rounded-md border border-[var(--border)] bg-[var(--bg-elevated)]",
          shouldVirtualize ? "max-h-[60vh]" : "",
        )}
        role="region"
        aria-label={ariaLabel}
      >
        <table
          className="w-full caption-bottom text-sm"
          role="table"
          aria-rowcount={rows.length}
        >
          <thead className="sticky top-0 z-[var(--z-sticky)] bg-[var(--bg-elevated)]">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id} className="border-b border-[var(--border)]">
                {headerGroup.headers.map((header) => {
                  const canSort = header.column.getCanSort();
                  const sorted = header.column.getIsSorted();
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={
                        sorted === "asc"
                          ? "ascending"
                          : sorted === "desc"
                            ? "descending"
                            : "none"
                      }
                      className="h-10 px-3 text-left align-middle text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]"
                      style={{ width: header.getSize() }}
                    >
                      {header.isPlaceholder ? null : canSort ? (
                        <button
                          type="button"
                          onClick={header.column.getToggleSortingHandler()}
                          className="inline-flex items-center gap-1 rounded-sm hover:text-[var(--fg)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
                        >
                          {flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                          <span aria-hidden="true">
                            {sorted === "asc" ? "▲" : sorted === "desc" ? "▼" : "↕"}
                          </span>
                        </button>
                      ) : (
                        flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )
                      )}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody
            style={
              shouldVirtualize
                ? { display: "block", height: `${totalSize}px`, position: "relative" }
                : undefined
            }
          >
            {shouldVirtualize
              ? virtualRows.map((virtualRow) => {
                  const row = rows[virtualRow.index];
                  if (!row) return null;
                  return (
                    <tr
                      key={row.id}
                      data-index={virtualRow.index}
                      style={{
                        position: "absolute",
                        top: 0,
                        left: 0,
                        width: "100%",
                        transform: `translateY(${virtualRow.start}px)`,
                        height: `${virtualRow.size}px`,
                        display: "table",
                        tableLayout: "fixed",
                      }}
                      className="border-b border-[var(--border)] hover:bg-[var(--muted)]/40"
                    >
                      {row.getVisibleCells().map((cell) => (
                        <td
                          key={cell.id}
                          className="px-3 align-middle text-sm"
                          style={{ width: cell.column.getSize() }}
                        >
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      ))}
                    </tr>
                  );
                })
              : rows.map((row) => (
                  <tr
                    key={row.id}
                    className="border-b border-[var(--border)] hover:bg-[var(--muted)]/40"
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className="px-3 align-middle text-sm"
                        style={{ width: cell.column.getSize() }}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                ))}
          </tbody>
        </table>
      </div>
      <div
        className="text-xs text-[var(--fg-muted)]"
        aria-live="polite"
      >
        {rows.length} of {data.length} {data.length === 1 ? "row" : "rows"}
        {shouldVirtualize ? " (virtualized)" : ""}
      </div>
    </div>
  );
}
