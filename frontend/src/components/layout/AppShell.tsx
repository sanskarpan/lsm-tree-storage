import { useDashboardStore } from "../../store/dashboard-store";
import { TopBar } from "./TopBar";
import { WriteWorkbench } from "../../features/write-workbench/WriteWorkbench";
import { LevelMatrix } from "../../features/level-matrix/LevelMatrix";
import { BloomTelemetry } from "../../features/bloom-telemetry/BloomTelemetry";
import { ReadInspector } from "../../features/read-inspector/ReadInspector";
import { CompactionStudio } from "../../features/compaction-studio/CompactionStudio";
import { AmplificationDeck } from "../../features/amplification-deck/AmplificationDeck";
import { ScenarioLab } from "../../features/scenario-lab/ScenarioLab";

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
        <div className="col-4"><WriteWorkbench /></div>
        <div className="col-5"><LevelMatrix /></div>
        <div className="col-3"><BloomTelemetry /></div>
        <div className="col-4"><ReadInspector /></div>
        <div className="col-4"><CompactionStudio /></div>
        <div className="col-4"><AmplificationDeck /></div>
        <div className="col-12"><ScenarioLab /></div>
      </main>
    </div>
  );
}
