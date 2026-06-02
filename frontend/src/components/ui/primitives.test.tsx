import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { axe, toHaveNoViolations } from "jest-axe";

import { Button } from "./button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./card";
import { Input } from "./input";
import { Label } from "./label";
import { Badge } from "./badge";

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
});
