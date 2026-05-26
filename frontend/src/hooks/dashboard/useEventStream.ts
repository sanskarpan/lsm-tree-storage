import { useEffect, useEffectEvent, useRef, useState, type Dispatch, type SetStateAction } from "react";

import { line, limitLines, num, str, type TimedEvent } from "../../lib/dashboard/feed";
import type { ActiveCompaction, AmpPoint, EngineEvent, EngineStats, FeedLine } from "../../types";

const WS_URL = `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/ws`;

type UseEventStreamArgs = {
  refreshSnapshot: () => Promise<void>;
  setStats: Dispatch<SetStateAction<EngineStats | null>>;
};

export function useEventStream({ refreshSnapshot, setStats }: UseEventStreamArgs) {
  const [connected, setConnected] = useState(false);
  const [writeFeed, setWriteFeed] = useState<FeedLine[]>([]);
  const [compactionFeed, setCompactionFeed] = useState<FeedLine[]>([]);
  const [opsFeed, setOpsFeed] = useState<FeedLine[]>([]);
  const [sessionWrites, setSessionWrites] = useState(0);
  const [sessionFlushes, setSessionFlushes] = useState(0);
  const [sessionCompactions, setSessionCompactions] = useState(0);
  const [amplification, setAmplification] = useState({ wa: 1, ra: 0, sa: 1 });
  const [ampHistory, setAmpHistory] = useState<AmpPoint[]>([]);
  const [activeCompaction, setActiveCompaction] = useState<ActiveCompaction | null>(null);

  const eventsRef = useRef<TimedEvent[]>([]);

  const addOpsFeed = useEffectEvent((tone: FeedLine["tone"], label: string, detail?: string) => {
    setOpsFeed((current) => limitLines(current, line(tone, label, detail)));
  });

  const addCompactionFeed = useEffectEvent((tone: FeedLine["tone"], label: string, detail?: string) => {
    setCompactionFeed((current) => limitLines(current, line(tone, label, detail)));
  });

  const recordEvent = useEffectEvent((event: EngineEvent) => {
    const now = Date.now();
    eventsRef.current = [{ receivedAt: now, event }, ...eventsRef.current].slice(0, 200);

    const extra = event.extra ?? {};

    switch (event.type) {
      case "wal.append": {
        const seq = num(extra.seq_no) ?? num(extra.seq);
        const key = str(extra.key) ?? "(unknown)";
        const kind = num(extra.type) === 2 ? "DELETE" : "PUT";
        const detail = seq != null ? `seq ${seq}` : undefined;
        setWriteFeed((current) => limitLines(current, line(kind === "DELETE" ? "warn" : "good", `${kind} ${key}`, detail)));
        setSessionWrites((count) => count + 1);
        if (seq != null) {
          setStats((current) => (current ? { ...current, seq_no: seq } : current));
        }
        break;
      }
      case "wal.sync":
        setWriteFeed((current) => limitLines(current, line("accent", "WAL sync", "fsync completed")));
        break;
      case "flush.start":
        setSessionFlushes((count) => count + 1);
        addOpsFeed("warn", "Flush started", str(extra.file_id));
        break;
      case "flush.complete":
        addOpsFeed("good", "Flush complete", `file #${num(extra.file_id) ?? "?"}`);
        void refreshSnapshot();
        break;
      case "compaction.start":
        setActiveCompaction({
          inputLevel: num(extra.input_level) ?? 0,
          outputLevel: num(extra.output_level) ?? 1,
          startedAt: now,
        });
        addCompactionFeed(
          "accent",
          `Compaction L${num(extra.input_level) ?? 0} -> L${num(extra.output_level) ?? 1}`,
          `${num(extra.num_inputs) ?? 0} files selected`,
        );
        break;
      case "compaction.complete":
        setSessionCompactions((count) => count + 1);
        setActiveCompaction(null);
        addCompactionFeed("good", "Compaction complete");
        void refreshSnapshot();
        break;
      case "compaction.merge":
        addCompactionFeed("info", "Merge step", str(extra.key));
        break;
      case "tombstone.dropped":
        addCompactionFeed("warn", "Tombstone dropped", str(extra.key));
        break;
      case "amplification": {
        setAmplification((current) => {
          const next = {
            wa: num(extra.wa) ?? current.wa,
            ra: num(extra.ra) ?? current.ra,
            sa: num(extra.sa) ?? current.sa,
          };
          setAmpHistory((history) => [...history.slice(-23), { ...next, timestamp: now }]);
          return next;
        });
        break;
      }
      case "cache.hit":
        setStats((current) => (current ? { ...current, cache_hits: current.cache_hits + 1 } : current));
        break;
      case "cache.miss":
        setStats((current) => (current ? { ...current, cache_misses: current.cache_misses + 1 } : current));
        break;
      default:
        break;
    }
  });

  useEffect(() => {
    let retryTimer = 0;
    let socket: WebSocket | null = null;

    const connect = () => {
      socket = new WebSocket(WS_URL);
      socket.onopen = () => {
        setConnected(true);
      };
      socket.onclose = () => {
        setConnected(false);
        retryTimer = window.setTimeout(connect, 1500);
      };
      socket.onerror = () => socket?.close();
      socket.onmessage = (message) => {
        try {
          recordEvent(JSON.parse(message.data) as EngineEvent);
        } catch {
          // Ignore malformed frames from unexpected clients.
        }
      };
    };

    connect();

    return () => {
      window.clearTimeout(retryTimer);
      socket?.close();
    };
  }, [recordEvent]);

  return {
    connected,
    writeFeed,
    compactionFeed,
    opsFeed,
    sessionWrites,
    sessionFlushes,
    sessionCompactions,
    amplification,
    ampHistory,
    activeCompaction,
    eventsRef,
    addOpsFeed,
    addCompactionFeed,
  };
}
