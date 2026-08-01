import { useDashboardStore } from "../../store/dashboard-store";

const formatNum = (n: number) =>
  n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M`
  : n >= 1_000   ? `${(n / 1_000).toFixed(1)}k`
  : String(n);

export function TopBar() {
  const stats              = useDashboardStore((s) => s.stats);
  const connected          = useDashboardStore((s) => s.connected);
  const runtime            = useDashboardStore((s) => s.runtime);
  const config             = useDashboardStore((s) => s.config);
  const sessionWrites      = useDashboardStore((s) => s.sessionWrites);
  const sessionFlushes     = useDashboardStore((s) => s.sessionFlushes);
  const sessionCompactions = useDashboardStore((s) => s.sessionCompactions);

  const compactionStyle = runtime?.CompactionStyle ?? config?.CompactionStyle ?? "leveled";

  const kpis = [
    { label: "Seq No",      value: stats?.seq_no        != null ? String(stats.seq_no) : "—"                     },
    { label: "Writes",      value: sessionWrites         != null ? formatNum(sessionWrites) : "—"                 },
    { label: "Flushes",     value: sessionFlushes        != null ? String(sessionFlushes)   : "—"                 },
    { label: "Compactions", value: sessionCompactions    != null ? String(sessionCompactions) : "—"               },
    { label: "Cache Hit",   value: stats?.cache_hit_rate != null ? `${(stats.cache_hit_rate * 100).toFixed(0)}%` : "—" },
    { label: "Strategy",    value: compactionStyle                                                                },
  ];

  return (
    <header style={{
      position: "sticky", top: 0, zIndex: 100,
      background: "var(--color-surface)",
      borderBottom: "1px solid var(--color-border)",
      display: "flex", alignItems: "center",
      padding: "0 16px", height: "52px", gap: "20px",
      backdropFilter: "blur(8px)",
    }}>
      {/* Brand */}
      <div style={{ display: "flex", flexDirection: "column", minWidth: 160, flexShrink: 0 }}>
        <span style={{
          fontFamily: "var(--font-display)", fontWeight: 700,
          fontSize: "16px", letterSpacing: "0.18em", textTransform: "uppercase",
          color: "var(--color-signal)", lineHeight: 1,
        }}>
          LSM ENGINE
        </span>
        <span style={{
          fontSize: "9px", letterSpacing: "0.15em", textTransform: "uppercase",
          color: "var(--color-text-muted)", marginTop: "2px",
        }}>
          Control Room<span className="animate-blink" style={{ color: "var(--color-signal)", marginLeft: 2 }}>█</span>
        </span>
      </div>

      {/* Divider */}
      <div style={{ width: 1, height: 28, background: "var(--color-border)" }} />

      {/* KPIs */}
      <div style={{ display: "flex", gap: "20px", flex: 1, overflow: "auto" }}>
        {kpis.map(kpi => (
          <div key={kpi.label} style={{ display: "flex", flexDirection: "column", minWidth: 56 }}>
            <span className="kv-label">{kpi.label}</span>
            <span style={{
              fontFamily: "var(--font-mono)", fontSize: "15px", fontWeight: 500,
              color: "var(--color-text)", lineHeight: 1.1, marginTop: "1px",
              whiteSpace: "nowrap",
            }}>{kpi.value}</span>
          </div>
        ))}
      </div>

      {/* Connection status */}
      <div style={{ display: "flex", alignItems: "center", gap: "6px", flexShrink: 0 }}>
        <span className={`conn-dot ${connected ? "live" : "connecting"}`} />
        <span style={{ fontSize: "10px", fontFamily: "var(--font-display)", fontWeight: 600,
          letterSpacing: "0.1em", textTransform: "uppercase",
          color: connected ? "var(--color-signal)" : "var(--color-amber)" }}>
          {connected ? "Live" : "Reconnecting"}
        </span>
      </div>
    </header>
  );
}
