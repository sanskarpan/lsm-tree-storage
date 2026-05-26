import { AmplificationDeck } from "./components/AmplificationDeck";
import { BloomTelemetry } from "./components/BloomTelemetry";
import { CompactionStudio } from "./components/CompactionStudio";
import { HeaderBar } from "./components/HeaderBar";
import { LevelMatrix } from "./components/LevelMatrix";
import { ReadInspector } from "./components/ReadInspector";
import { ScenarioLab } from "./components/ScenarioLab";
import { WriteWorkbench } from "./components/WriteWorkbench";
import { useDashboardData } from "./hooks/useDashboardData";

export function App() {
  const dashboard = useDashboardData();

  return (
    <div className="app-shell">
      <div className="app-shell__backdrop" />
      <main className="app-shell__content">
        <HeaderBar
          config={dashboard.config}
          connected={dashboard.connected}
          runtime={dashboard.runtime}
          sessionCompactions={dashboard.sessionCompactions}
          sessionFlushes={dashboard.sessionFlushes}
          sessionWrites={dashboard.sessionWrites}
          stats={dashboard.stats}
        />

        {dashboard.error ? <div className="error-banner">{dashboard.error}</div> : null}

        <div className="dashboard-grid">
          <WriteWorkbench
            capacityBytes={dashboard.config?.MemTableSize ?? 1}
            memtable={dashboard.memtable}
            onDelete={dashboard.handleDelete}
            onPut={dashboard.handlePut}
            walEntries={dashboard.walEntries}
            writeFeed={dashboard.writeFeed}
          />

          <LevelMatrix
            compactionStats={dashboard.compactionStats}
            levels={dashboard.levels}
            memtable={dashboard.memtable}
            onRefresh={dashboard.refreshSnapshot}
          />

          <BloomTelemetry bloomStats={dashboard.bloomStats} />

          <ReadInspector onInspect={dashboard.runReadTrace} pending={dashboard.queryPending} trace={dashboard.readTrace} />

          <CompactionStudio
            activeCompaction={dashboard.activeCompaction}
            compactionFeed={dashboard.compactionFeed}
            compactionStats={dashboard.compactionStats}
            currentStyle={dashboard.runtime?.CompactionStyle ?? dashboard.config?.CompactionStyle ?? "leveled"}
            onForceCompaction={dashboard.handleForceCompaction}
            onStyleChange={dashboard.handleStyleChange}
          />

          <AmplificationDeck
            history={dashboard.ampHistory}
            ra={dashboard.amplification.ra}
            sa={dashboard.amplification.sa}
            wa={dashboard.amplification.wa}
          />

          <ScenarioLab
            benchmarkResult={dashboard.benchmarkResult}
            closeMessage={dashboard.closeMessage}
            config={dashboard.config}
            onBenchRun={dashboard.handleBenchRun}
            onCloseAttempt={dashboard.handleCloseAttempt}
            onScenarioRun={dashboard.handleScenarioRun}
            opsFeed={dashboard.opsFeed}
            runtime={dashboard.runtime}
            scenarios={dashboard.scenarios}
          />
        </div>
      </main>
    </div>
  );
}
