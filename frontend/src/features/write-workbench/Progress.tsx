import { cn } from "@/lib/utils";

export interface ProgressProps extends React.HTMLAttributes<HTMLDivElement> {
  value: number;
  max?: number;
  tone?: "default" | "success" | "warning" | "danger";
}

const toneToFill: Record<NonNullable<ProgressProps["tone"]>, string> = {
  default: "bg-[var(--accent)]",
  success: "bg-[var(--success)]",
  warning: "bg-[var(--warning)]",
  danger: "bg-[var(--danger)]",
};

export function Progress({
  value,
  max = 100,
  tone = "default",
  className,
  ...rest
}: ProgressProps) {
  const pct = Math.max(0, Math.min(100, (value / max) * 100));
  return (
    <div
      role="progressbar"
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
      className={cn(
        "h-2 w-full overflow-hidden rounded-full bg-[var(--muted)]",
        className,
      )}
      {...rest}
    >
      <div
        className={cn("h-full transition-all", toneToFill[tone])}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}
