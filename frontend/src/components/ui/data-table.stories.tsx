import type { Meta, StoryObj } from "@storybook/react";
import type { ColumnDef } from "@tanstack/react-table";

import { DataTable } from "./data-table";
import { Badge } from "./badge";

type Person = {
  id: number;
  name: string;
  role: string;
  active: boolean;
};

const people: Person[] = [
  { id: 1, name: "L0 file", role: "Write batch", active: true },
  { id: 2, name: "Bloom filter", role: "Read", active: true },
  { id: 3, name: "WAL", role: "Write-ahead", active: false },
  { id: 4, name: "Compaction", role: "Background", active: true },
  { id: 5, name: "Memtable", role: "Mutable", active: true },
];

const columns: ColumnDef<Person, unknown>[] = [
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ getValue }) => (
      <span className="font-mono text-xs">{getValue<string>()}</span>
    ),
  },
  {
    accessorKey: "role",
    header: "Role",
    cell: ({ getValue }) => (
      <Badge variant="info">{getValue<string>()}</Badge>
    ),
  },
  {
    accessorKey: "active",
    header: "Status",
    cell: ({ getValue }) => (
      <Badge variant={getValue<boolean>() ? "success" : "secondary"}>
        {getValue<boolean>() ? "active" : "inactive"}
      </Badge>
    ),
  },
];

const meta: Meta<typeof DataTable<Person>> = {
  title: "Primitives/DataTable",
  component: DataTable<Person>,
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    "aria-label": "Demo data",
    data: people,
    searchPlaceholder: "Filter…",
    emptyState: "Nothing here yet.",
    columns,
  },
};

export const Empty: Story = {
  args: {
    "aria-label": "Empty demo",
    data: [],
    emptyState: "Nothing here yet.",
    columns,
  },
};
