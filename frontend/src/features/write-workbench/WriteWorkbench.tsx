import * as React from "react";
import { useDashboardStore } from "../../store/dashboard-store";

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function timeStr(iso: string) {
  return new Date(iso).toLocaleTimeString("en-US", {
    hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit",
  });
}

function feedTagClass(label: string): string {
  const l = label.toLowerCase();
  if (l === "put") return "feed-tag put";
  if (l === "del" || l === "delete") return "feed-tag del";
  if (l === "sync" || l === "checkpoint") return "feed-tag sync";
  if (l === "flush") return "feed-tag flush";
  if (l === "compact") return "feed-tag compact";
  return "feed-tag sync";
}

export function WriteWorkbench() {
  const capacityBytes = useDashboardStore((s) => s.config?.MemTableSize ?? 1);
  const memtable      = useDashboardStore((s) => s.memtable);
  const walEntries    = useDashboardStore((s) => s.walEntries);
  const writeFeed     = useDashboardStore((s) => s.writeFeed);
  const onPut         = useDashboardStore((s) => s.handlePut);
  const onDelete      = useDashboardStore((s) => s.handleDelete);

  const [key, setKey]     = React.useState("");
  const [value, setValue] = React.useState("");
  const [pending, setPending] = React.useState(false);

  const mutable     = memtable?.mutable;
  const approxBytes = mutable?.approximate_size ?? 0;
  const cap         = Math.max(capacityBytes, 1);
  const fillPct     = Math.min(1, approxBytes / cap);
  const fillClass   = fillPct > 0.8 ? "danger" : fillPct > 0.6 ? "warning" : "ok";

  async function submitPut() {
    if (!key) return;
    setPending(true);
    try { await onPut(key, value); setValue(""); }
    finally { setPending(false); }
  }

  async function submitDelete() {
    if (!key) return;
    setPending(true);
    try { await onDelete(key); setValue(""); }
    finally { setPending(false); }
  }

  const recentWal = (walEntries?.entries ?? []).slice(0, 12);

  return (
    <div className="panel" style={{ display: "flex", flexDirection: "column" }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Command Deck</div>
          <div className="panel-subtitle">Write path</div>
        </div>
      </div>
      <div className="panel-body" style={{ display: "flex", flexDirection: "column", gap: 12, flex: 1 }}>

        {/* PUT / DEL form */}
        <form
          onSubmit={(e) => { e.preventDefault(); void submitPut(); }}
          style={{ display: "flex", gap: 6, alignItems: "flex-end" }}
        >
          <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 3 }}>
            <label className="kv-label" htmlFor="wb-key">Key</label>
            <input
              id="wb-key"
              className="term-input"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="key"
            />
          </div>
          <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 3 }}>
            <label className="kv-label" htmlFor="wb-value">Value</label>
            <input
              id="wb-value"
              className="term-input"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="value"
            />
          </div>
          <button type="submit" className="term-btn primary" disabled={pending || !key}>
            PUT
          </button>
          <button
            type="button"
            className="term-btn danger-ghost"
            disabled={pending || !key}
            onClick={() => void submitDelete()}
          >
            DEL
          </button>
        </form>

        <hr className="term-divider" />

        {/* Memtable pressure */}
        <div>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 4 }}>
            <span className="kv-label">Mutable Memtable</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--color-text-muted)" }}>
              {formatBytes(approxBytes)} / {formatBytes(cap)}
            </span>
          </div>
          <div className="level-bar-track">
            <div className={`level-bar-fill ${fillClass}`} style={{ width: `${(fillPct * 100).toFixed(1)}%` }} />
          </div>
          <div style={{ marginTop: 4, fontSize: 9, color: "var(--color-text-muted)", fontFamily: "var(--font-display)", letterSpacing: "0.08em" }}>
            LOG #{memtable?.active_log_number ?? "?"} · {memtable?.immutables.length ?? 0} IMMUTABLE
            {(memtable?.immutables.length ?? 0) === 1 ? "" : "S"} QUEUED
          </div>
        </div>

        {/* Mutable records */}
        {(mutable?.entries.length ?? 0) > 0 && (
          <div>
            <div className="kv-label" style={{ marginBottom: 5 }}>Mutable Records</div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 3 }}>
              {mutable!.entries.map((entry) => (
                <span
                  key={`${entry.key}-${entry.seq_no}`}
                  style={{
                    fontSize: 9, fontFamily: "var(--font-mono)",
                    background: entry.type === "delete" ? "rgba(255,60,94,0.1)" : "rgba(157,255,60,0.08)",
                    border: `1px solid ${entry.type === "delete" ? "rgba(255,60,94,0.25)" : "rgba(157,255,60,0.2)"}`,
                    color: entry.type === "delete" ? "var(--color-crimson)" : "var(--color-signal)",
                    borderRadius: 1, padding: "1px 5px",
                  }}
                >
                  {entry.key}
                  <span style={{ color: "var(--color-text-dim)", marginLeft: 3 }}>
                    seq {entry.seq_no}
                  </span>
                </span>
              ))}
            </div>
          </div>
        )}

        <hr className="term-divider" />

        {/* WAL table */}
        <div>
          <div className="kv-label" style={{ marginBottom: 6 }}>Recent WAL Activity</div>
          {recentWal.length === 0 ? (
            <p style={{ fontSize: 11, color: "var(--color-text-dim)" }}>Waiting for WAL traffic.</p>
          ) : (
            <table className="term-table">
              <thead>
                <tr>
                  <th>Type</th>
                  <th>Key</th>
                  <th>SeqNo</th>
                  <th>Time</th>
                </tr>
              </thead>
              <tbody>
                {recentWal.map((entry, i) => (
                  <tr key={i}>
                    <td>
                      <span className={`feed-tag ${entry.type === "delete" ? "del" : entry.type === "sync" ? "sync" : "put"}`}>
                        {entry.type}
                      </span>
                    </td>
                    <td style={{ color: "var(--color-text)" }}>{entry.key || "—"}</td>
                    <td style={{ color: "var(--color-text-muted)" }}>
                      {entry.seq_no != null ? `seq ${entry.seq_no}` : "checkpoint"}
                    </td>
                    <td style={{ color: "var(--color-text-dim)" }}>
                      {new Date(entry.timestamp_unix_nano / 1_000_000).toISOString().slice(11, 19)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        <hr className="term-divider" />

        {/* Session write feed */}
        <div>
          <div className="kv-label" style={{ marginBottom: 5 }}>Session Write Feed</div>
          {writeFeed.length === 0 ? (
            <p style={{ fontSize: 11, color: "var(--color-text-dim)" }}>No live writes yet.</p>
          ) : (
            <div style={{ maxHeight: 120, overflowY: "auto" }}>
              {writeFeed.map((entry) => (
                <div key={entry.id} className="feed-entry animate-slide-in">
                  <span className="feed-time">
                    {timeStr(new Date(entry.timestamp).toISOString())}
                  </span>
                  <span className={feedTagClass(entry.label)}>{entry.label}</span>
                  {entry.detail && <span className="feed-detail">{entry.detail}</span>}
                </div>
              ))}
            </div>
          )}
        </div>

      </div>
    </div>
  );
}
