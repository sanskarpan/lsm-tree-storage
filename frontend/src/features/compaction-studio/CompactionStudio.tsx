import { useDashboardStore } from "../../store/dashboard-store";
import type { CompactionStyle } from "../../types";

const STYLES: CompactionStyle[] = ["leveled", "size-tiered", "time-window"];

function feedTagFromTone(tone: string): string {
  switch (tone) {
    case "good":   return "feed-tag sync";
    case "warn":   return "feed-tag compact";
    case "danger": return "feed-tag del";
    case "accent": return "feed-tag put";
    default:       return "feed-tag sync";
  }
}

export function CompactionStudio() {
  const activeCompaction = useDashboardStore((s) => s.activeCompaction);
  const compactionFeed   = useDashboardStore((s) => s.compactionFeed);
  const compactionStats  = useDashboardStore((s) => s.compactionStats);
  const runtime          = useDashboardStore((s) => s.runtime);
  const config           = useDashboardStore((s) => s.config);
  const onForceCompact   = useDashboardStore((s) => s.handleForceCompaction);
  const onStyleChange    = useDashboardStore((s) => s.handleStyleChange);

  const currentStyle = runtime?.CompactionStyle ?? config?.CompactionStyle ?? "leveled";
  const maxBytes     = Math.max(...compactionStats.map((item) => item.total_size), 1);

  return (
    <div className="panel" style={{ display: "flex", flexDirection: "column" }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Compaction Studio</div>
          <div className="panel-subtitle">Style &amp; pressure</div>
        </div>
        {activeCompaction ? (
          <span className="sig-badge signal">worker active</span>
        ) : (
          <span className="sig-badge muted">standing by</span>
        )}
      </div>
      <div className="panel-body" style={{ display: "flex", flexDirection: "column", gap: 12 }}>

        {/* Strategy selector */}
        <div>
          <div className="kv-label" style={{ marginBottom: 6 }}>Strategy</div>
          <div style={{ display: "flex", gap: 5, flexWrap: "wrap" }}>
            {STYLES.map((style) => (
              <button
                key={style}
                className={`term-btn ${style === currentStyle ? "primary" : "ghost"}`}
                style={{ padding: "4px 10px", fontSize: 10 }}
                onClick={() => void onStyleChange(style)}
              >
                {style}
              </button>
            ))}
            <button
              className="term-btn danger-ghost"
              style={{ padding: "4px 10px", fontSize: 10 }}
              onClick={() => void onForceCompact()}
            >
              ▶ Force L0
            </button>
          </div>
        </div>

        {/* Current state */}
        <div className="stat-tile" style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div className="stat-label">Current State</div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--color-text)", marginTop: 3 }}>
              {activeCompaction
                ? `Running L${activeCompaction.inputLevel} → L${activeCompaction.outputLevel}`
                : "Idle"}
            </div>
          </div>
          <div style={{ textAlign: "right" }}>
            <div className="stat-label">Strategy</div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--color-signal)", marginTop: 3 }}>
              {currentStyle}
            </div>
          </div>
        </div>

        {/* Level sizes */}
        <div>
          <div className="kv-label" style={{ marginBottom: 6 }}>Level Sizes</div>
          {compactionStats.length === 0 ? (
            <p style={{ fontSize: 11, color: "var(--color-text-dim)" }}>No level data yet.</p>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 7 }}>
              {compactionStats.map((item) => {
                const pct = item.total_size / maxBytes;
                const barClass = pct > 0.85 ? "danger" : pct > 0.65 ? "warning" : "ok";
                return (
                  <div key={item.level} style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <span className="sig-badge info" style={{ minWidth: 28, justifyContent: "center" }}>
                      L{item.level}
                    </span>
                    <div className="level-bar-track" style={{ flex: 1 }}>
                      <div className={`level-bar-fill ${barClass}`} style={{ width: `${(pct * 100).toFixed(1)}%` }} />
                    </div>
                    <span style={{ fontSize: 9, color: "var(--color-text-muted)", fontFamily: "var(--font-mono)", minWidth: 40, textAlign: "right" }}>
                      {item.num_files} files
                    </span>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        <hr className="term-divider" />

        {/* Worker feed */}
        <div>
          <div className="kv-label" style={{ marginBottom: 5 }}>Worker Feed</div>
          {compactionFeed.length === 0 ? (
            <p style={{ fontSize: 11, color: "var(--color-text-dim)" }}>Compaction events will appear here.</p>
          ) : (
            <div style={{ maxHeight: 130, overflowY: "auto" }}>
              {compactionFeed.map((entry) => (
                <div key={entry.id} className="feed-entry animate-slide-in">
                  <span className={feedTagFromTone(entry.tone)}>{entry.label}</span>
                  {entry.detail && <span className="feed-detail">{entry.detail}</span>}
                </div>
              ))}
            </div>
          )}
        </div>

      </div>
    </div>
  );
}
