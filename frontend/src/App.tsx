import * as React from "react";
import { lazy, Suspense, useCallback, useMemo } from "react";

import { AppShell, TopBar } from "./components/layout";
import { Banner } from "./components/ui/banner";
import { PanelSkeleton } from "./components/ui/panel-skeleton";
import { useDashboardData } from "./hooks/useDashboardData";
import { useDashboardStore } from "./store/dashboard-store";

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

const WriteWorkbenchPanel = React.memo(function WriteWorkbenchPanel() {
  const capacityBytes = useDashboardStore((s) => s.config?.MemTableSize ?? 1);
  const memtable = useDashboardStore((s) => s.memtable);
  const walEntries = useDashboardStore((s) => s.walEntries);
  const writeFeed = useDashboardStore((s) => s.writeFeed);
  const onPut = useDashboardStore((s) => s.handlePut);
  const onDelete = useDashboardStore((s) => s.handleDelete);
  return (
    <WriteWorkbench
      capacityBytes={capacityBytes}
      memtable={memtable}
      walEntries={walEntries}
      writeFeed={writeFeed}
      onPut={onPut}
      onDelete={onDelete}
    />
  );
});

const LevelMatrixPanel = React.memo(function LevelMatrixPanel() {
  const levels = useDashboardStore((s) => s.levels);
  const memtable = useDashboardStore((s) => s.memtable);
  const compactionStats = useDashboardStore((s) => s.compactionStats);
  const refresh = useDashboardStore((s) => s.refreshSnapshot);
  return (
    <LevelMatrix
      levels={levels}
      memtable={memtable}
      compactionStats={compactionStats}
      onRefresh={refresh}
    />
  );
});

const BloomTelemetryPanel = React.memo(function BloomTelemetryPanel() {
  const bloomStats = useDashboardStore((s) => s.bloomStats);
  return <BloomTelemetry bloomStats={bloomStats} />;
});

const ReadInspectorPanel = React.memo(function ReadInspectorPanel() {
  const trace = useDashboardStore((s) => s.readTrace);
  const pending = useDashboardStore((s) => s.queryPending);
  const onInspect = useDashboardStore((s) => s.runReadTrace);
  return <ReadInspector trace={trace} pending={pending} onInspect={onInspect} />;
});

const CompactionStudioPanel = React.memo(function CompactionStudioPanel() {
  const activeCompaction = useDashboardStore((s) => s.activeCompaction);
  const compactionFeed = useDashboardStore((s) => s.compactionFeed);
  const compactionStats = useDashboardStore((s) => s.compactionStats);
  const runtime = useDashboardStore((s) => s.runtime);
  const config = useDashboardStore((s) => s.config);
  const onForceCompaction = useDashboardStore((s) => s.handleForceCompaction);
  const onStyleChange = useDashboardStore((s) => s.handleStyleChange);
  const currentStyle = runtime?.CompactionStyle ?? config?.CompactionStyle ?? "leveled";
  return (
    <CompactionStudio
      activeCompaction={activeCompaction}
      compactionFeed={compactionFeed}
      compactionStats={compactionStats}
      currentStyle={currentStyle}
      onForceCompaction={onForceCompaction}
      onStyleChange={onStyleChange}
    />
  );
});

const AmplificationDeckPanel = React.memo(function AmplificationDeckPanel() {
  const wa = useDashboardStore((s) => s.amplification.wa);
  const ra = useDashboardStore((s) => s.amplification.ra);
  const sa = useDashboardStore((s) => s.amplification.sa);
  const history = useDashboardStore((s) => s.ampHistory);
  return <AmplificationDeck wa={wa} ra={ra} sa={sa} history={history} />;
});

const ScenarioLabPanel = React.memo(function ScenarioLabPanel() {
  const runtime = useDashboardStore((s) => s.runtime);
  const config = useDashboardStore((s) => s.config);
  const scenarios = useDashboardStore((s) => s.scenarios);
  const benchmarkResult = useDashboardStore((s) => s.benchmarkResult);
  const closeMessage = useDashboardStore((s) => s.closeMessage);
  const opsFeed = useDashboardStore((s) => s.opsFeed);
  const onScenarioRun = useDashboardStore((s) => s.handleScenarioRun);
  const onBenchRun = useDashboardStore((s) => s.handleBenchRun);
  const onCloseAttempt = useDashboardStore((s) => s.handleCloseAttempt);
  return (
    <ScenarioLab
      runtime={runtime}
      config={config}
      scenarios={scenarios}
      benchmarkResult={benchmarkResult}
      closeMessage={closeMessage}
      opsFeed={opsFeed}
      onScenarioRun={onScenarioRun}
      onBenchRun={onBenchRun}
      onCloseAttempt={onCloseAttempt}
    />
  );
});

const TopBarBlock = React.memo(function TopBarBlock() {
  const config = useDashboardStore((s) => s.config);
  const connected = useDashboardStore((s) => s.connected);
  const runtime = useDashboardStore((s) => s.runtime);
  const stats = useDashboardStore((s) => s.stats);
  const sessionWrites = useDashboardStore((s) => s.sessionWrites);
  const sessionFlushes = useDashboardStore((s) => s.sessionFlushes);
  const sessionCompactions = useDashboardStore((s) => s.sessionCompactions);
  return (
    <TopBar
      config={config}
      connected={connected}
      runtime={runtime}
      sessionWrites={sessionWrites}
      sessionFlushes={sessionFlushes}
      sessionCompactions={sessionCompactions}
      stats={stats}
    />
  );
});

const ErrorBannerBlock = React.memo(function ErrorBannerBlock() {
  const error = useDashboardStore((s) => s.error);
  const setError = useDashboardStore((s) => s.setError);
  const [dismissed, dismiss] = React.useState<string | null>(null);
  React.useEffect(() => {
    if (error && error !== dismissed) {
      dismiss(null);
    }
  }, [error, dismissed]);
  if (!error || error === dismissed) return null;
  return (
    <Banner tone="danger" dismissible onDismiss={() => dismiss(error)}>
      {error}
    </Banner>
  );
});

export function App() {
  useDashboardData();

  const panelLayout = useMemo(
    () => (
      <>
        <AppShell.Panel gridColumn="span 4">
          <PanelBoundary>
            <WriteWorkbenchPanel />
          </PanelBoundary>
        </AppShell.Panel>

        <AppShell.Panel gridColumn="span 5">
          <PanelBoundary>
            <LevelMatrixPanel />
          </PanelBoundary>
        </AppShell.Panel>

        <AppShell.Panel gridColumn="span 3">
          <PanelBoundary skeletonLines={2}>
            <BloomTelemetryPanel />
          </PanelBoundary>
        </AppShell.Panel>

        <AppShell.Panel gridColumn="span 4">
          <PanelBoundary skeletonLines={4}>
            <ReadInspectorPanel />
          </PanelBoundary>
        </AppShell.Panel>

        <AppShell.Panel gridColumn="span 4">
          <PanelBoundary>
            <CompactionStudioPanel />
          </PanelBoundary>
        </AppShell.Panel>

        <AppShell.Panel gridColumn="span 4">
          <PanelBoundary skeletonLines={2}>
            <AmplificationDeckPanel />
          </PanelBoundary>
        </AppShell.Panel>

        <AppShell.Panel gridColumn="full">
          <PanelBoundary skeletonLines={5}>
            <ScenarioLabPanel />
          </PanelBoundary>
        </AppShell.Panel>
      </>
    ),
    [],
  );

  return (
    <AppShell
      topBar={<TopBarBlock />}
      errorBanner={<ErrorBannerBlock />}
    >
      {panelLayout}
    </AppShell>
  );
}
