export type CompactionStyle = "leveled" | "size-tiered" | "time-window";

export interface LevelFile {
  file_id: number;
  first_key: string;
  last_key: string;
  file_size: number;
  num_keys: number;
}

export interface LevelInfo {
  level: number;
  num_files: number;
  total_size: number;
  files: LevelFile[];
}

export interface EngineConfig {
  DataDir: string;
  MemTableSize: number;
  BlockSize: number;
  BloomBitsPerKey: number;
  SSTMaxSize: number;
  SyncWAL: boolean;
  MaxOpenFiles: number;
  BlockCacheSize: number;
  MaxLevels: number;
  LevelSizeMultiplier: number;
  Level0FileNumCompactionTrigger: number;
  Level0StopWritesTrigger: number;
  MaxImmutableMemTables: number;
  CompactionStyle: string;
  TimeWindowSize: number;
}

export interface RuntimeState {
  Open: boolean;
  DataDir: string;
  ActiveWALPath: string;
  ActiveLogNumber: number;
  SyncWAL: boolean;
  CompactionStyle: string;
}

export interface EngineStats {
  cache_hit_rate: number;
  cache_hits: number;
  cache_misses: number;
  cache_size: number;
  memtable_size: number;
  num_immutables: number;
  seq_no: number;
  total_sst_bytes: number;
  total_sst_files: number;
  wal_files: number;
}

export interface OpenResponse {
  status: string;
  state: RuntimeState;
  stats: EngineStats;
  config: EngineConfig;
}

export interface QueryResponse {
  key: string;
  value?: string;
  found: boolean;
}

export interface WalEntry {
  type: string;
  key?: string;
  value_len?: number;
  seq_no?: number;
  timestamp_unix_nano: number;
}

export interface WalEntriesResponse {
  count: number;
  entries: WalEntry[];
  limit: number;
  state: RuntimeState;
}

export interface MemtableEntry {
  key: string;
  seq_no: number;
  type: "put" | "delete";
  value?: string;
  value_len?: number;
}

export interface TableSnapshot {
  approximate_size: number;
  wal_seq_no: number;
  truncated: boolean;
  entries: MemtableEntry[];
}

export interface ImmutableSnapshot {
  log_number: number;
  wal_path: string;
  table: TableSnapshot;
}

export interface MemtableSnapshotResponse {
  mutable: TableSnapshot;
  immutables: ImmutableSnapshot[];
  active_wal_path: string;
  active_log_number: number;
  limit: number;
}

export interface BloomStat {
  file_id: number;
  bits_per_key: number;
  estimated_fp_rate: number;
  status: string;
}

export interface ScenarioInfo {
  name: string;
  description: string;
}

export interface CompactionLevelStat {
  level: number;
  num_files: number;
  total_size: number;
}

export interface BenchRequest {
  type: string;
  num_keys: number;
  value_size: number;
  read_write_ratio?: number;
}

export interface BenchResult {
  total_ops: number;
  duration_ms: number;
  ops_per_sec: number;
  p50_write_us: number;
  p99_write_us: number;
  p50_read_us: number;
  p99_read_us: number;
}

export interface EngineEvent {
  type: string;
  extra?: Record<string, unknown>;
}

export interface FeedLine {
  id: string;
  tone: "info" | "good" | "warn" | "danger" | "accent";
  label: string;
  detail?: string;
  timestamp: number;
}

export interface AmpPoint {
  timestamp: number;
  wa: number;
  ra: number;
  sa: number;
}

export interface ReadTraceReport {
  key: string;
  found: boolean;
  value?: string;
  status: number;
  steps: string[];
  bloomChecks: number;
  bloomMisses: number;
  memtableHits: number;
  sstableHits: number;
  generatedAt: number;
}

export interface ActiveCompaction {
  inputLevel: number;
  outputLevel: number;
  startedAt: number;
}
