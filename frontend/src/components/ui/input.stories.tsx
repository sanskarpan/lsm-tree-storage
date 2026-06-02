import type { Meta, StoryObj } from "@storybook/react";
import { Mail, Search } from "lucide-react";

import { Button } from "./button";
import { Input } from "./input";
import { Label } from "./label";

const meta: Meta<typeof Input> = {
  title: "Primitives/Input",
  component: Input,
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { placeholder: "key" },
};

export const WithLabel: Story = {
  render: () => (
    <div className="flex w-72 flex-col gap-1">
      <Label htmlFor="demo-input">Key</Label>
      <Input id="demo-input" placeholder="value" />
    </div>
  ),
};

export const WithIcon: Story = {
  render: () => (
    <div className="relative w-72">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-[var(--fg-muted)]" />
      <Input className="pl-8" placeholder="Search..." />
    </div>
  ),
};

export const WithButton: Story = {
  render: () => (
    <form className="flex w-80 gap-2" onSubmit={(e) => e.preventDefault()}>
      <Input placeholder="value" />
      <Button type="submit">PUT</Button>
    </form>
  ),
};
