import path from "node:path";

import { describe, expect, it, vi } from "vitest";

import { ensureIntegrationTestPrerequisites } from "./integration-test-setup.js";

describe("ensureIntegrationTestPrerequisites", () => {
  it("builds the shared package, server package, and wrapper binary in order", async () => {
    const calls: Array<{
      command: string;
      args: string[];
      cwd: string;
      label: string;
    }> = [];

    await ensureIntegrationTestPrerequisites({
      repoRoot: "/repo",
      runCommand: async (command, args, options) => {
        calls.push({
          command,
          args,
          cwd: options.cwd,
          label: options.label,
        });
      },
    });

    expect(calls).toEqual([
      {
        command: process.platform === "win32" ? "npm.cmd" : "npm",
        args: ["run", "build"],
        cwd: path.join("/repo", "shared"),
        label: "shared package",
      },
      {
        command: process.platform === "win32" ? "npm.cmd" : "npm",
        args: ["run", "build"],
        cwd: path.join("/repo", "server"),
        label: "server package",
      },
      {
        command: "cargo",
        args: ["build"],
        cwd: path.join("/repo", "wrapper"),
        label: "wrapper binary",
      },
    ]);
  });

  it("stops at the first failing prerequisite build", async () => {
    const runCommand = vi.fn(
      async (_command: string, _args: string[], options: { label: string }) => {
        if (options.label === "server package") {
          throw new Error("server build failed");
        }
      },
    );

    await expect(
      ensureIntegrationTestPrerequisites({
        repoRoot: "/repo",
        runCommand,
      }),
    ).rejects.toThrowError("server build failed");

    expect(runCommand).toHaveBeenCalledTimes(2);
  });
});
