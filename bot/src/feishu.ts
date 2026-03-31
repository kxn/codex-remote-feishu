import * as Lark from "@larksuiteoapi/node-sdk";
import { z } from "zod";

import type { BotMessenger, IncomingTextMessage } from "./bot-service.js";

const incomingMessageEventSchema = z.object({
  sender: z.object({
    sender_id: z.object({
      user_id: z.string().min(1).optional(),
      open_id: z.string().min(1).optional(),
    }),
  }),
  message: z.object({
    message_id: z.string().min(1),
    chat_id: z.string().min(1),
    message_type: z.string().min(1),
    content: z.string(),
  }),
});

const textMessageContentSchema = z.object({
  text: z.string(),
});

export interface FeishuGatewayConfig {
  appId: string;
  appSecret: string;
}

export interface FeishuGatewayHandlers {
  onTextMessage: (message: IncomingTextMessage) => Promise<void> | void;
}

interface FeishuClientLike {
  im: {
    v1: {
      message: {
        create: (payload: {
          params: {
            receive_id_type: "chat_id";
          };
          data: {
            receive_id: string;
            msg_type: "text";
            content: string;
          };
        }) => Promise<unknown>;
      };
    };
  };
}

interface FeishuWsClientLike {
  start: (params: { eventDispatcher: unknown }) => Promise<void>;
  close: () => void;
}

interface FeishuEventDispatcherLike {
  register: (handles: Record<string, Function>) => unknown;
}

export interface FeishuSdkLike {
  Client: new (params: {
    appId: string;
    appSecret: string;
  }) => FeishuClientLike;
  WSClient: new (params: {
    appId: string;
    appSecret: string;
  }) => FeishuWsClientLike;
  EventDispatcher: new (params: Record<string, never>) => FeishuEventDispatcherLike;
}

export class FeishuGateway implements BotMessenger {
  private readonly client: FeishuClientLike;

  private readonly wsClient: FeishuWsClientLike;

  constructor(
    private readonly config: FeishuGatewayConfig,
    private readonly sdk: FeishuSdkLike = Lark as unknown as FeishuSdkLike,
  ) {
    this.client = new this.sdk.Client({
      appId: this.config.appId,
      appSecret: this.config.appSecret,
    });

    this.wsClient = new this.sdk.WSClient({
      appId: this.config.appId,
      appSecret: this.config.appSecret,
    });
  }

  async start(handlers: FeishuGatewayHandlers): Promise<void> {
    const dispatcher = new this.sdk.EventDispatcher({}).register({
      "im.message.receive_v1": async (event: unknown) => {
        const message = parseIncomingTextMessage(event);
        if (!message) {
          return;
        }

        await handlers.onTextMessage(message);
      },
    });

    await this.wsClient.start({
      eventDispatcher: dispatcher,
    });
  }

  async sendText(chatId: string, text: string): Promise<void> {
    await this.client.im.v1.message.create({
      params: {
        receive_id_type: "chat_id",
      },
      data: {
        receive_id: chatId,
        msg_type: "text",
        content: JSON.stringify({
          text,
        }),
      },
    });
  }

  close(): void {
    this.wsClient.close();
  }
}

export function parseIncomingTextMessage(
  event: unknown,
): IncomingTextMessage | undefined {
  const parsedEvent = incomingMessageEventSchema.safeParse(event);
  if (!parsedEvent.success) {
    return undefined;
  }

  if (parsedEvent.data.message.message_type !== "text") {
    return undefined;
  }

  const content = safeParseJson(parsedEvent.data.message.content);
  const parsedContent = textMessageContentSchema.safeParse(content);
  if (!parsedContent.success) {
    return undefined;
  }

  const userId =
    parsedEvent.data.sender.sender_id.user_id ??
    parsedEvent.data.sender.sender_id.open_id;
  if (!userId) {
    return undefined;
  }

  return {
    userId,
    chatId: parsedEvent.data.message.chat_id,
    messageId: parsedEvent.data.message.message_id,
    text: parsedContent.data.text,
  };
}

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return undefined;
  }
}
