import type { Meta, StoryObj } from "@storybook/react";

import { Select } from "./select";

const meta: Meta<typeof Select> = {
  title: "Primitives/Select",
  component: Select,
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    value: "leveled",
    onChange: () => undefined,
    placeholder: "Choose a style…",
    "aria-label": "Compaction style",
    options: [
      { value: "leveled", label: "Leveled" },
      { value: "size-tiered", label: "Size-tiered" },
      { value: "time-window", label: "Time window" },
    ],
  },
};

export const Disabled: Story = {
  args: {
    value: "leveled",
    onChange: () => undefined,
    disabled: true,
    options: [
      { value: "leveled", label: "Leveled" },
    ],
  },
};
