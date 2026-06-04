import * as React from "react";

import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import type { AmpPoint } from "../../types";

type AmplificationDeckProps = {
  wa: number;
  ra: number;
  sa: number;
  history: AmpPoint[];
};

type Channel = "wa" | "ra" | "sa";

const CHANNEL_META: Record<
  Channel,
  { label: string; cssVar: string; threshold: number }
> = {
  wa: { label: "Write amp", cssVar: "--success", threshold: 8 },
  ra: { label: "Read amp", cssVar: "--warning", threshold: 8 },
  sa: { label: "Space amp", cssVar: "--danger", threshold: 4 },
};

function Gauge({
  label,
  value,
  cssVar,
}: {
  label: string;
  value: number;
  cssVar: string;
}) {
  const radius = 42;
  const circumference = 2 * Math.PI * radius;
  const max = 16;
  const progress = Math.min(value / max, 1);
  const dash = circumference * progress;

  return (
    <div className="flex flex-col items-center gap-2">
      <svg viewBox="0 0 120 120" className="h-24 w-24 -rotate-90">
        <circle
          cx="60"
          cy="60"
          r={radius}
          fill="none"
          stroke="var(--muted)"
          strokeWidth="8"
        />
        <circle
          cx="60"
          cy="60"
          r={radius}
          fill="none"
          stroke={`var(${cssVar})`}
          strokeWidth="8"
          strokeLinecap="round"
          strokeDasharray={`${dash} ${circumference - dash}`}
        />
      </svg>
      <div className="flex flex-col items-center">
        <strong className="font-mono text-xl">{value.toFixed(1)}x</strong>
        <span className="text-xs text-[var(--fg-muted)]">{label}</span>
      </div>
    </div>
  );
}

export function AmplificationDeck({ wa, ra, sa, history }: AmplificationDeckProps) {
  const points =
    history.length > 0 ? history : [{ wa, ra, sa, timestamp: Date.now() }];
  const maxValue = Math.max(
    ...points.flatMap((point) => [point.wa, point.ra, point.sa]),
    1,
  );

  const polyline = (channel: Channel) =>
    points
      .map((point, index) => {
        const x = (index / Math.max(points.length - 1, 1)) * 100;
        const y = 48 - (point[channel] / maxValue) * 40;
        return `${x},${y}`;
      })
      .join(" ");

  return (
    <Card>
      <CardHeader>
        <CardTitle>Stress profile</CardTitle>
        <CardDescription>Amplification</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <div className="grid grid-cols-3 gap-3">
          {(["wa", "ra", "sa"] as const).map((channel) => {
            const value = channel === "wa" ? wa : channel === "ra" ? ra : sa;
            const meta = CHANNEL_META[channel];
            return (
              <div
                key={channel}
                className="flex flex-col items-center gap-3 rounded-md border border-[var(--border)] p-4"
              >
                <Gauge
                  label={meta.label}
                  value={value}
                  cssVar={meta.cssVar}
                />
                <Progress
                  value={Math.min(100, (value / meta.threshold) * 50)}
                  tone={
                    value > meta.threshold
                      ? "danger"
                      : value > meta.threshold * 0.75
                        ? "warning"
                        : "success"
                  }
                  label={`${meta.label} threshold progress`}
                  className="w-full"
                />
                <Badge
                  variant={
                    value > meta.threshold
                      ? "danger"
                      : value > meta.threshold * 0.75
                        ? "warning"
                        : "secondary"
                  }
                >
                  {value > meta.threshold
                    ? "elevated"
                    : value > meta.threshold * 0.75
                      ? "watch"
                      : "stable"}
                </Badge>
              </div>
            );
          })}
        </div>

        <div className="flex flex-col gap-2 rounded-md border border-[var(--border)] p-3">
          <div className="flex items-center justify-between">
            <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
              Recent amplification signal
            </h4>
            <span className="text-xs text-[var(--fg-subtle)]">
              driven by live events
            </span>
          </div>
          <svg
            viewBox="0 0 100 52"
            preserveAspectRatio="none"
            className="h-32 w-full"
          >
            {(["wa", "ra", "sa"] as const).map((channel) => (
              <polyline
                key={channel}
                points={polyline(channel)}
                fill="none"
                stroke={`var(${CHANNEL_META[channel].cssVar})`}
                strokeWidth="0.6"
                vectorEffect="non-scaling-stroke"
              />
            ))}
          </svg>
          <div className="flex items-center justify-end gap-3 text-[10px] text-[var(--fg-muted)]">
            <span className="flex items-center gap-1">
              <span className="h-0.5 w-3 bg-[var(--success)]" /> WA
            </span>
            <span className="flex items-center gap-1">
              <span className="h-0.5 w-3 bg-[var(--warning)]" /> RA
            </span>
            <span className="flex items-center gap-1">
              <span className="h-0.5 w-3 bg-[var(--danger)]" /> SA
            </span>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
