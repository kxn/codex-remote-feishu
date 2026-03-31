import { describe, expect, it } from "vitest";

import { parseIncomingText } from "./commands.js";

describe("parseIncomingText", () => {
  it("parses the supported slash commands", () => {
    expect(parseIncomingText("/list")).toEqual({ kind: "list" });
    expect(parseIncomingText("/detach")).toEqual({ kind: "detach" });
    expect(parseIncomingText("/stop")).toEqual({ kind: "stop" });
    expect(parseIncomingText("/status")).toEqual({ kind: "status" });
  });

  it("parses attach and history arguments", () => {
    expect(parseIncomingText("/attach session-1")).toEqual({
      kind: "attach",
      sessionQuery: "session-1",
    });

    expect(parseIncomingText(" /history 7 ")).toEqual({
      kind: "history",
      limit: 7,
    });

    expect(parseIncomingText("/history")).toEqual({
      kind: "history",
      limit: undefined,
    });
  });

  it("treats non-slash text as a prompt and preserves newlines", () => {
    expect(parseIncomingText("hello\nremote world")).toEqual({
      kind: "prompt",
      content: "hello\nremote world",
    });
  });

  it("rejects malformed commands and reports unknown slash commands", () => {
    expect(parseIncomingText("   ")).toEqual({
      kind: "invalid",
      error: "Message cannot be empty.",
    });

    expect(parseIncomingText("/attach")).toEqual({
      kind: "invalid",
      error: "`/attach` requires a session name or ID.",
    });

    expect(parseIncomingText("/history nope")).toEqual({
      kind: "invalid",
      error: "`/history` expects an optional non-negative integer limit.",
    });

    expect(parseIncomingText("/foobar")).toEqual({
      kind: "unknown-command",
      command: "foobar",
    });
  });
});
