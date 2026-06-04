import { create } from "zustand";

import {
  attemptRemoteClose,
  deleteValue,
  forceCompaction,
  getBloomStat,
  getCompactionStats,
  getLevels,
  getMemtableSnapshot,
  getOpenState,
  getValue,
  getWalEntries,
  listScenarios,
  putValue,
  runBench,
  runScenario,
  setCompactionStyle,
} from "../lib/api";
import { line, limitLines, num, str, type TimedEvent } from "../lib/dashboard/feed";
import type {
  ActiveCompaction,
  AmpPoint,
  BenchRequest,
  BenchResult,
  BloomStat,
  CompactionLevelStat,
  CompactionStyle,
  EngineConfig,
  EngineEvent,
  EngineStats,
  FeedLine,
  LevelInfo,
  MemtableSnapshotResponse,
  ReadTraceReport,
  RuntimeState,
  ScenarioInfo,
  WalEntriesResponse,
} from "../types";

const WS_URL = `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/ws`;

export interface AmplificationSnapshot {
  wa: number;
  ra: number;
  sa: number;
}

export interface DashboardState {
  runtime: RuntimeState | null;
  stats: EngineStats | null;
  config: EngineConfig | null;
  levels: LevelInfo[];
  walEntries: WalEntriesResponse | null;
  memtable: MemtableSnapshotResponse | null;
  compactionStats: CompactionLevelStat[];
  scenarios: ScenarioInfo[];
  bloomStats: BloomStat[];

  connected: boolean;
  writeFeed: FeedLine[];
  compactionFeed: FeedLine[];
  opsFeed: FeedLine[];
  sessionWrites: number;
  sessionFlushes: number;
  sessionCompactions: number;
  amplification: AmplificationSnapshot;
  ampHistory: AmpPoint[];
  activeCompaction: ActiveCompaction | null;

  readTrace: ReadTraceReport | null;
  queryPending: boolean;
  benchmarkResult: BenchResult | null;
  closeMessage: string | null;

  error: string | null;
  events: TimedEvent[];

  refreshSnapshot: () => Promise<void>;
  setStats: (updater: (current: EngineStats | null) => EngineStats | null) => void;
  setError: (next: string | null) => void;
  addOpsFeed: (tone: FeedLine["tone"], label: string, detail?: string) => void;
  addCompactionFeed: (tone: FeedLine["tone"], label: string, detail?: string) => void;

  handlePut: (key: string, value: string) => Promise<void>;
  handleDelete: (key: string) => Promise<void>;
  handleStyleChange: (style: CompactionStyle) => Promise<void>;
  handleForceCompaction: () => Promise<void>;
  handleScenarioRun: (name: string) => Promise<void>;
  handleBenchRun: (request: BenchRequest) => Promise<void>;
  handleCloseAttempt: () => Promise<void>;
  runReadTrace: (key: string) => Promise<void>;
  recordEvent: (event: EngineEvent) => void;
}

export const useDashboardStore = create<DashboardState>((set, get) => ({
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

  setStats: (updater) =>
    set((state) => ({ stats: updater(state.stats) })),
  setError: (next) => set({ error: next }),

  addOpsFeed: (tone, label, detail) =>
    set((state) => ({
      opsFeed: limitLines(state.opsFeed, line(tone, label, detail)),
    })),
  addCompactionFeed: (tone, label, detail) =>
    set((state) => ({
      compactionFeed: limitLines(state.compactionFeed, line(tone, label, detail)),
    })),

  refreshSnapshot: async () => {
    try {
      const [openState, levelState, walState, memState, compactionState, scenarioState] =
        await Promise.all([
          getOpenState(),
          getLevels(),
          getWalEntries(30),
          getMemtableSnapshot(18),
          getCompactionStats(),
          listScenarios(),
        ]);

      set({
        runtime: openState.state,
        stats: openState.stats,
        config: openState.config,
        levels: levelState,
        walEntries: walState,
        memtable: memState,
        compactionStats: compactionState,
        scenarios: scenarioState,
      });

      const fileIds: number[] = [];
      for (const level of levelState) {
        for (const file of level.files) {
          fileIds.push(file.file_id);
          if (fileIds.length >= 10) break;
        }
        if (fileIds.length >= 10) break;
      }

      const bloomResults = await Promise.allSettled(
        fileIds.map((fileId) => getBloomStat(fileId)),
      );
      const nextBloomStats = bloomResults
        .flatMap((result) => (result.status === "fulfilled" ? [result.value] : []))
        .sort((a, b) => a.file_id - b.file_id);

      set({ bloomStats: nextBloomStats, error: null });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : String(err),
      });
    }
  },

  recordEvent: (event) => {
    const now = Date.now();
    set((state) => ({
      events: [{ receivedAt: now, event }, ...state.events].slice(0, 200),
    }));

    const extra = event.extra ?? {};

    switch (event.type) {
      case "wal.append": {
        const seq = num(extra.seq_no) ?? num(extra.seq);
        const key = str(extra.key) ?? "(unknown)";
        const kind = num(extra.type) === 2 ? "DELETE" : "PUT";
        const detail = seq != null ? `seq ${seq}` : undefined;
        get().setStats((current) =>
          seq != null && current ? { ...current, seq_no: seq } : current,
        );
        set((state) => ({
          writeFeed: limitLines(
            state.writeFeed,
            line(kind === "DELETE" ? "warn" : "good", `${kind} ${key}`, detail),
          ),
          sessionWrites: state.sessionWrites + 1,
        }));
        break;
      }
      case "wal.sync":
        set((state) => ({
          writeFeed: limitLines(state.writeFeed, line("accent", "WAL sync", "fsync completed")),
        }));
        break;
      case "flush.start":
        get().addOpsFeed("warn", "Flush started", str(extra.file_id));
        set((state) => ({ sessionFlushes: state.sessionFlushes + 1 }));
        break;
      case "flush.complete":
        get().addOpsFeed("good", "Flush complete", `file #${num(extra.file_id) ?? "?"}`);
        void get().refreshSnapshot();
        break;
      case "compaction.start": {
        get().addCompactionFeed(
          "accent",
          `Compaction L${num(extra.input_level) ?? 0} -> L${num(extra.output_level) ?? 1}`,
          `${num(extra.num_inputs) ?? 0} files selected`,
        );
        set({
          activeCompaction: {
            inputLevel: num(extra.input_level) ?? 0,
            outputLevel: num(extra.output_level) ?? 1,
            startedAt: now,
          },
        });
        break;
      }
      case "compaction.complete":
        get().addCompactionFeed("good", "Compaction complete");
        set((state) => ({
          sessionCompactions: state.sessionCompactions + 1,
          activeCompaction: null,
        }));
        void get().refreshSnapshot();
        break;
      case "compaction.merge":
        get().addCompactionFeed("info", "Merge step", str(extra.key));
        break;
      case "tombstone.dropped":
        get().addCompactionFeed("warn", "Tombstone dropped", str(extra.key));
        break;
      case "amplification": {
        const next = {
          wa: num(extra.wa) ?? get().amplification.wa,
          ra: num(extra.ra) ?? get().amplification.ra,
          sa: num(extra.sa) ?? get().amplification.sa,
        };
        set((state) => ({
          amplification: next,
          ampHistory: [...state.ampHistory.slice(-23), { ...next, timestamp: now }],
        }));
        break;
      }
      case "cache.hit":
        get().setStats((current) =>
          current ? { ...current, cache_hits: current.cache_hits + 1 } : current,
        );
        break;
      case "cache.miss":
        get().setStats((current) =>
          current ? { ...current, cache_misses: current.cache_misses + 1 } : current,
        );
        break;
      default:
        break;
    }
  },

  handlePut: async (key, value) => {
    await putValue(key, value);
    get().addOpsFeed("good", `Accepted PUT ${key}`, value);
    await get().refreshSnapshot();
  },

  handleDelete: async (key) => {
    await deleteValue(key);
    get().addOpsFeed("warn", `Accepted DELETE ${key}`);
    await get().refreshSnapshot();
  },

  handleStyleChange: async (style) => {
    await setCompactionStyle(style);
    get().addCompactionFeed("accent", `Compaction style -> ${style}`);
    await get().refreshSnapshot();
  },

  handleForceCompaction: async () => {
    await forceCompaction(0);
    get().addCompactionFeed("accent", "Manual L0 compaction requested");
  },

  handleScenarioRun: async (name) => {
    const result = await runScenario(name);
    get().addOpsFeed("accent", `Scenario ${result.scenario}`, result.status);
    await get().refreshSnapshot();
  },

  handleBenchRun: async (request) => {
    const result = await runBench(request);
    get().addOpsFeed(
      "info",
      "Benchmark complete",
      `${Math.round(result.ops_per_sec)} ops/sec`,
    );
    set({ benchmarkResult: result });
  },

  handleCloseAttempt: async () => {
    const result = await attemptRemoteClose();
    set({ closeMessage: result.reason });
  },

  runReadTrace: async (key) => {
    set({ queryPending: true });
    try {
      const startedAt = Date.now();
      const response = await getValue(key);
      await new Promise((resolve) => setTimeout(resolve, 160));

      const { events, memtable, levels } = get();
      const relevant = events
        .filter((entry) => entry.receivedAt >= startedAt - 40)
        .map((entry) => entry.event)
        .filter((event) =>
          event.type === "read.start" ||
          event.type === "read.memtable" ||
          event.type === "read.sstable" ||
          event.type === "bloom.check" ||
          event.type === "bloom.hit" ||
          event.type === "bloom.miss",
        );

      const steps: string[] = [];
      let bloomChecks = 0;
      let bloomMisses = 0;
      let memtableHits = 0;
      let sstableHits = 0;

      for (const event of relevant) {
        switch (event.type) {
          case "read.start":
            steps.push(`Started lookup for "${key}"`);
            break;
          case "read.memtable":
            memtableHits += 1;
            steps.push("Checked mutable or immutable memtable");
            break;
          case "read.sstable":
            sstableHits += 1;
            steps.push(`Visited SSTable level ${num(event.extra?.level) ?? "?"}`);
            break;
          case "bloom.check":
            bloomChecks += 1;
            steps.push("Consulted bloom filter before disk read");
            break;
          case "bloom.miss":
            bloomMisses += 1;
            steps.push("Bloom filter short-circuited a miss");
            break;
          case "bloom.hit":
            steps.push("Bloom filter admitted a possible hit");
            break;
          default:
            break;
        }
      }

      if (steps.length === 0) {
        const mutableKeys = memtable?.mutable.entries.map((entry) => entry.key) ?? [];
        if (mutableKeys.includes(key)) {
          steps.push("Key is present in the current memtable snapshot");
          memtableHits = 1;
        } else {
          const containingLevel = levels.find((level) =>
            level.files.some((file) => file.first_key <= key && key <= file.last_key),
          );
          if (containingLevel) {
            steps.push(`Key falls inside a tracked L${containingLevel.level} SSTable range`);
          } else {
            steps.push(
              "No live event trace was captured; key is outside current memtable and visible SSTable ranges",
            );
          }
        }
      }

      set({
        readTrace: {
          key,
          found: response.body.found,
          value: response.body.value,
          status: response.status,
          steps,
          bloomChecks,
          bloomMisses,
          memtableHits,
          sstableHits,
          generatedAt: Date.now(),
        },
        error: null,
      });
    } catch (err) {
      set({ error: err instanceof Error ? err.message : String(err) });
    } finally {
      set({ queryPending: false });
    }
  },
}));

let initStarted = false;
let cleanupInit: (() => void) | null = null;

export function initDashboardStore() {
  if (initStarted) return;
  initStarted = true;

  const { refreshSnapshot, recordEvent } = useDashboardStore.getState();

  void refreshSnapshot();
  const interval = window.setInterval(() => {
    void refreshSnapshot();
  }, 5000);

  let retryTimer = 0;
  let socket: WebSocket | null = null;

  const connect = () => {
    socket = new WebSocket(WS_URL);
    socket.onopen = () => useDashboardStore.setState({ connected: true });
    socket.onclose = () => {
      useDashboardStore.setState({ connected: false });
      retryTimer = window.setTimeout(connect, 1500);
    };
    socket.onerror = () => socket?.close();
    socket.onmessage = (message) => {
      try {
        recordEvent(JSON.parse(message.data) as EngineEvent);
      } catch {
        // Ignore malformed frames.
      }
    };
  };

  connect();

  cleanupInit = () => {
    window.clearInterval(interval);
    window.clearTimeout(retryTimer);
    socket?.close();
  };
}

export function teardownDashboardStore() {
  cleanupInit?.();
  cleanupInit = null;
  initStarted = false;
}
