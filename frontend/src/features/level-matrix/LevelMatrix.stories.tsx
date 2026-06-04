import type { Meta, StoryObj } from "@storybook/react";

import { LevelMatrix } from "./LevelMatrix";
import type {
  CompactionLevelStat,
  LevelInfo,
  MemtableSnapshotResponse,
} from "../../types";

const meta: Meta<typeof LevelMatrix> = {
  title: "Panels/LevelMatrix",
  component: LevelMatrix,
  decorators: [
    (Story) => (
      <div className="max-w-3xl">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof meta>;

const levels: LevelInfo[] = [
  {
    level: 0,
    num_files: 4,
    total_size: 4_096_000,
    files: [
      { file_id: 100, first_key: "a", last_key: "g", file_size: 1_024_000, num_keys: 1024 },
      { file_id: 101, first_key: "h", last_key: "n", file_size: 1_024_000, num_keys: 1024 },
      { file_id: 102, first_key: "o", last_key: "u", file_size: 1_024_000, num_keys: 1024 },
      { file_id: 103, first_key: "v", last_key: "z", file_size: 1_024_000, num_keys: 1024 },
    ],
  },
  {
    level: 1,
    num_files: 1,
    total_size: 8_192_000,
    files: [{ file_id: 90, first_key: "a", last_key: "z", file_size: 8_192_000, num_keys: 8192 }],
  },
  {
    level: 2,
    num_files: 2,
    total_size: 16_384_000,
    files: [
      { file_id: 80, first_key: "a", last_key: "m", file_size: 8_192_000, num_keys: 8192 },
      { file_id: 81, first_key: "n", last_key: "z", file_size: 8_192_000, num_keys: 8192 },
    ],
  },
];

const compactionStats: CompactionLevelStat[] = levels.map((level) => ({
  level: level.level,
  num_files: level.num_files,
  total_size: level.total_size,
}));

const memtable: MemtableSnapshotResponse = {
  active_log_number: 4,
  active_wal_path: "/tmp/lsm/000004.wal",
  limit: 18,
  immutables: [
    {
      log_number: 3,
      wal_path: "/tmp/lsm/000003.wal",
      table: {
        approximate_size: 1024,
        wal_seq_no: 50,
        truncated: false,
        entries: [{ key: "im-a", value: "v", seq_no: 50, type: "put" }],
      },
    },
  ],
  mutable: {
    approximate_size: 512,
    wal_seq_no: 102,
    truncated: false,
    entries: [],
  },
};

const noop = async () => undefined;

export const Populated: Story = {
  args: {
    levels,
    memtable,
    compactionStats,
    onRefresh: noop,
  },
};

export const Empty: Story = {
  args: {
    levels: [],
    memtable: null,
    compactionStats: [],
    onRefresh: noop,
  },
};

export const L0Hot: Story = {
  args: {
    levels: levels.map((l) =>
      l.level === 0 ? { ...l, num_files: 8, total_size: 8_192_000 } : l,
    ),
    memtable,
    compactionStats: compactionStats.map((s) =>
      s.level === 0 ? { ...s, num_files: 8, total_size: 8_192_000 } : s,
    ),
    onRefresh: noop,
  },
};
