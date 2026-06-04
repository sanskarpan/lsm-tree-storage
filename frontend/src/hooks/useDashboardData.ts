import { useEffect } from "react";

import { initDashboardStore, teardownDashboardStore } from "../store/dashboard-store";
import { useDashboardStore } from "../store/dashboard-store";

export function useDashboardData() {
  useEffect(() => {
    initDashboardStore();
    return () => {
      teardownDashboardStore();
    };
  }, []);

  return useDashboardStore((state) => ({
    connected: state.connected,
    runtime: state.runtime,
    stats: state.stats,
    config: state.config,
    levels: state.levels,
    walEntries: state.walEntries,
    memtable: state.memtable,
    compactionStats: state.compactionStats,
    scenarios: state.scenarios,
    bloomStats: state.bloomStats,
    writeFeed: state.writeFeed,
    compactionFeed: state.compactionFeed,
    opsFeed: state.opsFeed,
    readTrace: state.readTrace,
    queryPending: state.queryPending,
    benchmarkResult: state.benchmarkResult,
    closeMessage: state.closeMessage,
    error: state.error,
    sessionWrites: state.sessionWrites,
    sessionFlushes: state.sessionFlushes,
    sessionCompactions: state.sessionCompactions,
    amplification: state.amplification,
    ampHistory: state.ampHistory,
    activeCompaction: state.activeCompaction,
    refreshSnapshot: state.refreshSnapshot,
    runReadTrace: state.runReadTrace,
    handlePut: state.handlePut,
    handleDelete: state.handleDelete,
    handleStyleChange: state.handleStyleChange,
    handleForceCompaction: state.handleForceCompaction,
    handleScenarioRun: state.handleScenarioRun,
    handleBenchRun: state.handleBenchRun,
    handleCloseAttempt: state.handleCloseAttempt,
  }));
}
