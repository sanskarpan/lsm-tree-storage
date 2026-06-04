import * as React from "react";
import { ChevronDown } from "lucide-react";

import { cn } from "@/lib/utils";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface SelectProps {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  "aria-label"?: string;
  className?: string;
}

export function Select({
  id,
  value,
  onChange,
  options,
  placeholder,
  disabled,
  "aria-label": ariaLabel,
  className,
}: SelectProps) {
  return (
    <div className={cn("relative w-full", className)}>
      <select
        id={id}
        value={value}
        disabled={disabled}
        aria-label={ariaLabel}
        onChange={(event) => onChange(event.target.value)}
        className={cn(
          "flex h-9 w-full appearance-none rounded-md border border-[var(--border)] bg-[var(--bg)] px-3 pr-9 py-1 text-sm text-[var(--fg)]",
          "shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] focus-visible:ring-offset-1",
          "disabled:cursor-not-allowed disabled:opacity-50",
        )}
      >
        {placeholder ? (
          <option value="" disabled>
            {placeholder}
          </option>
        ) : null}
        {options.map((option) => (
          <option
            key={option.value}
            value={option.value}
            disabled={option.disabled}
          >
            {option.label}
          </option>
        ))}
      </select>
      <ChevronDown
        aria-hidden="true"
        className="pointer-events-none absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-[var(--fg-muted)]"
      />
    </div>
  );
}
