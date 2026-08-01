#!/usr/bin/env bun
import { readdir, stat } from "node:fs/promises";
import { join } from "node:path";

const ASSETS_DIR = "dist/assets";
const BUDGETS: Record<string, number> = {
  "app.js": 350 * 1024,
  "data-table.js": 90 * 1024,
  "LevelMatrix.js": 20 * 1024,
  "ScenarioLab.js": 20 * 1024,
  "WriteWorkbench.js": 10 * 1024,
  "ReadInspector.js": 10 * 1024,
  "CompactionStudio.js": 10 * 1024,
  "AmplificationDeck.js": 10 * 1024,
  "BloomTelemetry.js": 10 * 1024,
  "app.css": 14 * 1024,
};

function formatBytes(bytes: number): string {
  return `${(bytes / 1024).toFixed(1)} KB`;
}

async function main() {
  let failed = false;
  const files = await readdir(ASSETS_DIR);
  const sizes = new Map<string, number>();
  for (const file of files) {
    const stats = await stat(join(ASSETS_DIR, file));
    sizes.set(file, stats.size);
  }

  console.log("Bundle budget check");
  console.log("===================");

  for (const [file, budget] of Object.entries(BUDGETS)) {
    const size = sizes.get(file);
    if (size == null) {
      console.log(`  - ${file.padEnd(24)} (not built)`);
      continue;
    }
    const over = size > budget;
    const marker = over ? "FAIL" : " ok ";
    if (over) failed = true;
    const pct = ((size / budget) * 100).toFixed(0).padStart(3);
    console.log(
      `  ${marker} ${file.padEnd(24)} ${formatBytes(size).padStart(8)} / ${formatBytes(
        budget,
      )} (${pct}%)`,
    );
  }

  console.log();
  if (failed) {
    console.error("Bundle budget exceeded.");
    process.exit(1);
  }
  console.log("All bundle budgets met.");
}

await main();
