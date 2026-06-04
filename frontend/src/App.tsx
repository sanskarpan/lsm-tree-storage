import * as React from "react";
import { lazy, Suspense } from "react";

import { AppShell, TopBar } from "./components/layout";
import { Banner } from "./components/ui/banner";
import { PanelSkeleton } from "./components/ui/panel-skeleton";
import { useDashboardData } from "./hooks/useDashboardData";

const WriteWorkbench = lazy(() =>
  import("./features/write-workbench/WriteWorkbench").then((m) => ({
    default: m.WriteWorkbench,
  })),
);
const LevelMatrix = lazy(() =>
  import("./features/level-matrix/LevelMatrix").then((m) => ({
    default: m.LevelMatrix,
  })),
);
const BloomTelemetry = lazy(() =>
  import("./features/bloom-telemetry/BloomTelemetry").then((m) => ({
    default: m.BloomTelemetry,
  })),
);
const ReadInspector = lazy(() =>
  import("./features/read-inspector/ReadInspector").then((m) => ({
    default: m.ReadInspector,
  })),
);
const CompactionStudio = lazy(() =>
  import("./features/compaction-studio/CompactionStudio").then((m) => ({
    default: m.CompactionStudio,
  })),
);
const AmplificationDeck = lazy(() =>
  import("./features/amplification-deck/AmplificationDeck").then((m) => ({
    default: m.AmplificationDeck,
  })),
);
const ScenarioLab = lazy(() =>
  import("./features/scenario-lab/ScenarioLab").then((m) => ({
    default: m.ScenarioLab,
  })),
);

function PanelBoundary({
  children,
  skeletonLines = 3,
}: {
  children: React.ReactNode;
  skeletonLines?: number;
}) {
  return <Suspense fallback={<PanelSkeleton lines={skeletonLines} />}>{children}</Suspense>;
}

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
          <Banner tone="danger">{dashboard.error}</Banner>
        ) : null
      }
    >
      <AppShell.Panel gridColumn="span 4">
        <PanelBoundary>
          <WriteWorkbench
            capacityBytes={dashboard.config?.MemTableSize ?? 1}
            memtable={dashboard.memtable}
            onDelete={dashboard.handleDelete}
            onPut={dashboard.handlePut}
            walEntries={dashboard.walEntries}
            writeFeed={dashboard.writeFeed}
          />
        </PanelBoundary>
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 5">
        <PanelBoundary>
          <LevelMatrix
            compactionStats={dashboard.compactionStats}
            levels={dashboard.levels}
            memtable={dashboard.memtable}
            onRefresh={dashboard.refreshSnapshot}
          />
        </PanelBoundary>
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 3">
        <PanelBoundary skeletonLines={2}>
          <BloomTelemetry bloomStats={dashboard.bloomStats} />
        </PanelBoundary>
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 4">
        <PanelBoundary skeletonLines={4}>
          <ReadInspector
            onInspect={dashboard.runReadTrace}
            pending={dashboard.queryPending}
            trace={dashboard.readTrace}
          />
        </PanelBoundary>
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 4">
        <PanelBoundary>
          <CompactionStudio
            activeCompaction={dashboard.activeCompaction}
            compactionFeed={dashboard.compactionFeed}
            compactionStats={dashboard.compactionStats}
            currentStyle={dashboard.runtime?.CompactionStyle ?? dashboard.config?.CompactionStyle ?? "leveled"}
            onForceCompaction={dashboard.handleForceCompaction}
            onStyleChange={dashboard.handleStyleChange}
          />
        </PanelBoundary>
      </AppShell.Panel>

      <AppShell.Panel gridColumn="span 4">
        <PanelBoundary skeletonLines={2}>
          <AmplificationDeck
            history={dashboard.ampHistory}
            ra={dashboard.amplification.ra}
            sa={dashboard.amplification.sa}
            wa={dashboard.amplification.wa}
          />
        </PanelBoundary>
      </AppShell.Panel>

      <AppShell.Panel gridColumn="full">
        <PanelBoundary skeletonLines={5}>
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
        </PanelBoundary>
      </AppShell.Panel>
    </AppShell>
  );
}
