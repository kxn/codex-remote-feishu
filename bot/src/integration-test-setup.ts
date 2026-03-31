import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const MODULE_DIR = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(MODULE_DIR, "../..");

export interface CommandExecutionOptions {
  cwd: string;
  label: string;
}

export type CommandRunner = (
  command: string,
  args: string[],
  options: CommandExecutionOptions,
) => Promise<void>;

interface IntegrationBuildStep extends CommandExecutionOptions {
  command: string;
  args: string[];
}

export interface EnsureIntegrationPrerequisitesOptions {
  repoRoot?: string;
  runCommand?: CommandRunner;
}

export async function ensureIntegrationTestPrerequisites(
  options: EnsureIntegrationPrerequisitesOptions = {},
): Promise<void> {
  const repoRoot = options.repoRoot ?? REPO_ROOT;
  const runCommand = options.runCommand ?? executeCommand;

  for (const step of getIntegrationBuildSteps(repoRoot)) {
    await runCommand(step.command, step.args, {
      cwd: step.cwd,
      label: step.label,
    });
  }
}

function getIntegrationBuildSteps(repoRoot: string): IntegrationBuildStep[] {
  return [
    {
      command: resolveCommand("npm"),
      args: ["run", "build"],
      cwd: path.join(repoRoot, "shared"),
      label: "shared package",
    },
    {
      command: resolveCommand("npm"),
      args: ["run", "build"],
      cwd: path.join(repoRoot, "server"),
      label: "server package",
    },
    {
      command: resolveCommand("cargo"),
      args: ["build"],
      cwd: path.join(repoRoot, "wrapper"),
      label: "wrapper binary",
    },
  ];
}

function resolveCommand(command: "cargo" | "npm"): string {
  if (process.platform === "win32" && command === "npm") {
    return "npm.cmd";
  }

  return command;
}

async function executeCommand(
  command: string,
  args: string[],
  options: CommandExecutionOptions,
): Promise<void> {
  console.log(`[vitest] Building ${options.label}...`);

  await new Promise<void>((resolve, reject) => {
    let settled = false;

    const child = spawn(command, args, {
      cwd: options.cwd,
      env: process.env,
      stdio: "inherit",
    });

    child.once("error", (error) => {
      if (settled) {
        return;
      }

      settled = true;
      reject(
        new Error(`Failed to start ${options.label} build: ${error.message}`),
      );
    });

    child.once("exit", (code, signal) => {
      if (settled) {
        return;
      }

      settled = true;

      if (code === 0) {
        resolve();
        return;
      }

      reject(
        new Error(
          `Failed to build ${options.label} with "${formatCommand(
            command,
            args,
          )}" (${signal ? `signal ${signal}` : `exit code ${String(code)}`}).`,
        ),
      );
    });
  });
}

function formatCommand(command: string, args: string[]): string {
  return [command, ...args].join(" ");
}
