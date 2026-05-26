import type {
  BenchRequest,
  BenchResult,
  BloomStat,
  CompactionLevelStat,
  CompactionStyle,
  LevelInfo,
  MemtableSnapshotResponse,
  OpenResponse,
  QueryResponse,
  ScenarioInfo,
  WalEntriesResponse,
} from "../types";

const API_BASE = window.location.origin;

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
    ...init,
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(`${response.status} ${response.statusText}: ${text}`);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

export async function getOpenState(): Promise<OpenResponse> {
  return request<OpenResponse>("/db/open");
}

export async function getLevels(): Promise<LevelInfo[]> {
  return request<LevelInfo[]>("/levels");
}

export async function getWalEntries(limit = 30): Promise<WalEntriesResponse> {
  return request<WalEntriesResponse>(`/wal/entries?limit=${limit}`);
}

export async function getMemtableSnapshot(limit = 24): Promise<MemtableSnapshotResponse> {
  return request<MemtableSnapshotResponse>(`/memtable/snapshot?limit=${limit}`);
}

export async function getCompactionStats(): Promise<CompactionLevelStat[]> {
  return request<CompactionLevelStat[]>("/compaction/stats");
}

export async function listScenarios(): Promise<ScenarioInfo[]> {
  return request<ScenarioInfo[]>("/scenarios");
}

export async function getBloomStat(fileId: number): Promise<BloomStat> {
  return request<BloomStat>(`/bloom/${fileId}`);
}

export async function putValue(key: string, value: string): Promise<void> {
  await request<void>("/db/put", {
    method: "POST",
    body: JSON.stringify({ key, value }),
  });
}

export async function deleteValue(key: string): Promise<void> {
  await request<void>(`/db/delete?key=${encodeURIComponent(key)}`, {
    method: "DELETE",
  });
}

export async function getValue(key: string): Promise<{ status: number; body: QueryResponse }> {
  const response = await fetch(`${API_BASE}/db/get?key=${encodeURIComponent(key)}`);
  const body = (await response.json()) as QueryResponse;
  if (!response.ok && response.status !== 404) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return { status: response.status, body };
}

export async function setCompactionStyle(style: CompactionStyle): Promise<void> {
  await request<void>("/compaction/style", {
    method: "POST",
    body: JSON.stringify({ style }),
  });
}

export async function forceCompaction(level = 0): Promise<void> {
  await request<void>("/compaction/force", {
    method: "POST",
    body: JSON.stringify({ level }),
  });
}

export async function runScenario(name: string): Promise<{ status: string; scenario: string }> {
  return request<{ status: string; scenario: string }>(`/scenarios/${name}/run`, {
    method: "POST",
  });
}

export async function runBench(requestBody: BenchRequest): Promise<BenchResult> {
  return request<BenchResult>("/bench/run", {
    method: "POST",
    body: JSON.stringify(requestBody),
  });
}

export async function attemptRemoteClose(): Promise<{ status: string; reason: string; supported: boolean }> {
  const response = await fetch(`${API_BASE}/db/close`, { method: "POST" });
  const body = (await response.json()) as { status: string; reason: string; supported: boolean };
  if (!response.ok && response.status !== 409) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return body;
}
