import { startTransition, useEffect, useEffectEvent, useState } from "react";

import {
  getBloomStat,
  getCompactionStats,
  getLevels,
  getMemtableSnapshot,
  getOpenState,
  getWalEntries,
  listScenarios,
} from "../../lib/api";
import type {
  BloomStat,
  CompactionLevelStat,
  EngineStats,
  LevelInfo,
  MemtableSnapshotResponse,
  OpenResponse,
  RuntimeState,
  ScenarioInfo,
  WalEntriesResponse,
} from "../../types";

export function useSnapshotState() {
  const [runtime, setRuntime] = useState<RuntimeState | null>(null);
  const [stats, setStats] = useState<EngineStats | null>(null);
  const [config, setConfig] = useState<OpenResponse["config"] | null>(null);
  const [levels, setLevels] = useState<LevelInfo[]>([]);
  const [walEntries, setWalEntries] = useState<WalEntriesResponse | null>(null);
  const [memtable, setMemtable] = useState<MemtableSnapshotResponse | null>(null);
  const [compactionStats, setCompactionStats] = useState<CompactionLevelStat[]>([]);
  const [scenarios, setScenarios] = useState<ScenarioInfo[]>([]);
  const [bloomStats, setBloomStats] = useState<BloomStat[]>([]);
  const [error, setError] = useState<string | null>(null);

  const refreshSnapshot = useEffectEvent(async () => {
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

      startTransition(() => {
        setRuntime(openState.state);
        setStats(openState.stats);
        setConfig(openState.config);
        setLevels(levelState);
        setWalEntries(walState);
        setMemtable(memState);
        setCompactionStats(compactionState);
        setScenarios(scenarioState);
      });

      const fileIds: number[] = [];
      for (const level of levelState) {
        for (const file of level.files) {
          fileIds.push(file.file_id);
          if (fileIds.length >= 10) {
            break
          }
        }
        if (fileIds.length >= 10) {
          break
        }
      }

      const bloomResults = await Promise.allSettled(fileIds.map((fileId) => getBloomStat(fileId)));
      const nextBloomStats = bloomResults
        .flatMap((result) => (result.status === "fulfilled" ? [result.value] : []))
        .sort((a, b) => a.file_id - b.file_id);

      setBloomStats(nextBloomStats);
      setError(null);
    } catch (refreshError) {
      setError(refreshError instanceof Error ? refreshError.message : String(refreshError));
    }
  });

  useEffect(() => {
    void refreshSnapshot();
    const interval = window.setInterval(() => {
      void refreshSnapshot();
    }, 5000);
    return () => window.clearInterval(interval);
  }, [refreshSnapshot]);

  return {
    runtime,
    stats,
    config,
    levels,
    walEntries,
    memtable,
    compactionStats,
    scenarios,
    bloomStats,
    error,
    setStats,
    setError,
    refreshSnapshot,
  };
}
