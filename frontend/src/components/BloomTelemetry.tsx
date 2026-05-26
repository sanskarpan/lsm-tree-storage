import { useMemo, useState } from "react";

import type { BloomStat } from "../types";
import { Panel } from "./Panel";

type BloomTelemetryProps = {
  bloomStats: BloomStat[];
};

function estimateFp(bitsPerKey: number): number {
  return Math.pow(0.6185, bitsPerKey);
}

function hashPositions(key: string, count: number, width: number): number[] {
  const positions: number[] = [];
  let seed = 2166136261;

  for (let i = 0; i < key.length; i += 1) {
    seed ^= key.charCodeAt(i);
    seed = Math.imul(seed, 16777619);
  }

  for (let i = 0; i < count; i += 1) {
    seed = Math.imul(seed ^ (i + 11), 2246822519);
    positions.push(Math.abs(seed) % width);
  }

  return positions.sort((a, b) => a - b);
}

export function BloomTelemetry({ bloomStats }: BloomTelemetryProps) {
  const [bitsPerKey, setBitsPerKey] = useState(10);
  const [query, setQuery] = useState("alpha");
  const positions = useMemo(() => hashPositions(query || "alpha", 7, 64), [query]);

  return (
    <Panel eyebrow="Bloom" title="False-positive radar">
      <div className="stack">
        <div className="meter-card">
          <div className="meter-card__header">
            <span>Estimator</span>
            <strong>{(estimateFp(bitsPerKey) * 100).toFixed(2)}%</strong>
          </div>
          <input
            className="range"
            max={20}
            min={6}
            onChange={(event) => setBitsPerKey(Number(event.target.value))}
            type="range"
            value={bitsPerKey}
          />
          <p className="meter-card__hint">bits/key {bitsPerKey}</p>
        </div>

        <div className="hash-lab">
          <div className="hash-lab__header">
            <h3 className="section-title">Hash positions</h3>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="key probe" />
          </div>
          <div className="hash-grid">
            {Array.from({ length: 64 }, (_, index) => (
              <div key={index} className={`hash-grid__cell ${positions.includes(index) ? "is-hot" : ""}`}>
                {index}
              </div>
            ))}
          </div>
        </div>

        <div>
          <h3 className="section-title">Loaded SSTable filters</h3>
          <ul className="list">
            {bloomStats.length > 0 ? (
              bloomStats.map((stat) => (
                <li key={stat.file_id} className="list__item">
                  <span>File #{stat.file_id}</span>
                  <strong>{(stat.estimated_fp_rate * 100).toFixed(2)}% FP</strong>
                  <span>{stat.bits_per_key} bits/key</span>
                </li>
              ))
            ) : (
              <li className="list__item">No SSTable bloom filters loaded yet.</li>
            )}
          </ul>
        </div>
      </div>
    </Panel>
  );
}
