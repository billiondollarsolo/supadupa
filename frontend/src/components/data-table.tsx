import { flexRender, getCoreRowModel, type ColumnDef, useReactTable } from "@tanstack/react-table";

type DataTableProps<TData> = {
  className?: string;
  columns: ColumnDef<TData>[];
  data: TData[];
  emptyText: string;
  minWidth?: number;
  rowClassName?: (row: TData) => string;
};

export function DataTable<TData>({ className = "", columns, data, emptyText, minWidth = 760, rowClassName }: DataTableProps<TData>) {
  const table = useReactTable({
    columns,
    data,
    getCoreRowModel: getCoreRowModel(),
  });

  if (data.length === 0) {
    return <p className="text-sm text-muted">{emptyText}</p>;
  }

  return (
    <div className={`data-table-wrap ${className}`.trim()}>
      <table className="data-table" style={{ minWidth }}>
        <thead>
          {table.getHeaderGroups().map((headerGroup) => (
            <tr key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <th key={header.id} style={{ width: header.getSize() }}>
                  {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr className={rowClassName?.(row.original)} key={row.id}>
              {row.getVisibleCells().map((cell) => (
                <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
