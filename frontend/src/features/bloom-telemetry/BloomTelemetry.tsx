import * as React from "react";
import { useDashboardStore } from "../../store/dashboard-store";
import type { BloomStat } from "../../types";

export function BloomTelemetry() {
  const bloomStats = useDashboardStore((s) => s.bloomStats);

  const stats: BloomStat[] = React.useMemo(
    () => (Array.isArray(bloomStats) ? bloomStats : []),
    [bloomStats],
  );

  const maxBits = Math.max(0, ...stats.map((s) => s.bits_per_key ?? 0));
  const meanFpr = stats.length
    ? stats.reduce((sum, s) => sum + (s.estimated_fp_rate ?? 0), 0) / stats.length
    : 0;

  const fprColor = meanFpr > 0.05 ? "var(--color-crimson)" : meanFpr > 0.02 ? "var(--color-amber)" : "var(--color-signal)";
  const fprBarColor = meanFpr > 0.05 ? "var(--color-crimson)" : meanFpr > 0.02 ? "var(--color-amber)" : "var(--color-signal)";

  return (
    <div className="panel" style={{ display: "flex", flexDirection: "column" }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Bloom Telemetry</div>
          <div className="panel-subtitle">False-positive indicators</div>
        </div>
      </div>
      <div className="panel-body" style={{ display: "flex", flexDirection: "column", gap: 12 }}>

        {/* Stat tiles — 2 col grid */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          <div className="stat-tile">
            <div className="stat-label">Filters</div>
            <div className="stat-value">{stats.length}</div>
          </div>
          <div className="stat-tile">
            <div className="stat-label">Max bpk</div>
            <div className="stat-value">{maxBits}</div>
          </div>
        </div>

        {/* Mean FPR */}
        <div>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 4 }}>
            <span className="kv-label">Mean FPR Estimate</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: fprColor }}>
              {(meanFpr * 100).toFixed(2)}%
            </span>
          </div>
          <div className="term-progress-track">
            <div
              className="term-progress-fill"
              style={{ width: `${Math.min(100, meanFpr * 2000).toFixed(1)}%`, background: fprBarColor }}
            />
          </div>
        </div>

        {/* Bits/key */}
        <div>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 4 }}>
            <span className="kv-label">Max Bits / Key</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--color-text-muted)" }}>
              {maxBits} bpk
            </span>
          </div>
          <div className="term-progress-track">
            <div
              className="term-progress-fill"
              style={{ width: `${Math.min(100, (maxBits / 20) * 100).toFixed(1)}%`, background: "var(--color-signal)" }}
            />
          </div>
        </div>

        <hr className="term-divider" />

        {/* Filter list */}
        {stats.length === 0 ? (
          <p style={{
            fontSize: 11, color: "var(--color-text-dim)",
            border: "1px dashed var(--color-border-dim)",
            borderRadius: 2, padding: "12px 10px", textAlign: "center",
          }}>
            No bloom filters built yet.
          </p>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <div className="kv-label" style={{ marginBottom: 2 }}>Filters ({stats.length})</div>
            {stats.slice(0, 10).map((stat, index) => {
              const fpr = stat.estimated_fp_rate ?? 0;
              const fprPct = Math.min(100, fpr * 2000);
              const color = fpr > 0.05 ? "var(--color-crimson)" : fpr > 0.02 ? "var(--color-amber)" : "var(--color-signal)";
              return (
                <div key={stat.file_id ?? index} style={{ display: "flex", alignItems: "center", gap: 6 }}>
                  <span className="sig-badge info" style={{ minWidth: 32, justifyContent: "center" }}>
                    #{stat.file_id ?? index}
                  </span>
                  <span style={{ fontSize: 9, color: "var(--color-text-muted)", fontFamily: "var(--font-display)", minWidth: 44 }}>
                    {stat.bits_per_key ?? "?"} bpk
                  </span>
                  <div className="term-progress-track" style={{ flex: 1 }}>
                    <div className="term-progress-fill" style={{ width: `${fprPct.toFixed(1)}%`, background: color }} />
                  </div>
                  <span style={{ fontSize: 9, fontFamily: "var(--font-mono)", color, minWidth: 36, textAlign: "right" }}>
                    {(fpr * 100).toFixed(2)}%
                  </span>
                </div>
              );
            })}
            {stats.length > 10 && (
              <span style={{ fontSize: 9, color: "var(--color-text-dim)" }}>
                +{stats.length - 10} more filters
              </span>
            )}
          </div>
        )}

      </div>
    </div>
  );
}
