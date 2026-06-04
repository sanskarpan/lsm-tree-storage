import type { Meta, StoryObj } from "@storybook/react";

import { ScenarioLab } from "./ScenarioLab";
import type {
  BenchResult,
  EngineConfig,
  FeedLine,
  RuntimeState,
  ScenarioInfo,
} from "../../types";

const meta: Meta<typeof ScenarioLab> = {
  title: "Panels/ScenarioLab",
  component: ScenarioLab,
};

export default meta;
type Story = StoryObj<typeof meta>;

const config: EngineConfig = {
  DataDir: "/tmp/lsm",
  MemTableSize: 1_048_576,
  BlockSize: 4096,
  BloomBitsPerKey: 10,
  SSTMaxSize: 2_097_152,
  SyncWAL: true,
  MaxOpenFiles: 1000,
  BlockCacheSize: 8192,
  MaxLevels: 7,
  LevelSizeMultiplier: 10,
  Level0FileNumCompactionTrigger: 4,
  Level0StopWritesTrigger: 12,
  MaxImmutableMemTables: 2,
  CompactionStyle: "leveled",
  TimeWindowSize: 60,
};

const runtime: RuntimeState = {
  Open: true,
  DataDir: "/tmp/lsm",
  ActiveWALPath: "/tmp/lsm/000004.wal",
  ActiveLogNumber: 4,
  SyncWAL: true,
  CompactionStyle: "leveled",
};

const scenarios: ScenarioInfo[] = [
  { name: "uniform-writes", description: "Evenly spread write workload" },
  { name: "bursty-traffic", description: "Bursts of writes followed by reads" },
];

const benchmarkResult: BenchResult = {
  total_ops: 50_000,
  duration_ms: 4_050,
  ops_per_sec: 12_345,
  p50_write_us: 130,
  p99_write_us: 412,
  p50_read_us: 220,
  p99_read_us: 980,
};

const opsFeed: FeedLine[] = [
  { id: "1", label: "Benchmark complete", detail: "12345 ops/sec", tone: "good", timestamp: Date.now() - 5000 },
  { id: "2", label: "Scenario bursty-traffic", detail: "ok", tone: "accent", timestamp: Date.now() - 2000 },
];

const noop = async () => undefined;

export const Idle: Story = {
  args: {
    runtime,
    config,
    scenarios,
    benchmarkResult: null,
    closeMessage: null,
    opsFeed: [],
    onScenarioRun: noop,
    onBenchRun: noop,
    onCloseAttempt: noop,
  },
};

export const WithBenchmark: Story = {
  args: {
    runtime,
    config,
    scenarios,
    benchmarkResult,
    closeMessage: null,
    opsFeed,
    onScenarioRun: noop,
    onBenchRun: noop,
    onCloseAttempt: noop,
  },
};

export const CloseAttempted: Story = {
  args: {
    runtime,
    config,
    scenarios,
    benchmarkResult,
    closeMessage:
      "The /close endpoint is reserved for local lifecycle management; remote callers receive a 403.",
    opsFeed,
    onScenarioRun: noop,
    onBenchRun: noop,
    onCloseAttempt: noop,
  },
};
