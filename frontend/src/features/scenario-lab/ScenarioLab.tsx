import * as React from "react";
import { useDashboardStore } from "../../store/dashboard-store";

function feedTagFromTone(tone: string): string {
  switch (tone) {
    case "good":   return "feed-tag put";
    case "warn":   return "feed-tag compact";
    case "danger": return "feed-tag del";
    case "accent": return "feed-tag sync";
    default:       return "feed-tag sync";
  }
}

export function ScenarioLab() {
  const runtime         = useDashboardStore((s) => s.runtime);
  const config          = useDashboardStore((s) => s.config);
  const scenarios       = useDashboardStore((s) => s.scenarios);
  const benchmarkResult = useDashboardStore((s) => s.benchmarkResult);
  const closeMessage    = useDashboardStore((s) => s.closeMessage);
  const opsFeed         = useDashboardStore((s) => s.opsFeed);
  const onScenarioRun   = useDashboardStore((s) => s.handleScenarioRun);
  const onBenchRun      = useDashboardStore((s) => s.handleBenchRun);
  const onCloseAttempt  = useDashboardStore((s) => s.handleCloseAttempt);

  const [scenarioName, setScenarioName]     = React.useState("");
  const [benchType, setBenchType]           = React.useState("sequential_write");
  const [benchKeys, setBenchKeys]           = React.useState(2000);
  const [benchValueSize, setBenchValueSize] = React.useState(128);
  const [showCloseConfirm, setShowCloseConfirm] = React.useState(false);

  return (
    <div className="panel" style={{ display: "flex", flexDirection: "column" }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Operations Lab</div>
          <div className="panel-subtitle">Scenarios &amp; benchmarks</div>
        </div>
      </div>
      <div className="panel-body" style={{ display: "flex", flexDirection: "column", gap: 14 }}>

        {/* 3-column grid */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 10 }}>

          {/* Runtime info */}
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div className="kv-label" style={{ marginBottom: 2 }}>Runtime</div>
            <div className="stat-tile">
              <div className="stat-label">Data Dir</div>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--color-text)", marginTop: 2, wordBreak: "break-all" }}>
                {runtime?.DataDir ?? "unavailable"}
              </div>
            </div>
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
              <span className="sig-badge info">WAL #{runtime?.ActiveLogNumber ?? "?"}</span>
              <span className={`sig-badge ${runtime?.SyncWAL ? "signal" : "muted"}`}>
                sync {runtime?.SyncWAL ? "on" : "off"}
              </span>
            </div>
            <div className="stat-tile">
              <div className="stat-label">Memtable Target</div>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--color-text)", marginTop: 2 }}>
                {(config?.MemTableSize ?? 0).toLocaleString()} B
              </div>
            </div>
          </div>

          {/* Benchmark form */}
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div className="kv-label" style={{ marginBottom: 2 }}>Benchmark</div>
            <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
              <label className="kv-label" htmlFor="bench-type">Workload</label>
              <input
                id="bench-type"
                className="term-input"
                value={benchType}
                onChange={(e) => setBenchType(e.target.value)}
                placeholder="workload type"
              />
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 4 }}>
                <label className="kv-label" htmlFor="bench-keys">Keys</label>
                <input
                  id="bench-keys"
                  className="term-input"
                  type="number"
                  min={1}
                  value={benchKeys}
                  onChange={(e) => setBenchKeys(Number(e.target.value) || 0)}
                />
              </div>
              <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 4 }}>
                <label className="kv-label" htmlFor="bench-vsize">Value Bytes</label>
                <input
                  id="bench-vsize"
                  className="term-input"
                  type="number"
                  min={1}
                  value={benchValueSize}
                  onChange={(e) => setBenchValueSize(Number(e.target.value) || 0)}
                />
              </div>
            </div>
            <button
              className="term-btn primary"
              style={{ alignSelf: "flex-start" }}
              onClick={() => void onBenchRun({ type: benchType, num_keys: benchKeys, value_size: benchValueSize })}
            >
              ⚡ Run Bench
            </button>
            {benchmarkResult && (
              <div style={{ display: "flex", flexWrap: "wrap", gap: 5 }}>
                <span className="sig-badge signal">
                  {Math.round(benchmarkResult.ops_per_sec).toLocaleString()} ops/sec
                </span>
                <span style={{ fontSize: 10, color: "var(--color-text-muted)", fontFamily: "var(--font-mono)" }}>
                  p99w {benchmarkResult.p99_write_us}µs
                </span>
                <span style={{ fontSize: 10, color: "var(--color-text-muted)", fontFamily: "var(--font-mono)" }}>
                  p99r {benchmarkResult.p99_read_us}µs
                </span>
              </div>
            )}
          </div>

          {/* Scenario runner */}
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div className="kv-label" style={{ marginBottom: 2 }}>Scenario Runner</div>
            <select
              className="term-select"
              value={scenarioName}
              onChange={(e) => setScenarioName(e.target.value)}
            >
              <option value="">Select scenario…</option>
              {scenarios.map((s) => (
                <option key={s.name} value={s.name}>{s.name}</option>
              ))}
            </select>
            <button
              className="term-btn primary"
              disabled={!scenarioName}
              onClick={() => void onScenarioRun(scenarioName)}
              style={{ alignSelf: "flex-start" }}
            >
              ▶ Run Scenario
            </button>

            <hr className="term-divider" />

            {/* Lifecycle guardrail */}
            <div className="kv-label" style={{ marginBottom: 2 }}>Lifecycle Guardrail</div>
            {!showCloseConfirm ? (
              <button
                className="term-btn ghost"
                style={{ padding: "4px 10px", fontSize: 10, alignSelf: "flex-start" }}
                onClick={() => setShowCloseConfirm(true)}
              >
                ⏻ Attempt Close
              </button>
            ) : (
              <div style={{
                background: "rgba(255,60,94,0.08)", border: "1px solid rgba(255,60,94,0.25)",
                borderRadius: 2, padding: "8px 10px", display: "flex", flexDirection: "column", gap: 6,
              }}>
                <span style={{ fontSize: 10, color: "var(--color-crimson)", fontFamily: "var(--font-mono)" }}>
                  /close is reserved for local lifecycle — remote callers get 403.
                </span>
                <div style={{ display: "flex", gap: 6 }}>
                  <button
                    className="term-btn ghost"
                    style={{ padding: "3px 8px", fontSize: 10 }}
                    onClick={() => setShowCloseConfirm(false)}
                  >
                    Cancel
                  </button>
                  <button
                    className="term-btn danger-ghost"
                    style={{ padding: "3px 8px", fontSize: 10 }}
                    onClick={() => { setShowCloseConfirm(false); void onCloseAttempt(); }}
                  >
                    Send Anyway
                  </button>
                </div>
              </div>
            )}
            {closeMessage && (
              <p style={{ fontSize: 10, color: "var(--color-text-muted)", fontFamily: "var(--font-mono)", marginTop: 2 }}>
                {closeMessage}
              </p>
            )}
          </div>
        </div>

        <hr className="term-divider" />

        {/* Operations feed */}
        <div>
          <div className="kv-label" style={{ marginBottom: 5 }}>Operations Feed</div>
          {opsFeed.length === 0 ? (
            <p style={{ fontSize: 11, color: "var(--color-text-dim)" }}>Scenario and benchmark results will appear here.</p>
          ) : (
            <div style={{ maxHeight: 110, overflowY: "auto" }}>
              {opsFeed.map((entry) => (
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
