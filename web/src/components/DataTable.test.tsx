import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DataTable } from "./DataTable";

interface Row {
  id: number;
  name: string;
}

describe("DataTable", () => {
  it("renders typed rows and the shared empty state", () => {
    const columns = [{ title: "名称", dataIndex: "name", key: "name" }];
    const { rerender } = render(<DataTable<Row> columns={columns} data={[{ id: 1, name: "Paper" }]} rowKey="id" />);
    expect(screen.getByText("Paper")).toBeInTheDocument();
    rerender(<DataTable<Row> columns={columns} data={[]} emptyText="当前为空" rowKey="id" />);
    expect(screen.getByText("当前为空")).toBeInTheDocument();
  });
});
