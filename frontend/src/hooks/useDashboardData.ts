import { useDashboardActions } from "./dashboard/useDashboardActions";
import { useEventStream } from "./dashboard/useEventStream";
import { useReadTrace } from "./dashboard/useReadTrace";
import { useSnapshotState } from "./dashboard/useSnapshotState";

export function useDashboardData() {
  const snapshots = useSnapshotState();
  const events = useEventStream({
    refreshSnapshot: snapshots.refreshSnapshot,
    setStats: snapshots.setStats,
  });
  const trace = useReadTrace({
    eventsRef: events.eventsRef,
    memtable: snapshots.memtable,
    levels: snapshots.levels,
    setError: snapshots.setError,
  });
  const actions = useDashboardActions({
    refreshSnapshot: snapshots.refreshSnapshot,
    addOpsFeed: events.addOpsFeed,
    addCompactionFeed: events.addCompactionFeed,
  });

  return {
    connected: events.connected,
    runtime: snapshots.runtime,
    stats: snapshots.stats,
    config: snapshots.config,
    levels: snapshots.levels,
    walEntries: snapshots.walEntries,
    memtable: snapshots.memtable,
    compactionStats: snapshots.compactionStats,
    scenarios: snapshots.scenarios,
    bloomStats: snapshots.bloomStats,
    writeFeed: events.writeFeed,
    compactionFeed: events.compactionFeed,
    opsFeed: events.opsFeed,
    readTrace: trace.readTrace,
    queryPending: trace.queryPending,
    benchmarkResult: actions.benchmarkResult,
    closeMessage: actions.closeMessage,
    error: snapshots.error,
    sessionWrites: events.sessionWrites,
    sessionFlushes: events.sessionFlushes,
    sessionCompactions: events.sessionCompactions,
    amplification: events.amplification,
    ampHistory: events.ampHistory,
    activeCompaction: events.activeCompaction,
    refreshSnapshot: snapshots.refreshSnapshot,
    runReadTrace: trace.runReadTrace,
    handlePut: actions.handlePut,
    handleDelete: actions.handleDelete,
    handleStyleChange: actions.handleStyleChange,
    handleForceCompaction: actions.handleForceCompaction,
    handleScenarioRun: actions.handleScenarioRun,
    handleBenchRun: actions.handleBenchRun,
    handleCloseAttempt: actions.handleCloseAttempt,
  };
}
