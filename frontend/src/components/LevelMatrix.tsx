import type { CompactionLevelStat, LevelInfo, MemtableSnapshotResponse } from "../types";
import { Panel } from "./Panel";

type LevelMatrixProps = {
  levels: LevelInfo[];
  memtable: MemtableSnapshotResponse | null;
  compactionStats: CompactionLevelStat[];
  onRefresh: () => Promise<void>;
};

function bytes(value: number): string {
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

export function LevelMatrix({ levels, memtable, compactionStats, onRefresh }: LevelMatrixProps) {
  const maxLevelBytes = Math.max(...levels.map((level) => level.total_size), 1);

  return (
    <Panel
      eyebrow="Topology"
      title="Level matrix"
      actions={
        <button className="button--ghost" onClick={() => void onRefresh()}>
          Refresh view
        </button>
      }
    >
      <div className="stack">
        <div className="level-grid">
          {levels.map((level) => (
            <article key={level.level} className="level-card">
              <div className="level-card__topline">
                <span>L{level.level}</span>
                <strong>{level.num_files} files</strong>
              </div>
              <div className="meter">
                <div className="meter__fill meter__fill--amber" style={{ width: `${(level.total_size / maxLevelBytes) * 100}%` }} />
              </div>
              <p className="level-card__meta">{bytes(level.total_size)}</p>
              <div className="file-strip">
                {level.files.length > 0 ? (
                  level.files.map((file) => (
                    <div
                      key={file.file_id}
                      className="file-chip"
                      title={`#${file.file_id} ${file.first_key} → ${file.last_key}`}
                      style={{ flexGrow: Math.max(1, Math.round(file.file_size / 4096)) }}
                    >
                      <span>#{file.file_id}</span>
                      <small>{file.first_key}</small>
                    </div>
                  ))
                ) : (
                  <div className="file-chip file-chip--empty">No files</div>
                )}
              </div>
            </article>
          ))}
        </div>

        <div className="two-column">
          <div>
            <h3 className="section-title">Compaction balance</h3>
            <ul className="stat-list">
              {compactionStats.map((entry) => (
                <li key={entry.level} className="stat-list__row">
                  <span>L{entry.level}</span>
                  <div className="stat-list__bar">
                    <div
                      className="stat-list__bar-fill"
                      style={{ width: `${Math.min(100, (entry.total_size / maxLevelBytes) * 100)}%` }}
                    />
                  </div>
                  <strong>{bytes(entry.total_size)}</strong>
                </li>
              ))}
            </ul>
          </div>

          <div>
            <h3 className="section-title">Memtable ownership</h3>
            <ul className="list">
              <li className="list__item">
                <span>Active WAL</span>
                <strong>{memtable?.active_log_number ?? "?"}</strong>
                <span>{memtable?.active_wal_path ?? "unavailable"}</span>
              </li>
              {memtable?.immutables.map((immutable) => (
                <li key={immutable.log_number} className="list__item">
                  <span>Immutable log #{immutable.log_number}</span>
                  <strong>{immutable.table.entries.length} rows</strong>
                  <span>{immutable.wal_path}</span>
                </li>
              )) ?? null}
            </ul>
          </div>
        </div>
      </div>
    </Panel>
  );
}
