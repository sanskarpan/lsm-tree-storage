import type { ActiveCompaction, CompactionLevelStat, CompactionStyle, FeedLine } from "../types";
import { Panel } from "./Panel";

type CompactionStudioProps = {
  activeCompaction: ActiveCompaction | null;
  compactionFeed: FeedLine[];
  compactionStats: CompactionLevelStat[];
  currentStyle: string;
  onForceCompaction: () => Promise<void>;
  onStyleChange: (style: CompactionStyle) => Promise<void>;
};

const STYLES: CompactionStyle[] = ["leveled", "size-tiered", "time-window"];

function toneClass(tone: FeedLine["tone"]): string {
  if (tone === "good") return "feed__item is-good";
  if (tone === "warn") return "feed__item is-warn";
  if (tone === "accent") return "feed__item is-accent";
  return "feed__item";
}

export function CompactionStudio({
  activeCompaction,
  compactionFeed,
  compactionStats,
  currentStyle,
  onForceCompaction,
  onStyleChange,
}: CompactionStudioProps) {
  const maxBytes = Math.max(...compactionStats.map((item) => item.total_size), 1);

  return (
    <Panel
      eyebrow="Compaction"
      title="Style and pressure"
      actions={
        <div className="inline-form">
          {STYLES.map((style) => (
            <button
              key={style}
              className={style === currentStyle ? "button--active" : "button--ghost"}
              onClick={() => void onStyleChange(style)}
            >
              {style}
            </button>
          ))}
          <button onClick={() => void onForceCompaction()}>Force L0</button>
        </div>
      }
    >
      <div className="stack">
        <div className="read-summary">
          <div>
            <p className="panel__eyebrow">Current state</p>
            <h3>{activeCompaction ? `Running L${activeCompaction.inputLevel} -> L${activeCompaction.outputLevel}` : "Idle"}</h3>
          </div>
          <div className={`hero__pill ${activeCompaction ? "is-live" : ""}`}>
            {activeCompaction ? "worker active" : "standing by"}
          </div>
        </div>

        <ul className="stat-list">
          {compactionStats.map((item) => (
            <li key={item.level} className="stat-list__row">
              <span>L{item.level}</span>
              <div className="stat-list__bar">
                <div className="stat-list__bar-fill stat-list__bar-fill--rose" style={{ width: `${(item.total_size / maxBytes) * 100}%` }} />
              </div>
              <strong>{item.num_files} files</strong>
            </li>
          ))}
        </ul>

        <div>
          <h3 className="section-title">Worker feed</h3>
          <ul className="feed">
            {compactionFeed.length > 0 ? (
              compactionFeed.map((entry) => (
                <li key={entry.id} className={toneClass(entry.tone)}>
                  <span>{entry.label}</span>
                  {entry.detail ? <small>{entry.detail}</small> : null}
                </li>
              ))
            ) : (
              <li className="feed__item">Compaction events will appear here.</li>
            )}
          </ul>
        </div>
      </div>
    </Panel>
  );
}
