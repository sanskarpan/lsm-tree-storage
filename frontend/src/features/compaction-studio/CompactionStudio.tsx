import * as React from "react";
import { Play } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import type {
  ActiveCompaction,
  CompactionLevelStat,
  CompactionStyle,
  FeedLine,
} from "../../types";

type CompactionStudioProps = {
  activeCompaction: ActiveCompaction | null;
  compactionFeed: FeedLine[];
  compactionStats: CompactionLevelStat[];
  currentStyle: string;
  onForceCompaction: () => Promise<void>;
  onStyleChange: (style: CompactionStyle) => Promise<void>;
};

const STYLES: CompactionStyle[] = ["leveled", "size-tiered", "time-window"];

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

export function CompactionStudio({
  activeCompaction,
  compactionFeed,
  compactionStats,
  currentStyle,
  onForceCompaction,
  onStyleChange,
}: CompactionStudioProps) {
  const maxBytes = Math.max(
    ...compactionStats.map((item) => item.total_size),
    1,
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>Style and pressure</CardTitle>
        <CardDescription>Compaction</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <div className="flex flex-col gap-2">
          <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
            Strategy
          </p>
          <div className="flex flex-wrap gap-2">
            {STYLES.map((style) => (
              <Button
                key={style}
                size="sm"
                variant={style === currentStyle ? "default" : "ghost"}
                onClick={() => void onStyleChange(style)}
              >
                {style}
              </Button>
            ))}
            <Button
              size="sm"
              variant="outline"
              onClick={() => void onForceCompaction()}
            >
              <Play className="h-3.5 w-3.5" />
              Force L0
            </Button>
          </div>
        </div>

        <div className="flex items-center justify-between gap-3 rounded-md border border-[var(--border)] p-4">
          <div className="flex flex-col gap-1">
            <p className="text-xs uppercase tracking-wider text-[var(--fg-muted)]">
              Current state
            </p>
            <p className="font-mono text-sm">
              {activeCompaction
                ? `Running L${activeCompaction.inputLevel} → L${activeCompaction.outputLevel}`
                : "Idle"}
            </p>
          </div>
          <Badge variant={activeCompaction ? "success" : "secondary"}>
            {activeCompaction ? "worker active" : "standing by"}
          </Badge>
        </div>

        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
            Level sizes
          </h4>
          {compactionStats.length > 0 ? (
            <ul className="flex flex-col gap-2">
              {compactionStats.map((item) => (
                <li
                  key={item.level}
                  className="flex items-center gap-3 text-sm"
                >
                  <Badge variant="info">L{item.level}</Badge>
                  <div className="flex-1">
                    <Progress
                      value={(item.total_size / maxBytes) * 100}
                      tone={item.total_size / maxBytes > 0.85 ? "danger" : "default"}
                      label={`Level ${item.level} size`}
                    />
                  </div>
                  <span className="font-mono text-xs text-[var(--fg-muted)]">
                    {item.num_files} files
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-[var(--fg-subtle)]">
              No level data yet.
            </p>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wider text-[var(--fg-muted)]">
            Worker feed
          </h4>
          {compactionFeed.length > 0 ? (
            <ul className="flex flex-col gap-1.5">
              {compactionFeed.map((entry) => (
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
              Compaction events will appear here.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
