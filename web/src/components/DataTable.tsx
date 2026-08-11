import type { Key, ReactNode } from "react";
import { Empty, Table } from "antd";
import type { TableColumnsType, TablePaginationConfig, TableProps } from "antd";

export interface DataTableProps<T extends object> {
  columns: TableColumnsType<T>;
  data: readonly T[];
  rowKey: string | ((record: T) => Key);
  loading?: boolean;
  emptyText?: ReactNode;
  pagination?: false | TablePaginationConfig;
  scrollX?: number | string;
  className?: string;
}

export function DataTable<T extends object>({
  columns,
  data,
  rowKey,
  loading = false,
  emptyText = "暂无数据。",
  pagination = false,
  scrollX,
  className,
}: DataTableProps<T>): ReactNode {
  const tableProps: TableProps<T> = {
    columns,
    dataSource: [...data],
    loading,
    locale: { emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyText} /> },
    pagination,
    rowKey,
    size: "middle",
  };
  if (className !== undefined) tableProps.className = className;
  if (scrollX !== undefined) tableProps.scroll = { x: scrollX };
  return (
    <Table<T> {...tableProps} />
  );
}
