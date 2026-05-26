import type { AmpPoint } from "../types";
import { Panel } from "./Panel";

type AmplificationDeckProps = {
  wa: number;
  ra: number;
  sa: number;
  history: AmpPoint[];
};

function Gauge({ label, value, accent }: { label: string; value: number; accent: string }) {
  const radius = 42;
  const circumference = 2 * Math.PI * radius;
  const progress = Math.min(value / 16, 1);
  const dash = circumference * progress;

  return (
    <figure className="gauge">
      <svg viewBox="0 0 120 120">
        <circle className="gauge__track" cx="60" cy="60" r={radius} />
        <circle
          className="gauge__progress"
          cx="60"
          cy="60"
          r={radius}
          style={{
            stroke: accent,
            strokeDasharray: `${dash} ${circumference - dash}`,
          }}
        />
      </svg>
      <figcaption>
        <strong>{value.toFixed(1)}x</strong>
        <span>{label}</span>
      </figcaption>
    </figure>
  );
}

export function AmplificationDeck({ wa, ra, sa, history }: AmplificationDeckProps) {
  const points = history.length > 0 ? history : [{ wa, ra, sa, timestamp: Date.now() }];
  const maxValue = Math.max(...points.flatMap((point) => [point.wa, point.ra, point.sa]), 1);
  const polyline = (selector: "wa" | "ra" | "sa") =>
    points
      .map((point, index) => {
        const x = (index / Math.max(points.length - 1, 1)) * 100;
        const y = 48 - (point[selector] / maxValue) * 40;
        return `${x},${y}`;
      })
      .join(" ");

  return (
    <Panel eyebrow="Amplification" title="Stress profile">
      <div className="stack">
        <div className="gauge-grid">
          <Gauge accent="#67f5c6" label="Write amp" value={wa} />
          <Gauge accent="#ffb255" label="Read amp" value={ra} />
          <Gauge accent="#ff7f96" label="Space amp" value={sa} />
        </div>

        <div className="chart-card">
          <div className="chart-card__header">
            <h3 className="section-title">Recent amplification signal</h3>
            <span>driven by live events</span>
          </div>
          <svg className="sparkline" viewBox="0 0 100 52" preserveAspectRatio="none">
            <polyline points={polyline("wa")} />
            <polyline className="is-amber" points={polyline("ra")} />
            <polyline className="is-rose" points={polyline("sa")} />
          </svg>
        </div>
      </div>
    </Panel>
  );
}
