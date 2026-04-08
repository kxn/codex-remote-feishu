import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { SetupRoute } from "./SetupRoute";
import { makeApp, makeBootstrap, makeManifest, makeVSCodeDetect } from "../test/fixtures";
import { installMockFetch } from "../test/http";

describe("SetupRoute", () => {
  it("shows read-only connect state and disables credential inputs", async () => {
    window.history.replaceState({}, "", "/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              readOnly: true,
              readOnlyReason: "当前由运行时环境变量接管，只能做连接测试。",
              wizard: {},
            }),
          ],
        },
      },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(screen.getByText("正在读取最新状态")).toBeInTheDocument();
    expect(await screen.findByText("当前应用由运行时环境变量接管，setup 页面会直接对它做连接测试，但不会修改本地配置。")).toBeInTheDocument();
    expect(screen.getByLabelText("显示名称")).toBeDisabled();
    expect(screen.getByLabelText("App ID")).toBeDisabled();
    expect(screen.getByLabelText("App Secret")).toBeDisabled();
    expect(screen.getByRole("button", { name: "测试并继续" })).toBeEnabled();
  });

  it("lands on permissions after verify and blocks continue until confirmed", async () => {
    window.history.replaceState({}, "", "/setup");
    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              wizard: {
                connectionVerifiedAt: "2026-04-08T00:00:00Z",
              },
            }),
          ],
        },
      },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByText("权限导入说明")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "继续" }));

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("请先在飞书平台完成权限导入，并勾选页面上的确认项。");
    expect(calls.some((call) => call.method === "PATCH" && call.path.includes("/wizard"))).toBe(false);
  });

  it("renders the current manifest menu requirements in the menus step", async () => {
    window.history.replaceState({}, "", "/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              wizard: {
                connectionVerifiedAt: "2026-04-08T00:00:00Z",
                scopesExportedAt: "2026-04-08T00:01:00Z",
                eventsConfirmedAt: "2026-04-08T00:02:00Z",
                callbacksConfirmedAt: "2026-04-08T00:03:00Z",
              },
            }),
          ],
        },
      },
      "/api/setup/feishu/manifest": {
        body: {
          manifest: makeManifest({
            menus: [
              { key: "threads", name: "切换会话", description: "展示最近可见会话，并切换后续输入目标。" },
              { key: "reason_high", name: "推理 High", description: "只覆盖下一条消息的推理强度为 high。" },
            ],
          }),
        },
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByText("这些菜单 key 会真正生效")).toBeInTheDocument();
    expect(screen.getByText("threads")).toBeInTheDocument();
    expect(screen.getByText("切换会话")).toBeInTheDocument();
    expect(screen.getByText("展示最近可见会话，并切换后续输入目标。")).toBeInTheDocument();
    expect(screen.getByText("reason_high")).toBeInTheDocument();
    expect(screen.getByText("只覆盖下一条消息的推理强度为 high。")).toBeInTheDocument();
  });

  it("supports skipping current machine when user mainly uses remote ssh targets", async () => {
    window.history.replaceState({}, "", "/setup");
    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              wizard: {
                connectionVerifiedAt: "2026-04-08T00:00:00Z",
                scopesExportedAt: "2026-04-08T00:01:00Z",
                eventsConfirmedAt: "2026-04-08T00:02:00Z",
                callbacksConfirmedAt: "2026-04-08T00:03:00Z",
                menusConfirmedAt: "2026-04-08T00:04:00Z",
                publishedAt: "2026-04-08T00:05:00Z",
              },
            }),
          ],
        },
      },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": {
        body: makeVSCodeDetect({
          settings: {
            path: "/tmp/settings.json",
            exists: true,
            cliExecutable: "/usr/local/bin/codex",
            matchesBinary: false,
          },
          latestShim: {
            entrypoint: "/tmp/codex-shim.js",
            exists: true,
            realBinaryPath: "/usr/local/bin/codex",
            realBinaryExists: true,
            installed: true,
            matchesBinary: false,
          },
        }),
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByText("你以后主要怎么使用 VS Code 里的 Codex？")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("radio", { name: /主要去别的 SSH 机器上使用/ }));
    await user.click(screen.getByRole("button", { name: "我会去目标 SSH 机器上处理" }));

    expect(await screen.findByText("已跳过当前机器的 VS Code 接入。等你在目标 SSH 机器上安装 codex-remote 后，再在那里完成 VS Code 接入即可。")).toBeInTheDocument();
    expect(screen.getByText("当前机器未接入；你选择稍后在目标 SSH 机器上处理")).toBeInTheDocument();
    expect(calls.some((call) => call.path === "/api/setup/vscode/apply")).toBe(false);
  });

  it("applies editor settings for local-only vscode usage", async () => {
    window.history.replaceState({}, "", "/setup");
    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              wizard: {
                connectionVerifiedAt: "2026-04-08T00:00:00Z",
                scopesExportedAt: "2026-04-08T00:01:00Z",
                eventsConfirmedAt: "2026-04-08T00:02:00Z",
                callbacksConfirmedAt: "2026-04-08T00:03:00Z",
                menusConfirmedAt: "2026-04-08T00:04:00Z",
                publishedAt: "2026-04-08T00:05:00Z",
              },
            }),
          ],
        },
      },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": {
        body: makeVSCodeDetect({
          settings: {
            path: "/tmp/settings.json",
            exists: true,
            cliExecutable: "/usr/local/bin/codex",
            matchesBinary: false,
          },
          latestShim: {
            entrypoint: "/tmp/codex-shim.js",
            exists: true,
            realBinaryPath: "/usr/local/bin/codex",
            realBinaryExists: true,
            installed: true,
            matchesBinary: false,
          },
        }),
      },
      "/api/setup/vscode/apply": (call) => {
        expect(JSON.parse(String(call.init?.body))).toEqual({ mode: "editor_settings" });
        return {
          body: makeVSCodeDetect({
            currentMode: "editor_settings",
            settings: {
              path: "/tmp/settings.json",
              exists: true,
              cliExecutable: "/usr/local/bin/codex",
              matchesBinary: true,
            },
            latestShim: {
              entrypoint: "/tmp/codex-shim.js",
              exists: true,
              realBinaryPath: "/usr/local/bin/codex",
              realBinaryExists: true,
              installed: true,
              matchesBinary: false,
            },
          }),
        };
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByText("你以后主要怎么使用 VS Code 里的 Codex？")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("radio", { name: /只在这台机器本地使用/ }));
    await user.click(screen.getByRole("button", { name: "在这台机器上启用 VS Code" }));

    expect(await screen.findByText("已写入这台机器的 VS Code settings.json，现在可以在本机 VS Code 里使用 Codex。")).toBeInTheDocument();
    expect(screen.getByText("已在这台机器上接入（settings.json）")).toBeInTheDocument();
    expect(calls.some((call) => call.path === "/api/setup/vscode/apply")).toBe(true);
  });

  it("shows the remote-machine bundle warning and disables continue in ssh sessions", async () => {
    window.history.replaceState({}, "", "/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap({ sshSession: true }) },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              wizard: {
                connectionVerifiedAt: "2026-04-08T00:00:00Z",
                scopesExportedAt: "2026-04-08T00:01:00Z",
                eventsConfirmedAt: "2026-04-08T00:02:00Z",
                callbacksConfirmedAt: "2026-04-08T00:03:00Z",
                menusConfirmedAt: "2026-04-08T00:04:00Z",
                publishedAt: "2026-04-08T00:05:00Z",
              },
            }),
          ],
        },
      },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": {
        body: makeVSCodeDetect({
          sshSession: true,
          latestBundleEntrypoint: "",
          recordedBundleEntrypoint: "",
          candidateBundleEntrypoints: [],
          settings: {
            path: "/tmp/settings.json",
            exists: true,
            cliExecutable: "/usr/local/bin/codex",
            matchesBinary: false,
          },
          latestShim: {
            entrypoint: "",
            exists: false,
            realBinaryPath: "",
            realBinaryExists: false,
            installed: false,
            matchesBinary: false,
          },
        }),
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByText("检测到当前是远程 SSH 机器")).toBeInTheDocument();
    expect(screen.getByText("还没检测到这台机器上的 VS Code 扩展。请先在这台机器上打开一次 VS Code Remote 窗口，并确保 Codex 扩展已经安装，然后再回来继续。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "在这台远程机器上启用 VS Code" })).toBeDisabled();
  });
});
