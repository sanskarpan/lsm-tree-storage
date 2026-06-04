import * as React from "react";
import { AlertCircle, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface BannerProps {
  tone?: "info" | "warning" | "danger" | "success";
  dismissible?: boolean;
  onDismiss?: () => void;
  children: React.ReactNode;
  className?: string;
  id?: string;
}

const toneStyles: Record<
  NonNullable<BannerProps["tone"]>,
  { border: string; bg: string; text: string; iconColor: string }
> = {
  info: {
    border: "border-[var(--accent)]/40",
    bg: "bg-[var(--accent)]/10",
    text: "text-[var(--fg)]",
    iconColor: "text-[var(--accent)]",
  },
  warning: {
    border: "border-[var(--warning)]/40",
    bg: "bg-[var(--warning)]/10",
    text: "text-[var(--fg)]",
    iconColor: "text-[var(--warning)]",
  },
  danger: {
    border: "border-[var(--danger)]/40",
    bg: "bg-[var(--danger)]/10",
    text: "text-[var(--fg)]",
    iconColor: "text-[var(--danger)]",
  },
  success: {
    border: "border-[var(--success)]/40",
    bg: "bg-[var(--success)]/10",
    text: "text-[var(--fg)]",
    iconColor: "text-[var(--success)]",
  },
};

export function Banner({
  tone = "info",
  dismissible = false,
  onDismiss,
  children,
  className,
  id,
}: BannerProps) {
  const styles = toneStyles[tone];
  return (
    <div
      id={id}
      role={tone === "danger" || tone === "warning" ? "alert" : "status"}
      className={cn(
        "mx-auto flex w-full max-w-[1440px] items-start gap-3 rounded-md border px-4 py-3 text-sm",
        styles.border,
        styles.bg,
        styles.text,
        className,
      )}
    >
      <AlertCircle
        className={cn("mt-0.5 h-4 w-4 flex-shrink-0", styles.iconColor)}
        aria-hidden="true"
      />
      <div className="flex-1">{children}</div>
      {dismissible ? (
        <Button
          variant="ghost"
          size="icon"
          onClick={onDismiss}
          aria-label="Dismiss banner"
          className="h-6 w-6"
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      ) : null}
    </div>
  );
}
