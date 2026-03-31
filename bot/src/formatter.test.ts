import { describe, expect, it } from "vitest";

import {
  formatCodeBlock,
  formatFeishuMessageChunks,
  formatSessionTag,
} from "./formatter.js";

describe("formatter", () => {
  it("formats session tags and markdown code blocks", () => {
    expect(formatSessionTag("workspace-a")).toBe("[workspace-a]");
    expect(formatCodeBlock("echo hello", "bash")).toBe(
      "```bash\necho hello\n```",
    );
  });

  it("adds the session prefix to plain text messages", () => {
    expect(
      formatFeishuMessageChunks({
        sessionName: "workspace-a",
        content: "hello from codex",
      }),
    ).toEqual(["[workspace-a] hello from codex"]);
  });

  it("splits long plain text messages without exceeding the limit", () => {
    const chunks = formatFeishuMessageChunks({
      sessionName: "workspace-a",
      content: "abcdefghij".repeat(6),
      maxLength: 24,
    });

    expect(chunks.length).toBeGreaterThan(1);
    for (const chunk of chunks) {
      expect(chunk.length).toBeLessThanOrEqual(24);
      expect(chunk.startsWith("[workspace-a] ")).toBe(true);
    }

    expect(
      chunks.map((chunk) => chunk.replace("[workspace-a] ", "")).join(""),
    ).toBe("abcdefghij".repeat(6));
  });

  it("splits fenced code blocks into valid markdown chunks", () => {
    const chunks = formatFeishuMessageChunks({
      sessionName: "workspace-a",
      content: "0123456789".repeat(8),
      codeBlock: true,
      language: "json",
      maxLength: 32,
    });

    expect(chunks.length).toBeGreaterThan(1);
    for (const chunk of chunks) {
      expect(chunk.length).toBeLessThanOrEqual(32);
      expect(chunk.startsWith("[workspace-a]\n```json\n")).toBe(true);
      expect(chunk.endsWith("\n```")).toBe(true);
    }
  });

  it("returns no chunks for empty content", () => {
    expect(
      formatFeishuMessageChunks({
        sessionName: "workspace-a",
        content: "",
      }),
    ).toEqual([]);
  });
});
