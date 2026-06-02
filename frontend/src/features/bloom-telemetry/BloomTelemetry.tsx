import * as React from "react";

import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Progress } from "../write-workbench/Progress";

type BloomTelemetryProps = {
  bloomStats: unknown;
};

type BloomStat = {
  file_id?: number;
  bits_per_key?: number;
  approximate_count?: number;
  size_bytes?: number;
  fpr_estimate?: number;
};

function isBloomStat(value: unknown): value is BloomStat {
  return typeof value === "object" && value !== null;
}

function asArray(value: unknown): BloomStat[] {
  if (Array.isArray(value)) return value.filter(isBloomStat);
  if (isBloomStat(value)) return [value];
  return [];
}

export function BloomTelemetry({ bloomStats }: BloomTelemetryProps) {
  const stats = React.useMemo(() => asArray(bloomStats), [bloomStats]);
  const totalSize = stats.reduce(
    (sum, stat) => sum + (stat.size_bytes ?? 0),
    0,
  );
  const meanFpr = stats.length
    ? stats.reduce((sum, stat) => sum + (stat.fpr_estimate ?? 0), 0) /
      stats.length
    : 0;
  const maxBits = Math.max(0, ...stats.map((s) => s.bits_per_key ?? 0));

  return (
    <Card>
      <CardHeader>
        <CardTitle>Bloom telemetry</CardTitle>
        <CardDescription>False-positive indicators</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-3">
          <div className="rounded-md border border-[var(--border)] p-3">
            <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
              Filters
            </p>
            <p className="mt-1 text-2xl font-semibold">{stats.length}</p>
          </div>
          <div className="rounded-md border border-[var(--border)] p-3">
            <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
              Total size
            </p>
            <p className="mt-1 text-2xl font-semibold">
              {totalSize < 1024
                ? `${totalSize} B`
                : `${(totalSize / 1024).toFixed(1)} KB`}
            </p>
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between text-sm">
            <span className="text-[var(--fg-muted)]">Mean FPR estimate</span>
            <span className="font-mono text-[var(--fg)]">
              {(meanFpr * 100).toFixed(2)}%
            </span>
          </div>
          <Progress
            value={meanFpr * 100}
            tone={
              meanFpr > 0.05
                ? "danger"
                : meanFpr > 0.02
                  ? "warning"
                  : "success"
            }
          />
        </div>

        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between text-sm">
            <span className="text-[var(--fg-muted)]">Max bits / key</span>
            <span className="font-mono text-[var(--fg)]">{maxBits}</span>
          </div>
          <Progress value={Math.min(100, (maxBits / 20) * 100)} />
        </div>

        {stats.length === 0 ? (
          <p className="rounded-md border border-dashed border-[var(--border)] p-4 text-center text-sm text-[var(--fg-muted)]">
            No bloom filters have been built yet.
          </p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {stats.slice(0, 10).map((stat, index) => (
              <li
                key={stat.file_id ?? index}
                className="flex items-center justify-between text-sm"
              >
                <span className="flex items-center gap-2">
                  <Badge variant="info">
                    #{stat.file_id ?? index}
                  </Badge>
                  <span className="text-[var(--fg-muted)]">
                    {stat.approximate_count ?? "?"} keys
                  </span>
                </span>
                <span className="font-mono text-xs text-[var(--fg-muted)]">
                  {stat.bits_per_key ?? "?"} bpk
                </span>
              </li>
            ))}
            {stats.length > 10 ? (
              <li className="text-xs text-[var(--fg-subtle)]">
                +{stats.length - 10} more filters
              </li>
            ) : null}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
