import type { Meta, StoryObj } from "@storybook/react";

import { CompactionStudio } from "./CompactionStudio";
import type {
  ActiveCompaction,
  CompactionLevelStat,
  FeedLine,
} from "../../types";

const meta: Meta<typeof CompactionStudio> = {
  title: "Panels/CompactionStudio",
  component: CompactionStudio,
  decorators: [
    (Story) => (
      <div className="max-w-2xl">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof meta>;

const stats: CompactionLevelStat[] = [
  { level: 0, num_files: 4, total_size: 4_096_000 },
  { level: 1, num_files: 1, total_size: 8_192_000 },
  { level: 2, num_files: 2, total_size: 16_384_000 },
];

const activeCompaction: ActiveCompaction = {
  inputLevel: 0,
  outputLevel: 1,
  startedAt: Date.now() - 3500,
};

const feed: FeedLine[] = [
  { id: "1", label: "Compaction L0 -> L1", detail: "4 files selected", tone: "accent", timestamp: Date.now() - 5000 },
  { id: "2", label: "Merge step", detail: "k-42", tone: "info", timestamp: Date.now() - 2000 },
];

const noop = async () => undefined;

export const Idle: Story = {
  args: {
    activeCompaction: null,
    compactionFeed: [],
    compactionStats: stats,
    currentStyle: "leveled",
    onForceCompaction: noop,
    onStyleChange: noop,
  },
};

export const Running: Story = {
  args: {
    activeCompaction,
    compactionFeed: feed,
    compactionStats: stats,
    currentStyle: "leveled",
    onForceCompaction: noop,
    onStyleChange: noop,
  },
};

export const SizeTiered: Story = {
  args: {
    activeCompaction: null,
    compactionFeed: [],
    compactionStats: stats,
    currentStyle: "size-tiered",
    onForceCompaction: noop,
    onStyleChange: noop,
  },
};
