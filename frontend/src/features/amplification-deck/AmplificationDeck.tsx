import { useDashboardStore } from "../../store/dashboard-store";
import type { AmpPoint } from "../../types";

const MAX_AMP = 16;

function ringColorClass(value: number, threshold: number): string {
  if (value > threshold) return "danger";
  if (value > threshold * 0.75) return "warning";
  return "ok";
}

function RingGauge({
  value,
  max,
  label,
  colorClass,
}: {
  value: number;
  max: number;
  label: string;
  colorClass: string;
}) {
  const R = 42;
  const circ = 2 * Math.PI * R;
  const pct = Math.min(value / max, 1);
  const offset = circ * (1 - pct);

  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 4 }}>
      <svg width="100" height="100" viewBox="0 0 100 100">
        <circle cx="50" cy="50" r={R} className="ring-track" />
        <circle
          cx="50"
          cy="50"
          r={R}
          className={`ring-fill ${colorClass}`}
          style={{ strokeDasharray: circ, strokeDashoffset: offset }}
        />
        <text
          x="50"
          y="46"
          textAnchor="middle"
          style={{ fill: "var(--color-text)", fontSize: "16px", fontFamily: "var(--font-mono)", fontWeight: 500 }}
        >
          {value.toFixed(1)}x
        </text>
        <text
          x="50"
          y="60"
          textAnchor="middle"
          style={{
            fill: "var(--color-text-muted)", fontSize: "8px",
            fontFamily: "var(--font-display)", fontWeight: 600,
            letterSpacing: "0.1em", textTransform: "uppercase",
          }}
        >
          {label}
        </text>
      </svg>
    </div>
  );
}

function Sparkline({
  data,
  color,
  label,
}: {
  data: number[];
  color: string;
  label: string;
}) {
  if (!data || data.length < 2) return null;
  const W = 280; const H = 52; const PAD = 4;
  const min = Math.min(...data);
  const max = Math.max(...data);
  const range = max - min || 1;
  const pts: [number, number][] = data.map((v, i) => [
    PAD + (i / (data.length - 1)) * (W - PAD * 2),
    PAD + (1 - (v - min) / range) * (H - PAD * 2),
  ]);
  const line = pts.map((p, i) => `${i === 0 ? "M" : "L"} ${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(" ");
  const last = pts[pts.length - 1] ?? [W - PAD, H];
  const first = pts[0] ?? [PAD, H];
  const area = `${line} L ${last[0].toFixed(1)},${H} L ${first[0].toFixed(1)},${H} Z`;
  const gradId = `grad-amp-${label}`;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <span style={{
        fontSize: 9, letterSpacing: "0.12em", textTransform: "uppercase",
        color: "var(--color-text-muted)", fontFamily: "var(--font-display)",
      }}>
        {label}
      </span>
      <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} style={{ display: "block" }}>
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.4" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        <path d={area} fill={`url(#${gradId})`} />
        <path d={line} fill="none" stroke={color} strokeWidth="1.5"
          strokeLinecap="round" strokeLinejoin="round" />
        {pts.map((p, i) => (
          <circle key={i} cx={p[0]} cy={p[1]} r="2" fill={color}
            opacity={i === pts.length - 1 ? 1 : 0.3} />
        ))}
      </svg>
    </div>
  );
}

export function AmplificationDeck() {
  const wa      = useDashboardStore((s) => s.amplification.wa);
  const ra      = useDashboardStore((s) => s.amplification.ra);
  const sa      = useDashboardStore((s) => s.amplification.sa);
  const history = useDashboardStore((s) => s.ampHistory);

  const points: AmpPoint[] = history.length > 0
    ? history
    : [{ wa, ra, sa, timestamp: Date.now() }];

  const waData = points.map((p) => p.wa);
  const raData = points.map((p) => p.ra);
  const saData = points.map((p) => p.sa);

  const gauges = [
    { key: "wa", label: "Write Amp", value: wa, threshold: 8, color: "#9DFF3C" },
    { key: "ra", label: "Read Amp",  value: ra, threshold: 8, color: "#3CF4FF" },
    { key: "sa", label: "Space Amp", value: sa, threshold: 4, color: "#FFB800" },
  ];

  return (
    <div className="panel" style={{ display: "flex", flexDirection: "column" }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Stress Profile</div>
          <div className="panel-subtitle">Amplification</div>
        </div>
      </div>
      <div className="panel-body" style={{ display: "flex", flexDirection: "column", gap: 14 }}>

        {/* Ring gauges */}
        <div style={{ display: "flex", justifyContent: "space-around", alignItems: "center" }}>
          {gauges.map(({ key, label, value, threshold, color }) => {
            const colorClass = ringColorClass(value, threshold);
            const badgeClass = colorClass === "danger" ? "danger" : colorClass === "warning" ? "warn" : "signal";
            const status = value > threshold ? "elevated" : value > threshold * 0.75 ? "watch" : "stable";
            return (
              <div key={key} style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 4 }}>
                <RingGauge value={value} max={MAX_AMP} label={label} colorClass={colorClass} />
                <span className={`sig-badge ${badgeClass}`}>{status}</span>
              </div>
            );
          })}
        </div>

        <hr className="term-divider" />

        {/* Sparklines */}
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div className="kv-label" style={{ marginBottom: 2 }}>Recent Amplification Signal</div>
          <Sparkline data={waData} color="#9DFF3C" label="Write Amplification" />
          <Sparkline data={raData} color="#3CF4FF" label="Read Amplification" />
          <Sparkline data={saData} color="#FFB800" label="Space Amplification" />
        </div>

        {/* Legend */}
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 12 }}>
          {[
            { color: "#9DFF3C", label: "WA" },
            { color: "#3CF4FF", label: "RA" },
            { color: "#FFB800", label: "SA" },
          ].map(({ color, label }) => (
            <span key={label} style={{ display: "flex", alignItems: "center", gap: 4, fontSize: 9, color: "var(--color-text-muted)", fontFamily: "var(--font-display)", letterSpacing: "0.1em" }}>
              <span style={{ display: "inline-block", width: 12, height: 2, background: color, borderRadius: 1 }} />
              {label}
            </span>
          ))}
        </div>

      </div>
    </div>
  );
}
