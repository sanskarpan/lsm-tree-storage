import type { EngineConfig, EngineStats, RuntimeState } from "../types";

type HeaderBarProps = {
  connected: boolean;
  runtime: RuntimeState | null;
  stats: EngineStats | null;
  config: EngineConfig | null;
  sessionWrites: number;
  sessionFlushes: number;
  sessionCompactions: number;
};

export function HeaderBar({
  connected,
  runtime,
  stats,
  config,
  sessionWrites,
  sessionFlushes,
  sessionCompactions,
}: HeaderBarProps) {
  return (
    <header className="hero">
      <div className="hero__copy">
        <p className="hero__kicker">LSM control room</p>
        <h1>Storage telemetry without the 2,000-line inline script.</h1>
        <p className="hero__lede">
          Live write-path activity, range topology, bloom health, compaction control, and recovery-facing
          observability in a typed client that is easier to extend safely.
        </p>
      </div>

      <div className="hero__status">
        <div className={`hero__pill ${connected ? "is-live" : ""}`}>
          <span className="hero__dot" />
          {connected ? "WebSocket live" : "Reconnecting"}
        </div>

        <dl className="hero__stats">
          <div>
            <dt>SeqNo</dt>
            <dd>{stats?.seq_no ?? 0}</dd>
          </div>
          <div>
            <dt>Session Writes</dt>
            <dd>{sessionWrites}</dd>
          </div>
          <div>
            <dt>Flushes</dt>
            <dd>{sessionFlushes}</dd>
          </div>
          <div>
            <dt>Compactions</dt>
            <dd>{sessionCompactions}</dd>
          </div>
          <div>
            <dt>Cache Hit Rate</dt>
            <dd>{((stats?.cache_hit_rate ?? 0) * 100).toFixed(0)}%</dd>
          </div>
          <div>
            <dt>Compaction Style</dt>
            <dd>{runtime?.CompactionStyle ?? config?.CompactionStyle ?? "leveled"}</dd>
          </div>
        </dl>
      </div>
    </header>
  );
}
