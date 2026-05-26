import { useEffectEvent, useState, type Dispatch, type RefObject, type SetStateAction } from "react";

import { getValue } from "../../lib/api";
import { num, sleep } from "../../lib/dashboard/feed";
import type { EngineEvent, LevelInfo, MemtableSnapshotResponse, ReadTraceReport } from "../../types";

type UseReadTraceArgs = {
  eventsRef: RefObject<Array<{ receivedAt: number; event: EngineEvent }>>;
  memtable: MemtableSnapshotResponse | null;
  levels: LevelInfo[];
  setError: Dispatch<SetStateAction<string | null>>;
};

export function useReadTrace({ eventsRef, memtable, levels, setError }: UseReadTraceArgs) {
  const [readTrace, setReadTrace] = useState<ReadTraceReport | null>(null);
  const [queryPending, setQueryPending] = useState(false);

  const runReadTrace = useEffectEvent(async (key: string) => {
    setQueryPending(true);

    try {
      const startedAt = Date.now();
      const response = await getValue(key);
      await sleep(160);

      const relevant = (eventsRef.current ?? [])
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
            steps.push("No live event trace was captured; key is outside current memtable and visible SSTable ranges");
          }
        }
      }

      setReadTrace({
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
      });
      setError(null);
    } catch (traceError) {
      setError(traceError instanceof Error ? traceError.message : String(traceError));
    } finally {
      setQueryPending(false);
    }
  });

  return {
    readTrace,
    queryPending,
    runReadTrace,
  };
}
