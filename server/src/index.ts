import path from "node:path";
import { fileURLToPath } from "node:url";

import dotenv from "dotenv";

import { readRelayServerConfig, startRelayServer } from "./relay-server.js";

loadEnvironmentFile();

const config = readRelayServerConfig();
const server = await startRelayServer(config);

let shutdownPromise: Promise<void> | null = null;

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, () => {
    if (!shutdownPromise) {
      shutdownPromise = shutdown(server, signal);
    }
  });
}

console.log(
  JSON.stringify({
    apiPort: server.apiPort,
    wsPort: server.wsPort,
  }),
);

function loadEnvironmentFile(): void {
  const packageRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  dotenv.config({
    path: path.resolve(packageRoot, "..", ".env"),
  });
}

async function shutdown(
  server: Awaited<ReturnType<typeof startRelayServer>>,
  signal: "SIGINT" | "SIGTERM",
): Promise<void> {
  try {
    console.error(`Received ${signal}, shutting down relay server.`);
    await server.close();
    process.exit(0);
  } catch (error) {
    console.error(`Failed to shut down relay server after ${signal}.`, error);
    process.exit(1);
  }
}
