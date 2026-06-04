import * as React from "react";
import { Play, Power, Zap } from "lucide-react";

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
import { Select } from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import type {
  BenchRequest,
  BenchResult,
  EngineConfig,
  FeedLine,
  RuntimeState,
  ScenarioInfo,
} from "../../types";

type ScenarioLabProps = {
  runtime: RuntimeState | null;
  config: EngineConfig | null;
  scenarios: ScenarioInfo[];
  benchmarkResult: BenchResult | null;
  closeMessage: string | null;
  opsFeed: FeedLine[];
  onScenarioRun: (name: string) => Promise<void>;
  onBenchRun: (request: BenchRequest) => Promise<void>;
  onCloseAttempt: () => Promise<void>;
};

function toneToVariant(tone: FeedLine["tone"]) {
  switch (tone) {
    case "good":
      return "success" as const;
    case "warn":
      return "warning" as const;
    case "danger":
      return "danger" as const;
    case "accent":
      return "info" as const;
    default:
      return "secondary" as const;
  }
}

export function ScenarioLab({
  runtime,
  config,
  scenarios,
  benchmarkResult,
  closeMessage,
  opsFeed,
  onScenarioRun,
  onBenchRun,
  onCloseAttempt,
}: ScenarioLabProps) {
  const [scenarioName, setScenarioName] = React.useState<string>("");
  const [benchType, setBenchType] = React.useState("sequential_write");
  const [benchKeys, setBenchKeys] = React.useState(2000);
  const [benchValueSize, setBenchValueSize] = React.useState(128);
  const [closeDialogOpen, setCloseDialogOpen] = React.useState(false);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Operations lab</CardTitle>
        <CardDescription>Scenarios &amp; benchmarks</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <div className="flex flex-col gap-3 rounded-md border border-[var(--border)] p-4">
            <div className="flex flex-col gap-1">
              <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
                Runtime
              </p>
              <p className="font-mono text-sm">
                {runtime?.DataDir ?? "unavailable"}
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <Badge variant="info">
                WAL #{runtime?.ActiveLogNumber ?? "?"}
              </Badge>
              <Badge variant={runtime?.SyncWAL ? "success" : "secondary"}>
                sync {runtime?.SyncWAL ? "on" : "off"}
              </Badge>
              <span className="text-[var(--fg-muted)]">
                target {(config?.MemTableSize ?? 0).toLocaleString()} B
              </span>
            </div>
          </div>

          <div className="flex flex-col gap-3 rounded-md border border-[var(--border)] p-4">
            <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
              Benchmark
            </p>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
              <div className="flex flex-col gap-1">
                <Label htmlFor="bench-type">Workload</Label>
                <Input
                  id="bench-type"
                  value={benchType}
                  onChange={(event) => setBenchType(event.target.value)}
                  placeholder="workload type"
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="bench-keys">Keys</Label>
                <Input
                  id="bench-keys"
                  type="number"
                  min={1}
                  value={benchKeys}
                  onChange={(event) =>
                    setBenchKeys(Number(event.target.value) || 0)
                  }
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="bench-vsize">Value bytes</Label>
                <Input
                  id="bench-vsize"
                  type="number"
                  min={1}
                  value={benchValueSize}
                  onChange={(event) =>
                    setBenchValueSize(Number(event.target.value) || 0)
                  }
                />
              </div>
            </div>
            <div className="flex items-center justify-between gap-3">
              <Button
                size="sm"
                onClick={() =>
                  void onBenchRun({
                    type: benchType,
                    num_keys: benchKeys,
                    value_size: benchValueSize,
                  })
                }
              >
                <Zap className="h-3.5 w-3.5" />
                Run bench
              </Button>
              {benchmarkResult ? (
                <div className="flex flex-wrap items-center gap-3 text-sm">
                  <Badge variant="success">
                    {Math.round(benchmarkResult.ops_per_sec).toLocaleString()}{" "}
                    ops/sec
                  </Badge>
                  <span className="text-[var(--fg-muted)]">
                    p99 write {benchmarkResult.p99_write_us} µs
                  </span>
                  <span className="text-[var(--fg-muted)]">
                    p99 read {benchmarkResult.p99_read_us} µs
                  </span>
                </div>
              ) : null}
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-3 rounded-md border border-[var(--border)] p-4">
          <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
            <div className="flex flex-col gap-1">
              <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
                Scenario runner
              </p>
              <p className="text-sm text-[var(--fg-muted)]">
                Pick a pre-baked workload from the server.
              </p>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
              <div className="min-w-[14rem]">
                <Label htmlFor="scenario-name" className="sr-only">
                  Scenario
                </Label>
                <Select
                  id="scenario-name"
                  value={scenarioName}
                  onChange={setScenarioName}
                  placeholder="Select scenario…"
                  options={scenarios.map((scenario) => ({
                    value: scenario.name,
                    label: scenario.name,
                  }))}
                />
              </div>
              <Button
                size="sm"
                disabled={!scenarioName}
                onClick={() => void onScenarioRun(scenarioName)}
              >
                <Play className="h-3.5 w-3.5" />
                Run scenario
              </Button>
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-3 rounded-md border border-[var(--border)] p-4">
          <div className="flex items-center justify-between">
            <div className="flex flex-col gap-1">
              <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
                Lifecycle guardrail
              </p>
              <h4 className="text-sm font-semibold">
                Remote close behaviour
              </h4>
            </div>
            <Dialog open={closeDialogOpen} onOpenChange={setCloseDialogOpen}>
              <DialogTrigger asChild>
                <Button variant="ghost" size="sm">
                  <Power className="h-3.5 w-3.5" />
                  Attempt close
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Attempt a remote close?</DialogTitle>
                  <DialogDescription>
                    The /close endpoint is reserved for local lifecycle
                    management. Remote callers will receive a 403 with the
                    reason below.
                  </DialogDescription>
                </DialogHeader>
                <DialogFooter className="gap-2">
                  <Button
                    variant="ghost"
                    onClick={() => setCloseDialogOpen(false)}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="destructive"
                    onClick={() => {
                      setCloseDialogOpen(false);
                      void onCloseAttempt();
                    }}
                  >
                    Send anyway
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>
          <p className="text-sm text-[var(--fg-muted)]">
            {closeMessage ??
              "The server now rejects remote lifecycle shutdown explicitly instead of pretending to close."}
          </p>
        </div>

        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
            Operations feed
          </h4>
          {opsFeed.length > 0 ? (
            <ul className="flex flex-col gap-1.5">
              {opsFeed.map((entry) => (
                <li
                  key={entry.id}
                  className="flex items-center gap-2 text-sm"
                >
                  <Badge variant={toneToVariant(entry.tone)}>
                    {entry.label}
                  </Badge>
                  {entry.detail ? (
                    <span className="text-[var(--fg-muted)]">{entry.detail}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-[var(--fg-subtle)]">
              Scenario and benchmark results will appear here.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
