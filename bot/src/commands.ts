import { z } from "zod";

const historyLimitSchema = z.coerce.number().int().min(0);

export type ParsedIncomingText =
  | { kind: "list" }
  | { kind: "attach"; sessionQuery: string }
  | { kind: "detach" }
  | { kind: "stop" }
  | { kind: "status" }
  | { kind: "history"; limit: number | undefined }
  | { kind: "prompt"; content: string }
  | { kind: "unknown-command"; command: string }
  | { kind: "invalid"; error: string };

export function parseIncomingText(text: string): ParsedIncomingText {
  if (text.trim().length === 0) {
    return {
      kind: "invalid",
      error: "Message cannot be empty.",
    };
  }

  const trimmed = text.trim();
  if (!trimmed.startsWith("/")) {
    return {
      kind: "prompt",
      content: text,
    };
  }

  const [rawCommand] = trimmed.split(/\s+/, 1);
  const command = rawCommand.slice(1).toLowerCase();
  const argumentText = trimmed.slice(rawCommand.length).trim();

  switch (command) {
    case "list":
      return { kind: "list" };
    case "attach":
      if (argumentText.length === 0) {
        return {
          kind: "invalid",
          error: "`/attach` requires a session name or ID.",
        };
      }
      return {
        kind: "attach",
        sessionQuery: argumentText,
      };
    case "detach":
      return { kind: "detach" };
    case "stop":
      return { kind: "stop" };
    case "status":
      return { kind: "status" };
    case "history":
      if (argumentText.length === 0) {
        return {
          kind: "history",
          limit: undefined,
        };
      }

      if (!historyLimitSchema.safeParse(argumentText).success) {
        return {
          kind: "invalid",
          error: "`/history` expects an optional non-negative integer limit.",
        };
      }

      return {
        kind: "history",
        limit: historyLimitSchema.parse(argumentText),
      };
    default:
      return {
        kind: "unknown-command",
        command,
      };
  }
}
