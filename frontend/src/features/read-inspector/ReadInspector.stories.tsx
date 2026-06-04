import type { Meta, StoryObj } from "@storybook/react";

import { ReadInspector } from "./ReadInspector";
import type { ReadTraceReport } from "../../types";

const meta: Meta<typeof ReadInspector> = {
  title: "Panels/ReadInspector",
  component: ReadInspector,
  decorators: [
    (Story) => (
      <div className="max-w-xl">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof meta>;

const trace: ReadTraceReport = {
  key: "live-a",
  found: true,
  value: "alpha",
  status: 200,
  steps: [
    'Started lookup for "live-a"',
    "Consulted bloom filter before disk read",
    "Checked mutable or immutable memtable",
    "Visited SSTable level 2",
  ],
  bloomChecks: 3,
  bloomMisses: 0,
  memtableHits: 1,
  sstableHits: 1,
  generatedAt: Date.now(),
};

const noop = async () => undefined;

export const Idle: Story = {
  args: { pending: false, trace: null, onInspect: noop },
};

export const Pending: Story = {
  args: { pending: true, trace: null, onInspect: noop },
};

export const Found: Story = {
  args: { pending: false, trace, onInspect: noop },
};

export const Missing: Story = {
  args: {
    pending: false,
    trace: { ...trace, found: false, value: undefined, sstableHits: 0 },
    onInspect: noop,
  },
};
