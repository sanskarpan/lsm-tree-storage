import { useState } from "react";

import type { BenchRequest, BenchResult, EngineConfig, FeedLine, RuntimeState, ScenarioInfo } from "../types";
import { Panel } from "./Panel";

type ScenarioLabProps = {
  runtime: RuntimeState | null;
  config: EngineConfig | null;
  scenarios: ScenarioInfo[];
  benchmarkResult: BenchResult | null;
  closeMessage: string | null;
  opsFeed: FeedLine[];
  onScenarioRun: (name: string) => Promise<void>;
  onBenchRun: (request: BenchRequest) => Promise<void>;
  onCloseAttempt: () => Promise<void>;
};

export function ScenarioLab({
  runtime,
  config,
  scenarios,
  benchmarkResult,
  closeMessage,
  opsFeed,
  onScenarioRun,
  onBenchRun,
  onCloseAttempt,
}: ScenarioLabProps) {
  const [scenarioName, setScenarioName] = useState<string>("");
  const [benchType, setBenchType] = useState("sequential_write");
  const [benchKeys, setBenchKeys] = useState(2000);
  const [benchValueSize, setBenchValueSize] = useState(128);

  return (
    <Panel
      eyebrow="Scenarios"
      title="Operations lab"
      actions={
        <div className="inline-form">
          <select value={scenarioName} onChange={(event) => setScenarioName(event.target.value)}>
            <option value="">Select scenario</option>
            {scenarios.map((scenario) => (
              <option key={scenario.name} value={scenario.name}>
                {scenario.name}
              </option>
            ))}
          </select>
          <button disabled={!scenarioName} onClick={() => void onScenarioRun(scenarioName)}>
            Run scenario
          </button>
        </div>
      }
    >
      <div className="stack">
        <div className="two-column">
          <div className="runtime-card">
            <p className="panel__eyebrow">Runtime</p>
            <h3>{runtime?.DataDir ?? "unavailable"}</h3>
            <p>Active WAL #{runtime?.ActiveLogNumber ?? "?"}</p>
            <p>Sync WAL: {runtime?.SyncWAL ? "on" : "off"}</p>
            <p>Memtable target: {config?.MemTableSize?.toLocaleString() ?? "?"} bytes</p>
          </div>

          <div className="runtime-card">
            <p className="panel__eyebrow">Benchmark</p>
            <div className="inline-form inline-form--stack">
              <input value={benchType} onChange={(event) => setBenchType(event.target.value)} placeholder="workload type" />
              <input
                type="number"
                value={benchKeys}
                onChange={(event) => setBenchKeys(Number(event.target.value))}
                placeholder="keys"
              />
              <input
                type="number"
                value={benchValueSize}
                onChange={(event) => setBenchValueSize(Number(event.target.value))}
                placeholder="value bytes"
              />
              <button
                onClick={() =>
                  void onBenchRun({
                    type: benchType,
                    num_keys: benchKeys,
                    value_size: benchValueSize,
                  })
                }
              >
                Run bench
              </button>
            </div>
            {benchmarkResult ? (
              <div className="bench-result">
                <strong>{Math.round(benchmarkResult.ops_per_sec)} ops/sec</strong>
                <span>p99 write {benchmarkResult.p99_write_us} us</span>
                <span>p99 read {benchmarkResult.p99_read_us} us</span>
              </div>
            ) : null}
          </div>
        </div>

        <div className="runtime-card">
          <div className="panel__header panel__header--inline">
            <div>
              <p className="panel__eyebrow">Lifecycle guardrail</p>
              <h3 className="panel__title panel__title--small">Remote close behavior</h3>
            </div>
            <button className="button--ghost" onClick={() => void onCloseAttempt()}>
              Attempt close
            </button>
          </div>
          <p>{closeMessage ?? "The server now rejects remote lifecycle shutdown explicitly instead of pretending to close."}</p>
        </div>

        <div>
          <h3 className="section-title">Operations feed</h3>
          <ul className="feed">
            {opsFeed.length > 0 ? (
              opsFeed.map((entry) => (
                <li key={entry.id} className="feed__item">
                  <span>{entry.label}</span>
                  {entry.detail ? <small>{entry.detail}</small> : null}
                </li>
              ))
            ) : (
              <li className="feed__item">Scenario and benchmark results will appear here.</li>
            )}
          </ul>
        </div>
      </div>
    </Panel>
  );
}
