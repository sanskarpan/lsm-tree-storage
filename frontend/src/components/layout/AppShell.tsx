import * as React from "react";

import { cn } from "@/lib/utils";

type GridColumn =
  | "span 3"
  | "span 4"
  | "span 5"
  | "span 6"
  | "span 7"
  | "span 8"
  | "span 9"
  | "span 12"
  | "full";

const columnToClass: Record<GridColumn, string> = {
  "span 3": "lg:col-span-3",
  "span 4": "lg:col-span-4",
  "span 5": "lg:col-span-5",
  "span 6": "lg:col-span-6",
  "span 7": "lg:col-span-7",
  "span 8": "lg:col-span-8",
  "span 9": "lg:col-span-9",
  "span 12": "lg:col-span-12",
  full: "col-span-full",
};

export interface AppShellProps {
  topBar?: React.ReactNode;
  errorBanner?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

function AppShellRoot({
  topBar,
  errorBanner,
  children,
  className,
}: AppShellProps) {
  return (
    <div className={cn("relative min-h-screen bg-[var(--bg)]", className)}>
      {topBar ? (
        <div className="border-b border-[var(--border)] bg-[var(--bg-elevated)]">
          {topBar}
        </div>
      ) : null}
      {errorBanner ? <div className="px-6 pt-4">{errorBanner}</div> : null}
      <main className="mx-auto w-full max-w-[1440px] px-6 py-6">
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-12">{children}</div>
      </main>
    </div>
  );
}

export interface AppShellPanelProps {
  gridColumn?: GridColumn;
  children: React.ReactNode;
  className?: string;
}

function AppShellPanel({
  gridColumn = "span 4",
  children,
  className,
}: AppShellPanelProps) {
  return (
    <div className={cn(columnToClass[gridColumn], "min-h-[240px]", className)}>
      {children}
    </div>
  );
}

export const AppShell = Object.assign(AppShellRoot, {
  Panel: AppShellPanel,
});

