import { build } from "vite";
import { join } from "path";

export async function ensureClientBuild(): Promise<void> {
  if (Bun.env.SKIP_CLIENT_BUILD === "1") {
    return;
  }

  console.log("Building React dashboard...");
  await build({
    configFile: join(import.meta.dir, "../vite.config.ts"),
    logLevel: "error",
  });
}
