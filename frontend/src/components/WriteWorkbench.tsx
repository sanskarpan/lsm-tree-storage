import { useState } from "react";

import type { FeedLine, MemtableSnapshotResponse, WalEntriesResponse } from "../types";
import { Panel } from "./Panel";

type WriteWorkbenchProps = {
  capacityBytes: number;
  memtable: MemtableSnapshotResponse | null;
  walEntries: WalEntriesResponse | null;
  writeFeed: FeedLine[];
  onPut: (key: string, value: string) => Promise<void>;
  onDelete: (key: string) => Promise<void>;
};

function toneClass(tone: FeedLine["tone"]): string {
  switch (tone) {
    case "good":
      return "feed__item is-good";
    case "warn":
      return "feed__item is-warn";
    case "danger":
      return "feed__item is-danger";
    case "accent":
      return "feed__item is-accent";
    default:
      return "feed__item";
  }
}

export function WriteWorkbench({
  capacityBytes,
  memtable,
  walEntries,
  writeFeed,
  onPut,
  onDelete,
}: WriteWorkbenchProps) {
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [pending, setPending] = useState(false);

  const mutable = memtable?.mutable;
  const approxBytes = mutable?.approximate_size ?? 0;
  const cap = Math.max(capacityBytes, 1);
  const fill = Math.min(100, (approxBytes / cap) * 100);

  async function submitPut() {
    if (!key) {
      return;
    }
    setPending(true);
    try {
      await onPut(key, value);
      setValue("");
    } finally {
      setPending(false);
    }
  }

  async function submitDelete() {
    if (!key) {
      return;
    }
    setPending(true);
    try {
      await onDelete(key);
      setValue("");
    } finally {
      setPending(false);
    }
  }

  return (
    <Panel
      eyebrow="Write path"
      title="Command deck"
      actions={
        <div className="inline-form">
          <input value={key} onChange={(event) => setKey(event.target.value)} placeholder="key" />
          <input value={value} onChange={(event) => setValue(event.target.value)} placeholder="value" />
          <button disabled={pending} onClick={() => void submitPut()}>
            PUT
          </button>
          <button className="button--ghost" disabled={pending} onClick={() => void submitDelete()}>
            DEL
          </button>
        </div>
      }
    >
      <div className="stack">
        <div className="meter-card">
          <div className="meter-card__header">
            <span>Mutable memtable pressure</span>
            <strong>
              {approxBytes.toLocaleString()} / {cap.toLocaleString()} B
            </strong>
          </div>
          <div className="meter">
            <div className="meter__fill" style={{ width: `${fill}%` }} />
          </div>
          <p className="meter-card__hint">
            Active log #{memtable?.active_log_number ?? "?"} with {memtable?.immutables.length ?? 0} immutable
            tables waiting behind it.
          </p>
        </div>

        <div className="two-column">
          <div>
            <h3 className="section-title">Recent WAL activity</h3>
            <ul className="list">
              {walEntries?.entries.map((entry) => (
                <li key={`${entry.type}-${entry.timestamp_unix_nano}`} className="list__item">
                  <span>{entry.type}</span>
                  <strong>{entry.key ?? "sync"}</strong>
                  <span>{entry.seq_no != null ? `seq ${entry.seq_no}` : "durability checkpoint"}</span>
                </li>
              )) ?? <li className="list__item">Waiting for WAL traffic.</li>}
            </ul>
          </div>

          <div>
            <h3 className="section-title">Session write feed</h3>
            <ul className="feed">
              {writeFeed.length > 0 ? (
                writeFeed.map((entry) => (
                  <li key={entry.id} className={toneClass(entry.tone)}>
                    <span>{entry.label}</span>
                    {entry.detail ? <small>{entry.detail}</small> : null}
                  </li>
                ))
              ) : (
                <li className="feed__item">No live writes yet.</li>
              )}
            </ul>
          </div>
        </div>

        <div>
          <h3 className="section-title">Mutable records</h3>
          <div className="token-grid">
            {mutable?.entries.map((entry) => (
              <article key={`${entry.key}-${entry.seq_no}`} className={`token ${entry.type === "delete" ? "is-delete" : ""}`}>
                <strong>{entry.key}</strong>
                <span>{entry.type}</span>
                <small>seq {entry.seq_no}</small>
              </article>
            )) ?? <p className="empty-copy">The mutable memtable is empty right now.</p>}
          </div>
        </div>
      </div>
    </Panel>
  );
}
