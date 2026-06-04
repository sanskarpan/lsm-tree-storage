import type { Meta, StoryObj } from "@storybook/react";

import { BloomTelemetry } from "./BloomTelemetry";

const meta: Meta<typeof BloomTelemetry> = {
  title: "Panels/BloomTelemetry",
  component: BloomTelemetry,
  decorators: [
    (Story) => (
      <div className="max-w-md">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof meta>;

const stats = [
  { file_id: 100, bits_per_key: 10, estimated_fp_rate: 0.0086, status: "ok" },
  { file_id: 101, bits_per_key: 10, estimated_fp_rate: 0.0086, status: "ok" },
  { file_id: 102, bits_per_key: 12, estimated_fp_rate: 0.002, status: "ok" },
  { file_id: 103, bits_per_key: 8, estimated_fp_rate: 0.038, status: "ok" },
];

export const Populated: Story = {
  args: { bloomStats: stats },
};

export const Empty: Story = {
  args: { bloomStats: [] },
};

export const HighFpr: Story = {
  args: {
    bloomStats: stats.map((s) => ({ ...s, estimated_fp_rate: 0.07, bits_per_key: 6 })),
  },
};
