#!/usr/bin/env node
import { spawnSync } from "node:child_process";

if (!process.execArgv.includes("--preserve-symlinks")) {
  const result = spawnSync(process.execPath, ["--preserve-symlinks", new URL("../tests/sdk-smoke/sdk-smoke.mjs", import.meta.url).pathname], {
    stdio: "inherit",
    env: process.env,
  });

  process.exit(result.status ?? 1);
}

await import("../tests/sdk-smoke/sdk-smoke.mjs");
