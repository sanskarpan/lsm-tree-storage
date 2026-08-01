import * as React from "react";
import { useDashboardStore } from "../../store/dashboard-store";

function stepColor(step: string): string {
  const s = step.toLowerCase();
  if (s.includes("memtable") && s.includes("hit")) return "var(--color-signal)";
  if (s.includes("bloom")) return "var(--color-cyan)";
  if (s.includes("sstable") || s.includes("sst")) return "var(--color-amber)";
  if (s.includes("miss") || s.includes("not found")) return "var(--color-text-dim)";
  return "var(--color-text-muted)";
}

export function ReadInspector() {
  const trace     = useDashboardStore((s) => s.readTrace);
  const pending   = useDashboardStore((s) => s.queryPending);
  const onInspect = useDashboardStore((s) => s.runReadTrace);

  const [key, setKey] = React.useState("live-a");

  return (
    <div className="panel" style={{ display: "flex", flexDirection: "column" }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Query Inspector</div>
          <div className="panel-subtitle">Read path</div>
        </div>
      </div>
      <div className="panel-body" style={{ display: "flex", flexDirection: "column", gap: 12 }}>

        {/* Key trace form */}
        <form
          onSubmit={(e) => { e.preventDefault(); if (key) void onInspect(key); }}
          style={{ display: "flex", gap: 6, alignItems: "flex-end" }}
        >
          <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 3 }}>
            <label className="kv-label" htmlFor="ri-key">Trace a key</label>
            <input
              id="ri-key"
              className="term-input"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="key"
            />
          </div>
          <button type="submit" className="term-btn primary" disabled={pending || !key}>
            {pending ? "Tracing..." : "Trace Read"}
          </button>
        </form>

        {/* Result box */}
        <div className="stat-tile" style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div className="stat-label">Result</div>
            {trace ? (
              <span className={`sig-badge ${trace.found ? "signal" : "danger"}`} style={{ marginTop: 4, display: "inline-flex" }}>
                {trace.found ? "Found" : "Missing"}
              </span>
            ) : (
              <span className="sig-badge muted" style={{ marginTop: 4, display: "inline-flex" }}>
                Awaiting query
              </span>
            )}
          </div>
          {trace && (
            <div style={{ textAlign: "right" }}>
              <div className="stat-label">Status</div>
              <div className="stat-value" style={{ fontSize: 22 }}>{trace.status}</div>
            </div>
          )}
        </div>

        {/* Metrics grid */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6 }}>
          {[
            { label: "Bloom Checks", value: trace?.bloomChecks ?? 0, color: "var(--color-cyan)" },
            { label: "Bloom Misses", value: trace?.bloomMisses ?? 0, color: "var(--color-amber)" },
            { label: "Memtable Hits", value: trace?.memtableHits ?? 0, color: "var(--color-signal)" },
            { label: "SSTable Hits", value: trace?.sstableHits ?? 0, color: "var(--color-text-muted)" },
          ].map(({ label, value, color }) => (
            <div key={label} className="stat-tile">
              <div className="stat-label">{label}</div>
              <div className="stat-value sm" style={{ color }}>{value}</div>
            </div>
          ))}
        </div>

        {/* Captured value */}
        {trace?.value && (
          <div style={{
            background: "var(--color-surface-2)", border: "1px solid var(--color-border-dim)",
            borderRadius: 2, padding: "8px 10px",
          }}>
            <div className="kv-label" style={{ marginBottom: 3 }}>Captured Value</div>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--color-text)", wordBreak: "break-all" }}>
              {trace.value}
            </span>
          </div>
        )}

        {/* Execution steps */}
        <div>
          <div className="kv-label" style={{ marginBottom: 6 }}>Execution Steps</div>
          {(trace?.steps.length ?? 0) > 0 ? (
            <ol style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: 4 }}>
              {trace!.steps.map((step, index) => (
                <li key={`${step}-${index}`} style={{
                  display: "flex", gap: 8, alignItems: "flex-start",
                  background: "var(--color-surface-2)", border: "1px solid var(--color-border-dim)",
                  borderRadius: 2, padding: "5px 8px",
                }}>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--color-text-dim)", minWidth: 18, paddingTop: 1 }}>
                    {String(index + 1).padStart(2, "0")}
                  </span>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: stepColor(step) }}>
                    {step}
                  </span>
                </li>
              ))}
            </ol>
          ) : (
            <p style={{ fontSize: 11, color: "var(--color-text-dim)" }}>No read trace captured yet.</p>
          )}
        </div>

      </div>
    </div>
  );
}
