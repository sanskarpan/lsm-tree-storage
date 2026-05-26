import { useEffectEvent, useState } from "react";

import {
  attemptRemoteClose,
  deleteValue,
  forceCompaction,
  putValue,
  runBench,
  runScenario,
  setCompactionStyle,
} from "../../lib/api";
import type { BenchRequest, BenchResult, CompactionStyle, FeedLine } from "../../types";

type UseDashboardActionsArgs = {
  refreshSnapshot: () => Promise<void>;
  addOpsFeed: (tone: FeedLine["tone"], label: string, detail?: string) => void;
  addCompactionFeed: (tone: FeedLine["tone"], label: string, detail?: string) => void;
};

export function useDashboardActions({
  refreshSnapshot,
  addOpsFeed,
  addCompactionFeed,
}: UseDashboardActionsArgs) {
  const [benchmarkResult, setBenchmarkResult] = useState<BenchResult | null>(null);
  const [closeMessage, setCloseMessage] = useState<string | null>(null);

  const handlePut = useEffectEvent(async (key: string, value: string) => {
    await putValue(key, value);
    addOpsFeed("good", `Accepted PUT ${key}`, value);
    await refreshSnapshot();
  });

  const handleDelete = useEffectEvent(async (key: string) => {
    await deleteValue(key);
    addOpsFeed("warn", `Accepted DELETE ${key}`);
    await refreshSnapshot();
  });

  const handleStyleChange = useEffectEvent(async (style: CompactionStyle) => {
    await setCompactionStyle(style);
    addCompactionFeed("accent", `Compaction style -> ${style}`);
    await refreshSnapshot();
  });

  const handleForceCompaction = useEffectEvent(async () => {
    await forceCompaction(0);
    addCompactionFeed("accent", "Manual L0 compaction requested");
  });

  const handleScenarioRun = useEffectEvent(async (name: string) => {
    const result = await runScenario(name);
    addOpsFeed("accent", `Scenario ${result.scenario}`, result.status);
    await refreshSnapshot();
  });

  const handleBenchRun = useEffectEvent(async (requestBody: BenchRequest) => {
    const result = await runBench(requestBody);
    setBenchmarkResult(result);
    addOpsFeed("info", "Benchmark complete", `${Math.round(result.ops_per_sec)} ops/sec`);
  });

  const handleCloseAttempt = useEffectEvent(async () => {
    const result = await attemptRemoteClose();
    setCloseMessage(result.reason);
  });

  return {
    benchmarkResult,
    closeMessage,
    handlePut,
    handleDelete,
    handleStyleChange,
    handleForceCompaction,
    handleScenarioRun,
    handleBenchRun,
    handleCloseAttempt,
  };
}
