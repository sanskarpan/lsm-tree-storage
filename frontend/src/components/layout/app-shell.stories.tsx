import type { Meta, StoryObj } from "@storybook/react";

import { AppShell, TopBar } from "../layout";
import { Badge } from "../ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "../ui/card";
import type { EngineConfig, EngineStats, RuntimeState } from "../../types";

const config: EngineConfig = {
  DataDir: "/tmp/lsm",
  MemTableSize: 1_048_576,
  BlockSize: 4096,
  BloomBitsPerKey: 10,
  SSTMaxSize: 2_097_152,
  SyncWAL: true,
  MaxOpenFiles: 1000,
  BlockCacheSize: 8192,
  MaxLevels: 7,
  LevelSizeMultiplier: 10,
  Level0FileNumCompactionTrigger: 4,
  Level0StopWritesTrigger: 12,
  MaxImmutableMemTables: 2,
  CompactionStyle: "leveled",
  TimeWindowSize: 60,
};

const runtime: RuntimeState = {
  Open: true,
  DataDir: "/tmp/lsm",
  ActiveWALPath: "/tmp/lsm/000004.wal",
  ActiveLogNumber: 4,
  SyncWAL: true,
  CompactionStyle: "leveled",
};

const stats: EngineStats = {
  cache_hit_rate: 0.92,
  cache_hits: 1280,
  cache_misses: 110,
  cache_size: 4096,
  memtable_size: 1024,
  num_immutables: 0,
  seq_no: 4096,
  total_sst_bytes: 16384,
  total_sst_files: 5,
  wal_files: 1,
};

const meta: Meta<typeof AppShell> = {
  title: "Layout/AppShell",
  component: AppShell,
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: () => (
    <AppShell
      topBar={
        <TopBar
          config={config}
          connected={true}
          runtime={runtime}
          sessionCompactions={2}
          sessionFlushes={5}
          sessionWrites={128}
          stats={stats}
        />
      }
    >
      <AppShell.Panel gridColumn="span 6">
        <Card>
          <CardHeader>
            <CardTitle>First</CardTitle>
          </CardHeader>
          <CardContent>
            <Badge variant="info">info</Badge>
          </CardContent>
        </Card>
      </AppShell.Panel>
      <AppShell.Panel gridColumn="span 6">
        <Card>
          <CardHeader>
            <CardTitle>Second</CardTitle>
          </CardHeader>
          <CardContent>
            <Badge variant="success">ok</Badge>
          </CardContent>
        </Card>
      </AppShell.Panel>
    </AppShell>
  ),
};
