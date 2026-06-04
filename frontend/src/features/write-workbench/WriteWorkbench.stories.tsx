import type { Meta, StoryObj } from "@storybook/react";

import { WriteWorkbench } from "./WriteWorkbench";
import type {
  FeedLine,
  MemtableSnapshotResponse,
  WalEntriesResponse,
} from "../../types";

const meta: Meta<typeof WriteWorkbench> = {
  title: "Panels/WriteWorkbench",
  component: WriteWorkbench,
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

const baseMemtable: MemtableSnapshotResponse = {
  active_log_number: 4,
  active_wal_path: "/tmp/lsm/000004.wal",
  immutables: [],
  limit: 18,
  mutable: {
    approximate_size: 4096,
    wal_seq_no: 102,
    truncated: false,
    entries: [
      { key: "live-a", value: "alpha", seq_no: 100, type: "put" },
      { key: "live-b", value: "beta", seq_no: 101, type: "put" },
      { key: "live-c", value: "gamma", seq_no: 102, type: "put" },
    ],
  },
};

const runtimeState = {
  Open: true,
  DataDir: "/tmp/lsm",
  ActiveWALPath: "/tmp/lsm/000004.wal",
  ActiveLogNumber: 4,
  SyncWAL: true,
  CompactionStyle: "leveled",
} as const;

const populatedWal: WalEntriesResponse = {
  count: 4,
  limit: 30,
  state: runtimeState,
  entries: [
    { type: "put", key: "live-a", seq_no: 100, timestamp_unix_nano: Date.now() - 30_000_000_000 },
    { type: "put", key: "live-b", seq_no: 101, timestamp_unix_nano: Date.now() - 25_000_000_000 },
    { type: "delete", key: "live-c", seq_no: 102, timestamp_unix_nano: Date.now() - 20_000_000_000 },
    { type: "sync", key: "", seq_no: 0, timestamp_unix_nano: Date.now() - 10_000_000_000 },
  ],
};

const writeFeed: FeedLine[] = [
  { id: "1", label: "PUT live-a", detail: "seq 100", tone: "good", timestamp: Date.now() - 30_000 },
  { id: "2", label: "WAL sync", detail: "fsync completed", tone: "accent", timestamp: Date.now() - 10_000 },
];

const noop = async () => undefined;

export const Populated: Story = {
  args: {
    capacityBytes: 16_384,
    memtable: baseMemtable,
    walEntries: populatedWal,
    writeFeed,
    onPut: noop,
    onDelete: noop,
  },
};

export const Empty: Story = {
  args: {
    capacityBytes: 16_384,
    memtable: null,
    walEntries: null,
    writeFeed: [],
    onPut: noop,
    onDelete: noop,
  },
};

export const NearFull: Story = {
  args: {
    capacityBytes: 4096,
    memtable: {
      ...baseMemtable,
      mutable: { ...baseMemtable.mutable, approximate_size: 3500 },
    },
    walEntries: populatedWal,
    writeFeed,
    onPut: noop,
    onDelete: noop,
  },
};
