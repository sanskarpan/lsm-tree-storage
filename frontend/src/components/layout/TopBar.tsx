import * as React from "react";
import { Moon, Sun, Rows3, Rows4 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { EngineConfig, EngineStats, RuntimeState } from "../../types";
import { useTheme } from "../../hooks/useTheme";

type TopBarProps = {
  connected: boolean;
  runtime: RuntimeState | null;
  stats: EngineStats | null;
  config: EngineConfig | null;
  sessionWrites: number;
  sessionFlushes: number;
  sessionCompactions: number;
};

function compactNumber(n: number | undefined): string {
  if (n == null) return "—";
  if (n < 1000) return n.toString();
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}K`;
  return `${(n / 1_000_000).toFixed(2)}M`;
}

function pct(n: number | null | undefined): string {
  if (n == null) return "—";
  return `${Math.round(n * 100)}%`;
}

export function TopBar({
  connected,
  runtime,
  stats,
  config,
  sessionWrites,
  sessionFlushes,
  sessionCompactions,
}: TopBarProps) {
  const { theme, density, setTheme, setDensity } = useTheme();

  return (
    <header className="mx-auto flex w-full max-w-[1440px] items-center justify-between gap-4 px-6 py-3">
      <div className="flex items-center gap-3">
        <p className="font-mono text-xs uppercase tracking-[0.18em] text-[var(--accent)]">
          LSM Control Room
        </p>
        <span className="text-xs text-[var(--fg-subtle)]">·</span>
        <p className="text-sm text-[var(--fg-muted)]">
          Storage telemetry for the LSM engine running on this node.
        </p>
      </div>

      <div className="flex items-center gap-4">
        <dl className="hidden items-center gap-4 text-xs text-[var(--fg-muted)] md:flex">
          <div className="flex flex-col">
            <dt className="font-mono uppercase tracking-wider text-[10px]">SeqNo</dt>
            <dd className="font-mono text-sm text-[var(--fg)]">
              {compactNumber(stats?.seq_no)}
            </dd>
          </div>
          <div className="flex flex-col">
            <dt className="font-mono uppercase tracking-wider text-[10px]">Writes</dt>
            <dd className="font-mono text-sm text-[var(--fg)]">
              {compactNumber(sessionWrites)}
            </dd>
          </div>
          <div className="flex flex-col">
            <dt className="font-mono uppercase tracking-wider text-[10px]">Flushes</dt>
            <dd className="font-mono text-sm text-[var(--fg)]">
              {compactNumber(sessionFlushes)}
            </dd>
          </div>
          <div className="flex flex-col">
            <dt className="font-mono uppercase tracking-wider text-[10px]">Compactions</dt>
            <dd className="font-mono text-sm text-[var(--fg)]">
              {compactNumber(sessionCompactions)}
            </dd>
          </div>
          <div className="flex flex-col">
            <dt className="font-mono uppercase tracking-wider text-[10px]">Cache</dt>
            <dd className="font-mono text-sm text-[var(--fg)]">
              {pct(stats?.cache_hit_rate)}
            </dd>
          </div>
          <div className="flex flex-col">
            <dt className="font-mono uppercase tracking-wider text-[10px]">Style</dt>
            <dd className="font-mono text-sm text-[var(--fg)]">
              {runtime?.CompactionStyle ?? config?.CompactionStyle ?? "leveled"}
            </dd>
          </div>
        </dl>

        <div
          className={
            "inline-flex items-center gap-2 rounded-md border px-2.5 py-1 text-xs " +
            (connected
              ? "border-[var(--success)]/40 bg-[var(--success)]/10 text-[var(--fg)]"
              : "border-[var(--border)] bg-[var(--muted)] text-[var(--fg-muted)]")
          }
        >
          <span
            className={
              "h-2 w-2 rounded-full " +
              (connected ? "bg-[var(--success)]" : "bg-[var(--fg-subtle)]")
            }
            aria-hidden="true"
          />
          {connected ? "Live" : "Reconnecting"}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="Theme">
              {theme === "dark" ? (
                <Moon className="h-4 w-4" />
              ) : (
                <Sun className="h-4 w-4" />
              )}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Theme</DropdownMenuLabel>
            <DropdownMenuRadioGroup
              value={theme}
              onValueChange={(value) => setTheme(value as "light" | "dark")}
            >
              <DropdownMenuRadioItem value="light">
                <Sun className="h-3.5 w-3.5" /> Light
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="dark">
                <Moon className="h-3.5 w-3.5" /> Dark
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="Density">
              {density === "compact" ? (
                <Rows3 className="h-4 w-4" />
              ) : (
                <Rows4 className="h-4 w-4" />
              )}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Density</DropdownMenuLabel>
            <DropdownMenuRadioGroup
              value={density}
              onValueChange={(value) =>
                setDensity(value as "comfortable" | "compact")
              }
            >
              <DropdownMenuRadioItem value="comfortable">
                <Rows4 className="h-3.5 w-3.5" /> Comfortable
              </DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="compact">
                <Rows3 className="h-3.5 w-3.5" /> Compact
              </DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
