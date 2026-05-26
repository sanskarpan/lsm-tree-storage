import type { PropsWithChildren, ReactNode } from "react";

type PanelProps = PropsWithChildren<{
  eyebrow: string;
  title: string;
  actions?: ReactNode;
  className?: string;
}>;

export function Panel({ eyebrow, title, actions, className, children }: PanelProps) {
  return (
    <section className={`panel ${className ?? ""}`.trim()}>
      <header className="panel__header">
        <div>
          <p className="panel__eyebrow">{eyebrow}</p>
          <h2 className="panel__title">{title}</h2>
        </div>
        {actions ? <div className="panel__actions">{actions}</div> : null}
      </header>
      <div className="panel__body">{children}</div>
    </section>
  );
}
