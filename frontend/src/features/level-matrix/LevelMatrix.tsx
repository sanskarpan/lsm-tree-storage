import * as React from "react";
import { useDashboardStore } from "../../store/dashboard-store";

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

export function LevelMatrix() {
  const levels          = useDashboardStore((s) => s.levels);
  const memtable        = useDashboardStore((s) => s.memtable);
  const compactionStats = useDashboardStore((s) => s.compactionStats);
  const onRefresh       = useDashboardStore((s) => s.refreshSnapshot);
  const [refreshing, setRefreshing] = React.useState(false);

  const maxLevelBytes = Math.max(...levels.map((l) => l.total_size), 1);

  async function handleRefresh() {
    setRefreshing(true);
    try { await onRefresh(); }
    finally { setRefreshing(false); }
  }

  return (
    <div className="panel" style={{ display: "flex", flexDirection: "column" }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Level Matrix</div>
          <div className="panel-subtitle">Topology</div>
        </div>
        <button
          className="term-btn ghost"
          style={{ padding: "4px 10px", fontSize: 10 }}
          onClick={() => void handleRefresh()}
          disabled={refreshing}
        >
          {refreshing ? "..." : "↻ Refresh"}
        </button>
      </div>
      <div className="panel-body" style={{ display: "flex", flexDirection: "column", gap: 14 }}>

        {/* LSM pyramid bars */}
        {levels.length === 0 ? (
          <p style={{ fontSize: 11, color: "var(--color-text-dim)" }}>No level data yet.</p>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            {levels.map((level) => {
              const fillPct = level.total_size / maxLevelBytes;
              const barClass = fillPct > 0.8 ? "danger" : fillPct > 0.6 ? "warning" : "ok";
              return (
                <div key={level.level} style={{ marginBottom: 2 }}>
                  {/* Level label + stats row */}
                  <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
                    <span className="sig-badge info" style={{ minWidth: 28, justifyContent: "center" }}>
                      L{level.level}
                    </span>
                    <span style={{ fontSize: 10, color: "var(--color-text-muted)", fontFamily: "var(--font-mono)" }}>
                      {level.num_files} file{level.num_files !== 1 ? "s" : ""} · {formatBytes(level.total_size)}
                    </span>
                    <span style={{
                      marginLeft: "auto", fontSize: 10, fontFamily: "var(--font-mono)",
                      color: fillPct > 0.8 ? "var(--color-crimson)" : fillPct > 0.6 ? "var(--color-amber)" : "var(--color-signal)",
                    }}>
                      {(fillPct * 100).toFixed(0)}%
                    </span>
                  </div>
                  {/* Proportional bar */}
                  <div className="level-bar-track">
                    <div
                      className={`level-bar-fill ${barClass}`}
                      style={{ width: `${(fillPct * 100).toFixed(1)}%` }}
                    />
                  </div>
                  {/* File chips for L0 */}
                  {level.level === 0 && level.files.length > 0 && (
                    <div style={{ marginTop: 5, display: "flex", flexWrap: "wrap", gap: 3 }}>
                      {level.files.slice(0, 16).map((f) => (
                        <span key={f.file_id} style={{
                          fontSize: 9, background: "var(--color-surface-2)",
                          border: "1px solid var(--color-border-dim)", borderRadius: 1,
                          padding: "1px 4px", fontFamily: "var(--font-mono)",
                          color: "var(--color-text-muted)",
                        }}>
                          #{f.file_id}
                        </span>
                      ))}
                      {level.files.length > 16 && (
                        <span style={{ fontSize: 9, color: "var(--color-text-dim)" }}>
                          +{level.files.length - 16}
                        </span>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}

        <hr className="term-divider" />

        {/* Memtable section */}
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <div className="kv-label">Memtable Ownership</div>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span className="sig-badge signal">active</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--color-text)" }}>
              log #{memtable?.active_log_number ?? "?"}
            </span>
            <span style={{ fontSize: 10, color: "var(--color-text-muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 180 }}>
              {memtable?.active_wal_path ?? "unavailable"}
            </span>
          </div>
          {(memtable?.immutables ?? []).map((imm) => (
            <div key={imm.log_number} style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span className="sig-badge warn">immutable</span>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--color-amber)" }}>
                log #{imm.log_number} · {imm.table.entries.length} rows
              </span>
              <span style={{ fontSize: 10, color: "var(--color-text-muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 160 }}>
                {imm.wal_path}
              </span>
            </div>
          ))}
          {(memtable?.immutables.length ?? 0) === 0 && (
            <span style={{ fontSize: 10, color: "var(--color-text-dim)" }}>No immutables queued.</span>
          )}
        </div>

        {/* Compaction stats table */}
        {compactionStats.length > 0 && (
          <>
            <hr className="term-divider" />
            <div>
              <div className="kv-label" style={{ marginBottom: 6 }}>Compaction Balance</div>
              <table className="term-table">
                <thead>
                  <tr><th>Level</th><th>Files</th><th>Size</th><th>Fill</th></tr>
                </thead>
                <tbody>
                  {compactionStats.map((item) => {
                    const maxStat = Math.max(...compactionStats.map((x) => x.total_size), 1);
                    const pct = (item.total_size / maxStat) * 100;
                    const barColor = pct > 85 ? "var(--color-crimson)" : pct > 65 ? "var(--color-amber)" : "var(--color-signal)";
                    return (
                      <tr key={item.level}>
                        <td><span className="sig-badge info">L{item.level}</span></td>
                        <td style={{ color: "var(--color-text-muted)" }}>{item.num_files}</td>
                        <td>{formatBytes(item.total_size)}</td>
                        <td style={{ width: 80 }}>
                          <div className="term-progress-track" style={{ width: 60 }}>
                            <div className="term-progress-fill" style={{ width: `${pct.toFixed(1)}%`, background: barColor }} />
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </>
        )}

      </div>
    </div>
  );
}
