import { AmplificationDeck } from "./components/AmplificationDeck";
import { CompactionStudio } from "./components/CompactionStudio";
import { ReadInspector } from "./components/ReadInspector";
import { ScenarioLab } from "./components/ScenarioLab";
import { BloomTelemetry } from "./features/bloom-telemetry/BloomTelemetry";
import { LevelMatrix } from "./features/level-matrix/LevelMatrix";
import { WriteWorkbench } from "./features/write-workbench/WriteWorkbench";
import { AppShell, TopBar } from "./components/layout";
import { useDashboardData } from "./hooks/useDashboardData";

export function App() {
  const dashboard = useDashboardData();

  return (
    <AppShell
      topBar={
        <TopBar
          config={dashboard.config}
          connected={dashboard.connected}
          runtime={dashboard.runtime}
          sessionCompactions={dashboard.sessionCompactions}
          sessionFlushes={dashboard.sessionFlushes}
          sessionWrites={dashboard.sessionWrites}
          stats={dashboard.stats}
        />
      }
      errorBanner={
        dashboard.error ? (
          <div className="error-banner">{dashboard.error}</div>
        ) : null
      }
    >
      <AppShell.Panel gridColumn="span 4">
        <WriteWorkbench
          capacityBytes={dashboard.config?.MemTableSize ?? 1}
          memtable={dashboard.memtable}
          onDelete={dashboard.handleDelete}
          onPut={dashboard.handlePut}
          walEntries={dashboard.walEntries}
          writeFeed={dashboard.writeFeed}
        />
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 5">
        <LevelMatrix
          compactionStats={dashboard.compactionStats}
          levels={dashboard.levels}
          memtable={dashboard.memtable}
          onRefresh={dashboard.refreshSnapshot}
        />
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 3">
        <BloomTelemetry bloomStats={dashboard.bloomStats} />
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 4">
        <ReadInspector
          onInspect={dashboard.runReadTrace}
          pending={dashboard.queryPending}
          trace={dashboard.readTrace}
        />
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 4">
        <CompactionStudio
          activeCompaction={dashboard.activeCompaction}
          compactionFeed={dashboard.compactionFeed}
          compactionStats={dashboard.compactionStats}
          currentStyle={dashboard.runtime?.CompactionStyle ?? dashboard.config?.CompactionStyle ?? "leveled"}
          onForceCompaction={dashboard.handleForceCompaction}
          onStyleChange={dashboard.handleStyleChange}
        />
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 4">
        <AmplificationDeck
          history={dashboard.ampHistory}
          ra={dashboard.amplification.ra}
          sa={dashboard.amplification.sa}
          wa={dashboard.amplification.wa}
        />
      </AppShell.Panel>

      <AppShell.Panel gridColumn="full">
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
      </AppShell.Panel>
    </AppShell>
  );
}
