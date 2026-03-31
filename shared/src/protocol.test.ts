import { describe, expect, it } from "vitest";

import {
  APPROVAL_RESPONSE_MESSAGE_TYPE,
  CODEX_METHODS,
  classifyMethod,
  createApprovalResponseMessage,
} from "./protocol.js";

describe("classifyMethod", () => {
  it("classifies agent message deltas as agentMessage", () => {
    expect(classifyMethod(CODEX_METHODS.output.agentMessageDelta)).toBe(
      "agentMessage",
    );
  });

  it("classifies tool lifecycle item methods by tool item type", () => {
    expect(
      classifyMethod(CODEX_METHODS.output.itemStarted, "commandExecution"),
    ).toBe("toolCall");
    expect(
      classifyMethod(CODEX_METHODS.output.itemCompleted, "fileChange"),
    ).toBe("toolCall");
    expect(
      classifyMethod(CODEX_METHODS.output.itemCompleted, "dynamicToolCall"),
    ).toBe("toolCall");
  });

  it("does not classify non-tool item lifecycle methods as agentMessage", () => {
    expect(
      classifyMethod(CODEX_METHODS.output.itemStarted, "agentMessage"),
    ).toBe("unknown");
    expect(classifyMethod(CODEX_METHODS.output.itemStarted, "plan")).toBe(
      "unknown",
    );
    expect(
      classifyMethod(CODEX_METHODS.output.itemCompleted, "contextCompaction"),
    ).toBe("unknown");
    expect(classifyMethod(CODEX_METHODS.output.itemCompleted)).toBe("unknown");
  });

  it("keeps other known method families classified correctly", () => {
    expect(classifyMethod("serverRequest/approval")).toBe("serverRequest");
    expect(classifyMethod(CODEX_METHODS.output.turnStarted)).toBe(
      "turnLifecycle",
    );
    expect(classifyMethod(CODEX_METHODS.output.turnCompleted)).toBe(
      "turnLifecycle",
    );
    expect(classifyMethod(CODEX_METHODS.output.threadStarted)).toBe(
      "threadLifecycle",
    );
    expect(classifyMethod(undefined)).toBe("unknown");
  });

  it("creates approval response relay messages with explicit decisions", () => {
    expect(createApprovalResponseMessage("req-1", true)).toEqual({
      type: APPROVAL_RESPONSE_MESSAGE_TYPE,
      requestId: "req-1",
      decision: "accept",
    });

    expect(createApprovalResponseMessage(7, false)).toEqual({
      type: APPROVAL_RESPONSE_MESSAGE_TYPE,
      requestId: 7,
      decision: "decline",
    });
  });
});
