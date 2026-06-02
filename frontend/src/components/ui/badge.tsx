import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-[var(--ring)] focus:ring-offset-1 focus:ring-offset-[var(--bg)]",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-[var(--primary)] text-[var(--primary-foreground)]",
        secondary:
          "border-transparent bg-[var(--secondary)] text-[var(--secondary-foreground)]",
        destructive:
          "border-transparent bg-[var(--destructive)] text-[var(--destructive-foreground)]",
        success:
          "border-transparent bg-[var(--success)] text-[var(--bg)]",
        warning:
          "border-transparent bg-[var(--warning)] text-[var(--bg)]",
        danger:
          "border-transparent bg-[var(--danger)] text-[var(--bg)]",
        info: "border-transparent bg-[var(--info)] text-[var(--bg)]",
        outline:
          "border-[var(--border)] text-[var(--fg)]",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { Badge, badgeVariants };
