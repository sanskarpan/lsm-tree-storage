import type { Meta, StoryObj } from "@storybook/react";

import { Progress } from "./progress";

const meta: Meta<typeof Progress> = {
  title: "Primitives/Progress",
  component: Progress,
  argTypes: {
    value: { control: { type: "range", min: 0, max: 100 } },
    tone: {
      control: "select",
      options: ["default", "success", "warning", "danger"],
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { value: 50, label: "Memtable fill" },
};

export const Tones: Story = {
  render: () => (
    <div className="flex w-80 flex-col gap-3">
      <Progress value={25} label="Memtable" />
      <Progress value={50} tone="success" label="Cache hit" />
      <Progress value={75} tone="warning" label="Backlog" />
      <Progress value={95} tone="danger" label="Capacity" />
    </div>
  ),
};
