import * as React from "react";
import { Search } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import type { ReadTraceReport } from "../../types";

type ReadInspectorProps = {
  pending: boolean;
  trace: ReadTraceReport | null;
  onInspect: (key: string) => Promise<void>;
};

export function ReadInspector({ pending, trace, onInspect }: ReadInspectorProps) {
  const [key, setKey] = React.useState("live-a");

  const status: "found" | "missing" | "idle" = trace
    ? trace.found
      ? "found"
      : "missing"
    : "idle";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Query inspector</CardTitle>
        <CardDescription>Read path</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (key) void onInspect(key);
          }}
          className="flex flex-col gap-2"
        >
          <Label htmlFor="ri-key">Trace a key</Label>
          <div className="flex gap-2">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-[var(--fg-muted)]" />
              <Input
                id="ri-key"
                value={key}
                onChange={(event) => setKey(event.target.value)}
                placeholder="key"
                className="pl-8"
              />
            </div>
            <Button type="submit" disabled={pending || !key}>
              {pending ? "Tracing..." : "Trace read"}
            </Button>
          </div>
        </form>

        <div className="flex items-center justify-between gap-3 rounded-md border border-[var(--border)] p-4">
          <div className="flex flex-col gap-1">
            <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
              Result
            </p>
            {trace ? (
              <Badge
                variant={trace.found ? "success" : "danger"}
                className="text-sm"
              >
                {trace.found ? "Found" : "Missing"}
              </Badge>
            ) : (
              <Badge variant="secondary" className="text-sm">
                Awaiting query
              </Badge>
            )}
          </div>
          <div className="flex flex-col items-end gap-1 text-right">
            <span className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
              Status
            </span>
            <span className="font-mono text-2xl font-semibold">
              {trace?.status ?? 0}
            </span>
          </div>
        </div>

        {pending && !trace ? (
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {[...Array(4)].map((_, index) => (
              <Skeleton key={index} className="h-16" />
            ))}
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <MetricTile label="Bloom checks" value={trace?.bloomChecks ?? 0} />
            <MetricTile label="Bloom misses" value={trace?.bloomMisses ?? 0} />
            <MetricTile label="Memtable hits" value={trace?.memtableHits ?? 0} />
            <MetricTile label="SSTable hits" value={trace?.sstableHits ?? 0} />
          </div>
        )}

        {trace?.value ? (
          <div className="rounded-md border border-[var(--border)] bg-[var(--bg-sunken)] p-3">
            <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
              Captured value
            </p>
            <p className="mt-1 break-all font-mono text-sm">{trace.value}</p>
          </div>
        ) : null}

        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
            Captured execution steps
          </h4>
          {trace?.steps.length ? (
            <ol className="flex flex-col gap-1.5 text-sm">
              {trace.steps.map((step, index) => (
                <li
                  key={`${step}-${index}`}
                  className="flex gap-2 rounded-md border border-[var(--border)] p-2"
                >
                  <span className="font-mono text-xs text-[var(--fg-muted)]">
                    {String(index + 1).padStart(2, "0")}
                  </span>
                  <span>{step}</span>
                </li>
              ))}
            </ol>
          ) : (
            <p className="text-sm text-[var(--fg-subtle)]">
              No read trace captured yet.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function MetricTile({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex flex-col gap-1 rounded-md border border-[var(--border)] p-3">
      <span className="text-xs text-[var(--fg-muted)]">{label}</span>
      <strong className="font-mono text-xl">{value}</strong>
    </div>
  );
}
