import * as React from "react";
import { RefreshCw } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { DataTable } from "@/components/ui/data-table";
import { Progress } from "@/components/ui/progress";
import type { CompactionLevelStat, LevelInfo, MemtableSnapshotResponse } from "../../types";

type LevelMatrixProps = {
  levels: LevelInfo[];
  memtable: MemtableSnapshotResponse | null;
  compactionStats: CompactionLevelStat[];
  onRefresh: () => Promise<void>;
};

function bytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

function levelTone(level: number, fillPct: number): "default" | "warning" | "danger" {
  if (level === 0 && fillPct > 80) return "danger";
  if (fillPct > 90) return "danger";
  if (fillPct > 70) return "warning";
  return "default";
}

export function LevelMatrix({ levels, memtable, compactionStats, onRefresh }: LevelMatrixProps) {
  const maxLevelBytes = Math.max(...levels.map((l) => l.total_size), 1);

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-3 space-y-0">
        <div className="flex flex-col gap-1">
          <CardTitle>Level matrix</CardTitle>
          <CardDescription>Topology</CardDescription>
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => void onRefresh()}
          aria-label="Refresh level snapshot"
        >
          <RefreshCw className="h-3.5 w-3.5" />
          Refresh
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4">
          {levels.map((level) => {
            const fill = (level.total_size / maxLevelBytes) * 100;
            return (
              <div
                key={level.level}
                className="flex flex-col gap-2 rounded-md border border-[var(--border)] p-3"
              >
                <div className="flex items-center justify-between">
                  <span className="text-sm font-semibold">L{level.level}</span>
                  <Badge variant="secondary">
                    {level.num_files} {level.num_files === 1 ? "file" : "files"}
                  </Badge>
                </div>
                <Progress
                  value={fill}
                  tone={levelTone(level.level, fill)}
                  label={`Level ${level.level} size`}
                />
                <p className="font-mono text-xs text-[var(--fg-muted)]">
                  {bytes(level.total_size)}
                </p>
                <div className="flex flex-wrap gap-1">
                  {level.files.length > 0 ? (
                    level.files.slice(0, 12).map((file) => (
                      <span
                        key={file.file_id}
                        title={`#${file.file_id} ${file.first_key} → ${file.last_key}`}
                        className="inline-flex items-center gap-1 rounded border border-[var(--border)] bg-[var(--bg-sunken)] px-1.5 py-0.5 text-[10px] font-mono"
                      >
                        #{file.file_id}
                      </span>
                    ))
                  ) : (
                    <span className="text-xs text-[var(--fg-subtle)]">No files</span>
                  )}
                  {level.files.length > 12 ? (
                    <span className="text-[10px] text-[var(--fg-subtle)]">
                      +{level.files.length - 12} more
                    </span>
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>

        <DataTable
          aria-label="Compaction balance by level"
          searchPlaceholder="Filter levels…"
          columns={[
            {
              accessorKey: "level",
              header: "Level",
              size: 100,
              cell: (info) => (
                <Badge variant="info">L{info.getValue<number>()}</Badge>
              ),
            },
            {
              accessorKey: "total_size",
              header: "Size",
              size: 120,
              cell: (info) => (
                <span className="font-mono text-xs">
                  {bytes(info.getValue<number>())}
                </span>
              ),
            },
            {
              accessorKey: "level",
              header: "Fill",
              size: 200,
              cell: (info) => {
                const fill = (info.getValue<number>() / maxLevelBytes) * 100;
                return <Progress value={fill} label={`Level ${info.getValue<number>()} fill`} />;
              },
              enableSorting: false,
            },
          ]}
          data={compactionStats}
        />

        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
            Memtable ownership
          </h4>
          <div className="flex flex-col gap-1.5 text-sm">
            <div className="flex items-center gap-2">
              <Badge variant="success">active</Badge>
              <span className="font-mono text-xs">
                log #{memtable?.active_log_number ?? "?"}
              </span>
              <span className="text-[var(--fg-muted)]">
                {memtable?.active_wal_path ?? "unavailable"}
              </span>
            </div>
            {memtable?.immutables.map((immutable) => (
              <div
                key={immutable.log_number}
                className="flex items-center gap-2"
              >
                <Badge variant="warning">immutable</Badge>
                <span className="font-mono text-xs">
                  log #{immutable.log_number} · {immutable.table.entries.length} rows
                </span>
                <span className="truncate text-[var(--fg-muted)]">
                  {immutable.wal_path}
                </span>
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
