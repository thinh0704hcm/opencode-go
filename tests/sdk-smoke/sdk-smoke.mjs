#!/usr/bin/env node
import { existsSync } from "node:fs";
import net from "node:net";

const sdkDir = process.env.OPENCODE_SDK_DIR || "/tmp/sdk-extract/package";
const repo = new URL("../..", import.meta.url).pathname.replace(/\/$/, "");
const smokeDir = new URL(".", import.meta.url).pathname.replace(/\/$/, "");
const strict = process.env.OPENCODE_SMOKE_REQUIRED === "1";

if (!existsSync(`${sdkDir}/package.json`)) {
  skip(`SDK extract not found at ${sdkDir}`);
}

if (!existsSync(`${smokeDir}/node_modules/@opencode-ai/sdk/package.json`)) {
  skip(`SDK smoke deps not found at ${smokeDir}/node_modules`);
}

process.env.PATH = `${repo}/bin:${process.env.PATH || ""}`;
process.env.OPENCODE_GO_MOCK = "1";

async function runV1() {
  const { createOpencodeServer, createOpencodeClient } = await importSdk("@opencode-ai/sdk");
  const port = Number(process.env.OPENCODE_SMOKE_V1_PORT || await freePort());
  const server = await createOpencodeServer({ hostname: "127.0.0.1", port, timeout: 5000 });

  try {
    const client = createOpencodeClient({ baseUrl: server.url, directory: repo });
    const sessions = await client.session.list();
    const config = await client.config.get();
    const project = await client.project.current();

    if (!Array.isArray(sessions?.data)) throw new Error(`v1 session.list failed: ${JSON.stringify(sessions)}`);
    if (!config?.data || typeof config.data !== "object") throw new Error(`v1 config.get failed: ${JSON.stringify(config)}`);
    if (!project?.data || typeof project.data !== "object") throw new Error(`v1 project.current failed: ${JSON.stringify(project)}`);
    if (typeof client.instance?.dispose === "function") await client.instance.dispose();

    console.log(`OK: SDK v1 smoke ${server.url}`);
  } finally {
    server.close();
  }
}

async function runV2() {
  const { createOpencodeServer, createOpencodeClient } = await importSdk("@opencode-ai/sdk/v2");
  const port = Number(process.env.OPENCODE_SMOKE_V2_PORT || await freePort());
  const server = await createOpencodeServer({ hostname: "127.0.0.1", port, timeout: 5000 });

  try {
    const client = createOpencodeClient({ baseUrl: server.url, directory: repo });
    const health = await client.global.health();
    const config = await client.global.config.get();
    const sessions = await client.experimental.session.list();
    const workspaces = await client.experimental.workspace.list();
    const adapters = await client.experimental.workspace.adapter.list();

    if (!health?.data?.healthy) throw new Error(`v2 global.health failed: ${JSON.stringify(health)}`);
    if (!config?.data || typeof config.data !== "object") throw new Error(`v2 global.config.get failed: ${JSON.stringify(config)}`);
    if (!isList(sessions?.data)) throw new Error(`v2 experimental.session.list failed: ${JSON.stringify(sessions)}`);
    if (!isList(workspaces?.data)) throw new Error(`v2 experimental.workspace.list failed: ${JSON.stringify(workspaces)}`);
    if (!isList(adapters?.data)) throw new Error(`v2 experimental.workspace.adapter.list failed: ${JSON.stringify(adapters)}`);
    if (typeof client.global.dispose === "function") await client.global.dispose();

    console.log(`OK: SDK v2 smoke ${server.url}`);
  } finally {
    server.close();
  }
}

function isList(data) {
  return Array.isArray(data) || Array.isArray(data?.items);
}

function skip(message) {
  console.log(`SKIP: ${message}`);
  process.exit(strict ? 1 : 0);
}

async function importSdk(specifier) {
  try {
    return await import(specifier);
  } catch (error) {
    if (error?.code === "ERR_MODULE_NOT_FOUND") {
      skip(`SDK dependency import failed for ${specifier}: ${error.message}`);
    }
    throw error;
  }
}

async function freePort() {
  return await new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
  });
}

await runV1();
await runV2();
