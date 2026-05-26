import type { EngineEvent, FeedLine } from "../../types";

export type TimedEvent = {
  receivedAt: number;
  event: EngineEvent;
};

export function limitLines(lines: FeedLine[], next: FeedLine, max = 28): FeedLine[] {
  return [next, ...lines].slice(0, max);
}

export function line(tone: FeedLine["tone"], label: string, detail?: string): FeedLine {
  return {
    id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
    tone,
    label,
    detail,
    timestamp: Date.now(),
  };
}

export function num(value: unknown): number | undefined {
  return typeof value === "number" ? value : undefined;
}

export function str(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
