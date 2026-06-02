import type { Meta, StoryObj } from "@storybook/react";

import { Skeleton } from "./skeleton";

const meta: Meta<typeof Skeleton> = {
  title: "Primitives/Skeleton",
  component: Skeleton,
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { className: "h-12 w-48" },
};

export const List: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-6 w-full" />
      ))}
    </div>
  ),
};
