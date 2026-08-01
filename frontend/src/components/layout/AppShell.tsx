import * as React from "react";
import { useDashboardStore } from "../../store/dashboard-store";
import { TopBar } from "./TopBar";

const WriteWorkbench   = React.lazy(() => import("../../features/write-workbench/WriteWorkbench").then(m => ({ default: m.WriteWorkbench })));
const LevelMatrix      = React.lazy(() => import("../../features/level-matrix/LevelMatrix").then(m => ({ default: m.LevelMatrix })));
const BloomTelemetry   = React.lazy(() => import("../../features/bloom-telemetry/BloomTelemetry").then(m => ({ default: m.BloomTelemetry })));
const ReadInspector    = React.lazy(() => import("../../features/read-inspector/ReadInspector").then(m => ({ default: m.ReadInspector })));
const CompactionStudio = React.lazy(() => import("../../features/compaction-studio/CompactionStudio").then(m => ({ default: m.CompactionStudio })));
const AmplificationDeck = React.lazy(() => import("../../features/amplification-deck/AmplificationDeck").then(m => ({ default: m.AmplificationDeck })));
const ScenarioLab      = React.lazy(() => import("../../features/scenario-lab/ScenarioLab").then(m => ({ default: m.ScenarioLab })));

export function AppShell() {
  const error = useDashboardStore((s) => s.error);
  return (
    <div style={{ minHeight: "100vh", background: "var(--color-bg)", position: "relative", zIndex: 1 }}>
      <TopBar />
      {error && (
        <div style={{
          background: "rgba(255,60,94,0.1)", border: "1px solid rgba(255,60,94,0.3)",
          color: "var(--color-crimson)", padding: "8px 16px", fontSize: "11px",
          fontFamily: "var(--font-mono)", letterSpacing: "0.05em",
        }}>
          ⚠ ENGINE ERROR: {error}
        </div>
      )}
      <main className="app-grid">
        <React.Suspense fallback={null}>
          <div className="col-4"><WriteWorkbench /></div>
          <div className="col-5"><LevelMatrix /></div>
          <div className="col-3"><BloomTelemetry /></div>
          <div className="col-4"><ReadInspector /></div>
          <div className="col-4"><CompactionStudio /></div>
          <div className="col-4"><AmplificationDeck /></div>
          <div className="col-12"><ScenarioLab /></div>
        </React.Suspense>
      </main>
    </div>
  );
}
