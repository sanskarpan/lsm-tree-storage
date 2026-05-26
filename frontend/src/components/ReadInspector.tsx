import { useState } from "react";

import type { ReadTraceReport } from "../types";
import { Panel } from "./Panel";

type ReadInspectorProps = {
  pending: boolean;
  trace: ReadTraceReport | null;
  onInspect: (key: string) => Promise<void>;
};

export function ReadInspector({ pending, trace, onInspect }: ReadInspectorProps) {
  const [key, setKey] = useState("live-a");

  return (
    <Panel
      eyebrow="Read path"
      title="Query inspector"
      actions={
        <div className="inline-form">
          <input value={key} onChange={(event) => setKey(event.target.value)} placeholder="key" />
          <button disabled={pending} onClick={() => void onInspect(key)}>
            {pending ? "Tracing..." : "Trace read"}
          </button>
        </div>
      }
    >
      <div className="stack">
        <div className="read-summary">
          <div>
            <p className="panel__eyebrow">Result</p>
            <h3>{trace ? (trace.found ? "Found" : "Missing") : "Awaiting query"}</h3>
          </div>
          <div className={`hero__pill ${trace?.found ? "is-live" : ""}`}>
            status {trace?.status ?? 0}
          </div>
          <div className="read-summary__value">{trace?.value ?? "No value captured yet."}</div>
        </div>

        <div className="metric-row">
          <div className="metric-tile">
            <span>Bloom checks</span>
            <strong>{trace?.bloomChecks ?? 0}</strong>
          </div>
          <div className="metric-tile">
            <span>Bloom misses</span>
            <strong>{trace?.bloomMisses ?? 0}</strong>
          </div>
          <div className="metric-tile">
            <span>Memtable hits</span>
            <strong>{trace?.memtableHits ?? 0}</strong>
          </div>
          <div className="metric-tile">
            <span>SSTable hits</span>
            <strong>{trace?.sstableHits ?? 0}</strong>
          </div>
        </div>

        <div>
          <h3 className="section-title">Captured execution steps</h3>
          <ol className="trace-list">
            {trace?.steps.map((step, index) => (
              <li key={`${step}-${index}`}>{step}</li>
            )) ?? <li>No read trace captured yet.</li>}
          </ol>
        </div>
      </div>
    </Panel>
  );
}
