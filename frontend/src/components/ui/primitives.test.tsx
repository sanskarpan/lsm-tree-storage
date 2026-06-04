import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { axe, toHaveNoViolations } from "jest-axe";

import { Banner } from "./banner";
import { Button } from "./button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./card";
import { DataTable } from "./data-table";
import { Input } from "./input";
import { Label } from "./label";
import { Badge } from "./badge";
import { Progress } from "./progress";
import { Select } from "./select";

expect.extend(toHaveNoViolations);

describe("primitives a11y", () => {
  it("Button has no violations", async () => {
    const { container } = render(<Button>Apply</Button>);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("Input with Label has no violations", async () => {
    const { container } = render(
      <div>
        <Label htmlFor="k">Key</Label>
        <Input id="k" placeholder="key" />
      </div>,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("Card has no violations", async () => {
    const { container } = render(
      <Card>
        <CardHeader>
          <CardTitle>Title</CardTitle>
          <CardDescription>Sub</CardDescription>
        </CardHeader>
        <CardContent>Body</CardContent>
      </Card>,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("Badge has no violations", async () => {
    const { container } = render(<Badge>tag</Badge>);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("Progress exposes aria-valuenow and has no violations", async () => {
    const { container } = render(<Progress value={42} label="Memtable fill" />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("Select with aria-label has no violations", async () => {
    const { container } = render(
      <Select
        aria-label="Compaction style"
        value="leveled"
        onChange={() => undefined}
        options={[
          { value: "leveled", label: "Leveled" },
          { value: "size-tiered", label: "Size-tiered" },
        ]}
      />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("Banner danger tone has no violations", async () => {
    const { container } = render(
      <Banner tone="danger">Snapshot endpoint returned 500.</Banner>,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("DataTable with rows has no violations", async () => {
    type Row = { name: string; role: string };
    const { container } = render(
      <DataTable<Row>
        aria-label="Demo rows"
        data={[
          { name: "L0", role: "Write batch" },
          { name: "WAL", role: "Write-ahead" },
        ]}
        emptyState="empty"
        columns={[
          { accessorKey: "name", header: "Name" },
          { accessorKey: "role", header: "Role" },
        ]}
      />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});
