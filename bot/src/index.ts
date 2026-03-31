import { z } from "zod";

import { BotService } from "./bot-service.js";
import { FeishuGateway } from "./feishu.js";
import { RelayClient } from "./relay.js";

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

await gateway.start({
  onTextMessage: async (message) => {
    await service.handleTextMessage(message);
  },
});
