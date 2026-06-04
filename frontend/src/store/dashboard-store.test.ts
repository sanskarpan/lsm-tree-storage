import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../lib/api", () => ({
  attemptRemoteClose: vi.fn(),
  deleteValue: vi.fn(),
  forceCompaction: vi.fn(),
  getBloomStat: vi.fn(),
  getCompactionStats: vi.fn(),
  getLevels: vi.fn(),
  getMemtableSnapshot: vi.fn(),
  getOpenState: vi.fn(),
  getValue: vi.fn(),
  getWalEntries: vi.fn(),
  listScenarios: vi.fn(),
  putValue: vi.fn(),
  runBench: vi.fn(),
  runScenario: vi.fn(),
  setCompactionStyle: vi.fn(),
}));

import * as api from "../lib/api";
import {
  teardownDashboardStore,
  useDashboardStore,
} from "./dashboard-store";

const mockedApi = vi.mocked(api);

function resetStore() {
  useDashboardStore.setState({
    runtime: null,
    stats: null,
    config: null,
    levels: [],
    walEntries: null,
    memtable: null,
    compactionStats: [],
    scenarios: [],
    bloomStats: [],
    connected: false,
    writeFeed: [],
    compactionFeed: [],
    opsFeed: [],
    sessionWrites: 0,
    sessionFlushes: 0,
    sessionCompactions: 0,
    amplification: { wa: 1, ra: 0, sa: 1 },
    ampHistory: [],
    activeCompaction: null,
    readTrace: null,
    queryPending: false,
    benchmarkResult: null,
    closeMessage: null,
    error: null,
    events: [],
  });
}

beforeEach(() => {
  resetStore();
  mockedApi.attemptRemoteClose.mockReset();
  mockedApi.deleteValue.mockReset();
  mockedApi.forceCompaction.mockReset();
  mockedApi.getBloomStat.mockReset();
  mockedApi.getCompactionStats.mockReset();
  mockedApi.getLevels.mockReset();
  mockedApi.getMemtableSnapshot.mockReset();
  mockedApi.getOpenState.mockReset();
  mockedApi.getValue.mockReset();
  mockedApi.getWalEntries.mockReset();
  mockedApi.listScenarios.mockReset();
  mockedApi.putValue.mockReset();
  mockedApi.runBench.mockReset();
  mockedApi.runScenario.mockReset();
  mockedApi.setCompactionStyle.mockReset();
});

afterEach(() => {
  teardownDashboardStore();
});

describe("refreshSnapshot", () => {
  it("populates snapshot fields on success", async () => {
    mockedApi.getOpenState.mockResolvedValue({
      status: "ok",
      state: { Open: true, DataDir: "/d", ActiveWALPath: "/d/4.wal", ActiveLogNumber: 4, SyncWAL: true, CompactionStyle: "leveled" },
      stats: { cache_hit_rate: 0.5, cache_hits: 1, cache_misses: 1, cache_size: 100, memtable_size: 10, num_immutables: 0, seq_no: 5, total_sst_bytes: 0, total_sst_files: 0, wal_files: 1 },
      config: { DataDir: "/d", MemTableSize: 1024, BlockSize: 4096, BloomBitsPerKey: 10, SSTMaxSize: 1024, SyncWAL: true, MaxOpenFiles: 100, BlockCacheSize: 100, MaxLevels: 7, LevelSizeMultiplier: 10, Level0FileNumCompactionTrigger: 4, Level0StopWritesTrigger: 12, MaxImmutableMemTables: 2, CompactionStyle: "leveled", TimeWindowSize: 60 },
    });
    mockedApi.getLevels.mockResolvedValue([]);
    mockedApi.getWalEntries.mockResolvedValue({ count: 0, entries: [], limit: 30, state: { Open: true, DataDir: "/d", ActiveWALPath: "/d/4.wal", ActiveLogNumber: 4, SyncWAL: true, CompactionStyle: "leveled" } });
    mockedApi.getMemtableSnapshot.mockResolvedValue({ mutable: { approximate_size: 0, wal_seq_no: 0, truncated: false, entries: [] }, immutables: [], active_wal_path: "/d/4.wal", active_log_number: 4, limit: 18 });
    mockedApi.getCompactionStats.mockResolvedValue([]);
    mockedApi.listScenarios.mockResolvedValue([]);

    await useDashboardStore.getState().refreshSnapshot();

    const state = useDashboardStore.getState();
    expect(state.runtime?.ActiveLogNumber).toBe(4);
    expect(state.stats?.seq_no).toBe(5);
    expect(state.config?.MemTableSize).toBe(1024);
    expect(state.error).toBeNull();
  });

  it("captures an error message on failure", async () => {
    mockedApi.getOpenState.mockRejectedValue(new Error("snapshot down"));
    mockedApi.getLevels.mockResolvedValue([]);
    mockedApi.getWalEntries.mockResolvedValue({ count: 0, entries: [], limit: 30, state: { Open: true, DataDir: "/d", ActiveWALPath: "/d/4.wal", ActiveLogNumber: 4, SyncWAL: true, CompactionStyle: "leveled" } });
    mockedApi.getMemtableSnapshot.mockResolvedValue({ mutable: { approximate_size: 0, wal_seq_no: 0, truncated: false, entries: [] }, immutables: [], active_wal_path: "/d/4.wal", active_log_number: 4, limit: 18 });
    mockedApi.getCompactionStats.mockResolvedValue([]);
    mockedApi.listScenarios.mockResolvedValue([]);

    await useDashboardStore.getState().refreshSnapshot();

    expect(useDashboardStore.getState().error).toBe("snapshot down");
  });
});

describe("recordEvent", () => {
  it("increments sessionWrites and updates stats on wal.append", () => {
    useDashboardStore.setState({
      stats: { cache_hit_rate: 0, cache_hits: 0, cache_misses: 0, cache_size: 0, memtable_size: 0, num_immutables: 0, seq_no: 0, total_sst_bytes: 0, total_sst_files: 0, wal_files: 0 },
    });
    useDashboardStore.getState().recordEvent({
      type: "wal.append",
      extra: { seq_no: 42, key: "k", type: 1 },
    });
    const state = useDashboardStore.getState();
    expect(state.sessionWrites).toBe(1);
    expect(state.stats?.seq_no).toBe(42);
    expect(state.writeFeed[0]?.label).toBe("PUT k");
  });

  it("increments cache_hits / cache_misses", () => {
    useDashboardStore.setState({
      stats: { cache_hit_rate: 0, cache_hits: 0, cache_misses: 0, cache_size: 0, memtable_size: 0, num_immutables: 0, seq_no: 0, total_sst_bytes: 0, total_sst_files: 0, wal_files: 0 },
    });
    useDashboardStore.getState().recordEvent({ type: "cache.hit" });
    useDashboardStore.getState().recordEvent({ type: "cache.miss" });
    useDashboardStore.getState().recordEvent({ type: "cache.miss" });
    const stats = useDashboardStore.getState().stats;
    expect(stats?.cache_hits).toBe(1);
    expect(stats?.cache_misses).toBe(2);
  });

  it("sets activeCompaction on compaction.start and clears it on complete", () => {
    useDashboardStore.getState().recordEvent({
      type: "compaction.start",
      extra: { input_level: 0, output_level: 1, num_inputs: 4 },
    });
    expect(useDashboardStore.getState().activeCompaction?.inputLevel).toBe(0);
    expect(useDashboardStore.getState().activeCompaction?.outputLevel).toBe(1);

    useDashboardStore.getState().recordEvent({ type: "compaction.complete" });
    expect(useDashboardStore.getState().activeCompaction).toBeNull();
    expect(useDashboardStore.getState().sessionCompactions).toBe(1);
  });

  it("appends amplification samples to ampHistory", () => {
    useDashboardStore.getState().recordEvent({
      type: "amplification",
      extra: { wa: 3, ra: 1, sa: 1.5 },
    });
    const state = useDashboardStore.getState();
    expect(state.amplification).toEqual({ wa: 3, ra: 1, sa: 1.5 });
    expect(state.ampHistory).toHaveLength(1);
    expect(state.ampHistory[0]).toMatchObject({ wa: 3, ra: 1, sa: 1.5 });
  });

  it("ignores events with no handler", () => {
    const before = useDashboardStore.getState();
    useDashboardStore.getState().recordEvent({ type: "unknown.event" });
    const after = useDashboardStore.getState();
    expect(after.writeFeed).toEqual(before.writeFeed);
    expect(after.sessionWrites).toBe(0);
  });
});

describe("actions", () => {
  it("handlePut calls api.putValue, logs, refreshes", async () => {
    mockedApi.putValue.mockResolvedValue(undefined);
    mockedApi.getOpenState.mockResolvedValue({ status: "ok", state: { Open: true, DataDir: "/d", ActiveWALPath: "/d/4.wal", ActiveLogNumber: 4, SyncWAL: true, CompactionStyle: "leveled" }, stats: { cache_hit_rate: 0, cache_hits: 0, cache_misses: 0, cache_size: 0, memtable_size: 0, num_immutables: 0, seq_no: 0, total_sst_bytes: 0, total_sst_files: 0, wal_files: 0 }, config: { DataDir: "/d", MemTableSize: 1024, BlockSize: 4096, BloomBitsPerKey: 10, SSTMaxSize: 1024, SyncWAL: true, MaxOpenFiles: 100, BlockCacheSize: 100, MaxLevels: 7, LevelSizeMultiplier: 10, Level0FileNumCompactionTrigger: 4, Level0StopWritesTrigger: 12, MaxImmutableMemTables: 2, CompactionStyle: "leveled", TimeWindowSize: 60 } });
    mockedApi.getLevels.mockResolvedValue([]);
    mockedApi.getWalEntries.mockResolvedValue({ count: 0, entries: [], limit: 30, state: { Open: true, DataDir: "/d", ActiveWALPath: "/d/4.wal", ActiveLogNumber: 4, SyncWAL: true, CompactionStyle: "leveled" } });
    mockedApi.getMemtableSnapshot.mockResolvedValue({ mutable: { approximate_size: 0, wal_seq_no: 0, truncated: false, entries: [] }, immutables: [], active_wal_path: "/d/4.wal", active_log_number: 4, limit: 18 });
    mockedApi.getCompactionStats.mockResolvedValue([]);
    mockedApi.listScenarios.mockResolvedValue([]);

    await useDashboardStore.getState().handlePut("k", "v");

    expect(mockedApi.putValue).toHaveBeenCalledWith("k", "v");
    expect(useDashboardStore.getState().opsFeed[0]?.label).toBe("Accepted PUT k");
  });

  it("handleDelete calls api.deleteValue and logs", async () => {
    mockedApi.deleteValue.mockResolvedValue(undefined);
    mockedApi.getOpenState.mockResolvedValue({ status: "ok", state: { Open: true, DataDir: "/d", ActiveWALPath: "/d/4.wal", ActiveLogNumber: 4, SyncWAL: true, CompactionStyle: "leveled" }, stats: { cache_hit_rate: 0, cache_hits: 0, cache_misses: 0, cache_size: 0, memtable_size: 0, num_immutables: 0, seq_no: 0, total_sst_bytes: 0, total_sst_files: 0, wal_files: 0 }, config: { DataDir: "/d", MemTableSize: 1024, BlockSize: 4096, BloomBitsPerKey: 10, SSTMaxSize: 1024, SyncWAL: true, MaxOpenFiles: 100, BlockCacheSize: 100, MaxLevels: 7, LevelSizeMultiplier: 10, Level0FileNumCompactionTrigger: 4, Level0StopWritesTrigger: 12, MaxImmutableMemTables: 2, CompactionStyle: "leveled", TimeWindowSize: 60 } });
    mockedApi.getLevels.mockResolvedValue([]);
    mockedApi.getWalEntries.mockResolvedValue({ count: 0, entries: [], limit: 30, state: { Open: true, DataDir: "/d", ActiveWALPath: "/d/4.wal", ActiveLogNumber: 4, SyncWAL: true, CompactionStyle: "leveled" } });
    mockedApi.getMemtableSnapshot.mockResolvedValue({ mutable: { approximate_size: 0, wal_seq_no: 0, truncated: false, entries: [] }, immutables: [], active_wal_path: "/d/4.wal", active_log_number: 4, limit: 18 });
    mockedApi.getCompactionStats.mockResolvedValue([]);
    mockedApi.listScenarios.mockResolvedValue([]);

    await useDashboardStore.getState().handleDelete("k");

    expect(mockedApi.deleteValue).toHaveBeenCalledWith("k");
    expect(useDashboardStore.getState().opsFeed[0]?.label).toBe("Accepted DELETE k");
  });

  it("handleForceCompaction calls api and logs", async () => {
    mockedApi.forceCompaction.mockResolvedValue(undefined);

    await useDashboardStore.getState().handleForceCompaction();

    expect(mockedApi.forceCompaction).toHaveBeenCalledWith(0);
    expect(useDashboardStore.getState().compactionFeed[0]?.label).toBe(
      "Manual L0 compaction requested",
    );
  });

  it("handleBenchRun records result and ops feed", async () => {
    mockedApi.runBench.mockResolvedValue({
      total_ops: 1000,
      duration_ms: 100,
      ops_per_sec: 10_000,
      p50_write_us: 50,
      p99_write_us: 200,
      p50_read_us: 60,
      p99_read_us: 250,
    });

    await useDashboardStore.getState().handleBenchRun({
      type: "sequential_write",
      num_keys: 1000,
      value_size: 128,
    });

    const state = useDashboardStore.getState();
    expect(state.benchmarkResult?.ops_per_sec).toBe(10_000);
    expect(state.opsFeed[0]?.label).toBe("Benchmark complete");
  });

  it("handleCloseAttempt records server reason", async () => {
    mockedApi.attemptRemoteClose.mockResolvedValue({
      status: "forbidden",
      reason: "Remote close is reserved for local lifecycle management",
      supported: false,
    });

    await useDashboardStore.getState().handleCloseAttempt();

    expect(useDashboardStore.getState().closeMessage).toBe(
      "Remote close is reserved for local lifecycle management",
    );
  });
});
