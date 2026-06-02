import type { Meta, StoryObj } from "@storybook/react";

import { Label } from "./label";

const meta: Meta<typeof Label> = {
  title: "Primitives/Label",
  component: Label,
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { children: "Key" },
};
