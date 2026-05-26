import { ensureClientBuild } from "./server/ensure-client";

await ensureClientBuild();
await import("./server/bff");
