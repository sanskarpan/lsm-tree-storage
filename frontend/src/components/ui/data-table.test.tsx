import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe, toHaveNoViolations } from "jest-axe";
import type { ColumnDef } from "@tanstack/react-table";

import { DataTable } from "./data-table";

expect.extend(toHaveNoViolations);

type Row = { name: string; size: number };

const rows: Row[] = [
  { name: "alpha", size: 100 },
  { name: "beta", size: 200 },
  { name: "gamma", size: 300 },
];

const columns: ColumnDef<Row, unknown>[] = [
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ getValue }) => getValue<string>(),
  },
  {
    accessorKey: "size",
    header: "Size",
    cell: ({ getValue }) => getValue<number>(),
  },
];

describe("DataTable keyboard nav", () => {
  it("the filter input is reachable via Tab and accepts typing", async () => {
    const user = userEvent.setup();
    render(
      <DataTable
        aria-label="Demo"
        data={rows}
        columns={columns}
        searchPlaceholder="Filter…"
      />,
    );

    const input = screen.getByLabelText("Filter rows");
    expect(input).toBeInstanceOf(HTMLInputElement);

    await user.tab();
    expect(input).toHaveFocus();

    await user.keyboard("alpha");
    expect(input).toHaveValue("alpha");
    expect(screen.getByText("1 of 3 rows")).toBeInTheDocument();
  });

  it("a sortable column header is a focusable button", async () => {
    render(
      <DataTable
        aria-label="Demo"
        data={rows}
        columns={columns}
      />,
    );

    const nameHeader = screen.getByRole("button", { name: /Name/ });
    expect(nameHeader).toBeInstanceOf(HTMLButtonElement);
    expect(screen.getByRole("columnheader", { name: /Name/ })).toHaveAttribute(
      "aria-sort",
      "none",
    );
  });

  it("clicking a sortable header toggles sort and updates aria-sort", async () => {
    const user = userEvent.setup();
    render(
      <DataTable
        aria-label="Demo"
        data={rows}
        columns={columns}
      />,
    );

    const nameHeader = screen.getByRole("button", { name: /Name/ });
    await user.click(nameHeader);
    expect(screen.getByRole("columnheader", { name: /Name/ })).toHaveAttribute(
      "aria-sort",
      "ascending",
    );

    await user.click(nameHeader);
    expect(screen.getByRole("columnheader", { name: /Name/ })).toHaveAttribute(
      "aria-sort",
      "descending",
    );

    await user.click(nameHeader);
    expect(screen.getByRole("columnheader", { name: /Name/ })).toHaveAttribute(
      "aria-sort",
      "none",
    );
  });

  it("Enter on a focused sortable header also toggles sort (keyboard activation)", async () => {
    const user = userEvent.setup();
    render(
      <DataTable
        aria-label="Demo"
        data={rows}
        columns={columns}
      />,
    );

    const nameHeader = screen.getByRole("button", { name: /Name/ });
    nameHeader.focus();
    expect(nameHeader).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("columnheader", { name: /Name/ })).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
  });

  it("the table region carries the aria-label and a tbody shows the rows", () => {
    render(
      <DataTable
        aria-label="Demo region"
        data={rows}
        columns={columns}
      />,
    );
    const region = screen.getByRole("region", { name: "Demo region" });
    const tbody = within(region).getAllByRole("row")[0]?.parentElement;
    expect(tbody).toBeInTheDocument();
    expect(within(region).getByText("alpha")).toBeInTheDocument();
    expect(within(region).getByText("beta")).toBeInTheDocument();
    expect(within(region).getByText("gamma")).toBeInTheDocument();
  });

  it("has no axe a11y violations when populated", async () => {
    const { container } = render(
      <DataTable
        aria-label="A11y"
        data={rows}
        columns={columns}
      />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("has no axe violations in the empty state", async () => {
    const { container } = render(
      <DataTable
        aria-label="A11y empty"
        data={[]}
        columns={columns}
        emptyState="Nothing here."
      />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("has no axe violations in the loading state", async () => {
    const { container } = render(
      <DataTable
        aria-label="A11y loading"
        data={[]}
        columns={columns}
        loading
      />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});
