import * as React from "react";

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "./Progress";
import type { FeedLine, MemtableSnapshotResponse, WalEntriesResponse } from "../../types";

type WriteWorkbenchProps = {
  capacityBytes: number;
  memtable: MemtableSnapshotResponse | null;
  walEntries: WalEntriesResponse | null;
  writeFeed: FeedLine[];
  onPut: (key: string, value: string) => Promise<void>;
  onDelete: (key: string) => Promise<void>;
};

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

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

export function WriteWorkbench({
  capacityBytes,
  memtable,
  walEntries,
  writeFeed,
  onPut,
  onDelete,
}: WriteWorkbenchProps) {
  const [key, setKey] = React.useState("");
  const [value, setValue] = React.useState("");
  const [pending, setPending] = React.useState(false);

  const mutable = memtable?.mutable;
  const approxBytes = mutable?.approximate_size ?? 0;
  const cap = Math.max(capacityBytes, 1);
  const fill = Math.min(100, (approxBytes / cap) * 100);

  async function submitPut() {
    if (!key) return;
    setPending(true);
    try {
      await onPut(key, value);
      setValue("");
    } finally {
      setPending(false);
    }
  }

  async function submitDelete() {
    if (!key) return;
    setPending(true);
    try {
      await onDelete(key);
      setValue("");
    } finally {
      setPending(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Command deck</CardTitle>
        <CardDescription>Write path</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void submitPut();
          }}
          className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_1fr_auto_auto]"
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="wb-key">Key</Label>
            <Input
              id="wb-key"
              value={key}
              onChange={(event) => setKey(event.target.value)}
              placeholder="key"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="wb-value">Value</Label>
            <Input
              id="wb-value"
              value={value}
              onChange={(event) => setValue(event.target.value)}
              placeholder="value"
            />
          </div>
          <div className="flex items-end">
            <Button type="submit" disabled={pending || !key}>
              PUT
            </Button>
          </div>
          <div className="flex items-end">
            <Button
              type="button"
              variant="ghost"
              disabled={pending || !key}
              onClick={() => void submitDelete()}
            >
              DEL
            </Button>
          </div>
        </form>

        <div className="flex flex-col gap-2 rounded-md border border-[var(--border)] p-4">
          <div className="flex items-center justify-between text-sm">
            <span className="text-[var(--fg-muted)]">Mutable memtable</span>
            <span className="font-mono text-[var(--fg)]">
              {formatBytes(approxBytes)} / {formatBytes(cap)}
            </span>
          </div>
          <Progress value={fill} />
          <p className="text-xs text-[var(--fg-muted)]">
            Active log #{memtable?.active_log_number ?? "?"} ·{" "}
            {memtable?.immutables.length ?? 0} immutable
            {memtable?.immutables.length === 1 ? "" : "s"} queued
          </p>
        </div>

        <DataTable
          aria-label="Recent WAL activity"
          searchPlaceholder="Filter WAL…"
          columns={[
            {
              accessorKey: "type",
              header: "Type",
              size: 80,
              cell: (info) => (
                <Badge
                  variant={
                    info.getValue<string>() === "delete" ? "danger" : "info"
                  }
                >
                  {info.getValue<string>()}
                </Badge>
              ),
            },
            {
              accessorKey: "key",
              header: "Key",
              size: 240,
              cell: (info) => (
                <span className="font-mono text-xs">
                  {info.getValue<string>() || "sync"}
                </span>
              ),
            },
            {
              accessorKey: "seq_no",
              header: "SeqNo",
              size: 100,
              cell: (info) => {
                const v = info.getValue<number | undefined>();
                return (
                  <span className="font-mono text-xs text-[var(--fg-muted)]">
                    {v != null ? `seq ${v}` : "checkpoint"}
                  </span>
                );
              },
            },
            {
              accessorKey: "timestamp_unix_nano",
              header: "Time",
              size: 140,
              cell: (info) => {
                const ns = info.getValue<number>();
                return (
                  <span className="font-mono text-xs text-[var(--fg-muted)]">
                    {new Date(ns / 1_000_000).toISOString().slice(11, 19)}
                  </span>
                );
              },
            },
          ]}
          data={walEntries?.entries ?? []}
          emptyState="Waiting for WAL traffic."
          virtualizeThreshold={50}
        />

        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
            Session write feed
          </h4>
          {writeFeed.length === 0 ? (
            <p className="text-sm text-[var(--fg-subtle)]">No live writes yet.</p>
          ) : (
            <ul className="flex flex-col gap-1.5">
              {writeFeed.map((entry) => (
                <li
                  key={entry.id}
                  className="flex items-center gap-2 text-sm"
                >
                  <Badge variant={toneToVariant(entry.tone)}>{entry.label}</Badge>
                  {entry.detail ? (
                    <span className="text-[var(--fg-muted)]">{entry.detail}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
            Mutable records
          </h4>
          {mutable?.entries.length ? (
            <div className="flex flex-wrap gap-1.5">
              {mutable.entries.map((entry) => (
                <Badge
                  key={`${entry.key}-${entry.seq_no}`}
                  variant={entry.type === "delete" ? "danger" : "secondary"}
                >
                  <span className="font-mono">{entry.key}</span>
                  <span className="ml-1.5 text-[10px] text-[var(--fg-muted)]">
                    seq {entry.seq_no}
                  </span>
                </Badge>
              ))}
            </div>
          ) : (
            <p className="text-sm text-[var(--fg-subtle)]">
              The mutable memtable is empty right now.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
