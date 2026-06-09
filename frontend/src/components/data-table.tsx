import { useState } from "react";
import { flexRender, getCoreRowModel, getSortedRowModel, type ColumnDef, type SortingState, useReactTable } from "@tanstack/react-table";
import { ChevronDown, ChevronUp, ChevronsUpDown } from "lucide-react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

type DataTableProps<TData> = {
  className?: string;
  columns: ColumnDef<TData>[];
  data: TData[];
  emptyText: string;
  minWidth?: number;
  rowClassName?: (row: TData) => string;
  // Enable click-to-sort headers (TanStack sorting). Columns opt out with
  // enableSorting: false in their ColumnDef (e.g. action/status columns).
  sortable?: boolean;
  initialSort?: SortingState;
};

export function DataTable<TData>({ className = "", columns, data, emptyText, minWidth = 760, rowClassName, sortable = false, initialSort = [] }: DataTableProps<TData>) {
  const [sorting, setSorting] = useState<SortingState>(initialSort);
  const table = useReactTable({
    columns,
    data,
    enableSorting: sortable,
    state: sortable ? { sorting } : undefined,
    onSortingChange: sortable ? setSorting : undefined,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: sortable ? getSortedRowModel() : undefined,
  });

  if (data.length === 0) {
    return <p className="text-sm text-muted">{emptyText}</p>;
  }

  return (
    <div className={`data-table-wrap ${className}`.trim()}>
      <Table className="data-table" style={{ minWidth }}>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => {
                const canSort = sortable && header.column.getCanSort();
                const sorted = header.column.getIsSorted();
                return (
                  <TableHead key={header.id} style={{ width: header.getSize() }}>
                    {header.isPlaceholder ? null : canSort ? (
                      <button type="button" className="flex items-center gap-1 text-left hover:text-text" onClick={header.column.getToggleSortingHandler()}>
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        {sorted === "asc" ? <ChevronUp size={12} /> : sorted === "desc" ? <ChevronDown size={12} /> : <ChevronsUpDown className="opacity-40" size={12} />}
                      </button>
                    ) : (
                      flexRender(header.column.columnDef.header, header.getContext())
                    )}
                  </TableHead>
                );
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.map((row) => (
            <TableRow className={rowClassName?.(row.original)} key={row.id}>
              {row.getVisibleCells().map((cell) => (
                <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
