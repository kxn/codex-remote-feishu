import path from "node:path";
import { fileURLToPath } from "node:url";

import dotenv from "dotenv";
import { z } from "zod";

import { BotService } from "./bot-service.js";
import { FeishuGateway } from "./feishu.js";
import { RelayClient } from "./relay.js";

loadEnvironmentFile();

const botConfigSchema = z.object({
  FEISHU_APP_ID: z.string().min(1),
  FEISHU_APP_SECRET: z.string().min(1),
  RELAY_API_URL: z.string().url(),
});

const config = botConfigSchema.parse({
  FEISHU_APP_ID: process.env.FEISHU_APP_ID,
  FEISHU_APP_SECRET: process.env.FEISHU_APP_SECRET,
  RELAY_API_URL: process.env.RELAY_API_URL ?? "http://localhost:9501",
});

const relayClient = new RelayClient({
  baseUrl: config.RELAY_API_URL,
});
const gateway = new FeishuGateway({
  appId: config.FEISHU_APP_ID,
  appSecret: config.FEISHU_APP_SECRET,
});
const service = new BotService(relayClient, gateway);

let shuttingDown = false;
for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, () => {
    if (shuttingDown) {
      return;
    }

    shuttingDown = true;
    console.error(`Received ${signal}, shutting down bot.`);
    service.close();
    gateway.close();
    process.exit(0);
  });
}

await gateway.start({
  onTextMessage: async (message) => {
    await service.handleTextMessage(message);
  },
  onMenuAction: async (action) => {
    await service.handleMenuAction(action);
  },
});

function loadEnvironmentFile(): void {
  const packageRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  dotenv.config({
    path: path.resolve(packageRoot, "..", ".env"),
  });
}
