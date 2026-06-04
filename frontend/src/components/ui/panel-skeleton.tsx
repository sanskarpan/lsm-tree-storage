import { Skeleton } from "./skeleton";

export function PanelSkeleton({ lines = 3 }: { lines?: number }) {
  return (
    <div
      role="status"
      aria-label="Loading panel"
      className="flex flex-col gap-3 rounded-md border border-[var(--border)] bg-[var(--bg)] p-4"
    >
      <Skeleton className="h-5 w-1/3" />
      <Skeleton className="h-3 w-2/3" />
      <div className="mt-2 flex flex-col gap-2">
        {Array.from({ length: lines }).map((_, i) => (
          <Skeleton key={i} className="h-4 w-full" />
        ))}
      </div>
    </div>
  );
}
