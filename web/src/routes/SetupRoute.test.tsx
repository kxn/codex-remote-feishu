import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { SetupRoute } from "./SetupRoute";
import { makeApp, makeAutostartDetect, makeBootstrap, makeManifest, makeRuntimeRequirementsDetect, makeVSCodeDetect } from "../test/fixtures";
import { installMockFetch } from "../test/http";

describe("SetupRoute", () => {
  it("keeps local API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    const { calls } = installMockFetch({
      "/g/demo/api/setup/bootstrap-state": { body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }) },
      "/g/demo/api/setup/feishu/apps": { body: { apps: [makeApp({ wizard: {} })] } },
      "/g/demo/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/g/demo/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "环境检查" })).toBeInTheDocument();
    expect(calls.length).toBeGreaterThan(0);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
    expect(calls.some((call) => call.path === "/g/demo/api/setup/bootstrap-state")).toBe(true);
  });

  it("toggles the shared setup step navigation and closes it after selecting a step", async () => {
    window.history.replaceState({}, "", "/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": { body: { apps: [makeApp({ wizard: {} })] } },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "环境检查" })).toBeInTheDocument();

    const user = userEvent.setup();
    const toggle = screen.getByRole("button", { name: "打开步骤导航" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");

    await user.click(screen.getByRole("button", { name: /接入飞书应用/ }));
    expect(toggle).toHaveAttribute("aria-expanded", "false");
  });

  it("starts from environment check and then shows manual fields for existing apps", async () => {
    window.history.replaceState({}, "", "/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": { body: { apps: [makeApp({ wizard: {} })] } },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "环境检查" })).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "继续" }));
    await user.click(await screen.findByRole("radio", { name: /接入已有飞书应用/ }));
    await user.click(screen.getByRole("button", { name: "下一步" }));

    expect(await screen.findByText("已有飞书应用怎么接")).toBeInTheDocument();
    expect(screen.getByLabelText("App ID")).toBeInTheDocument();
    expect(screen.getByLabelText("App Secret")).toBeInTheDocument();
    expect(screen.queryByLabelText("显示名称")).not.toBeInTheDocument();
  });

  it("creates a new app through qr onboarding and then advances to capability check", async () => {
    window.history.replaceState({}, "", "/setup");
    let appsConfigured = false;
    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": () => ({
        body: {
          apps: appsConfigured
            ? [
                makeApp({
                  id: "bot-qr",
                  name: "扫码 Bot",
                  appId: "cli_qr",
                  wizard: {
                    connectionVerifiedAt: "2026-04-09T00:00:00Z",
                    scopesExportedAt: "2026-04-09T00:00:01Z",
                    eventsConfirmedAt: "2026-04-09T00:00:02Z",
                    callbacksConfirmedAt: "2026-04-09T00:00:03Z",
                    menusConfirmedAt: "2026-04-09T00:00:04Z",
                    publishedAt: "2026-04-09T00:00:05Z",
                  },
                }),
              ]
            : [],
        },
      }),
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
      "/api/setup/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "sess-1",
            status: "ready",
            qrCodeDataUrl: "data:image/png;base64,abc",
            appId: "cli_qr",
            displayName: "扫码 Bot",
          },
        },
      },
      "/api/setup/feishu/onboarding/sessions/sess-1/complete": () => {
        appsConfigured = true;
        return {
          body: {
            app: makeApp({
              id: "bot-qr",
              name: "扫码 Bot",
              appId: "cli_qr",
              wizard: {
                connectionVerifiedAt: "2026-04-09T00:00:00Z",
                scopesExportedAt: "2026-04-09T00:00:01Z",
                eventsConfirmedAt: "2026-04-09T00:00:02Z",
                callbacksConfirmedAt: "2026-04-09T00:00:03Z",
                menusConfirmedAt: "2026-04-09T00:00:04Z",
                publishedAt: "2026-04-09T00:00:05Z",
              },
            }),
            mutation: {
              kind: "created",
              message: "飞书机器人已创建。接下来请先测试连接，并完成首次配置。",
            },
            result: {
              connected: true,
              duration: 1_000_000_000,
            },
            session: {
              id: "sess-1",
              status: "completed",
              appId: "cli_qr",
              displayName: "扫码 Bot",
            },
            guide: {
              autoConfiguredSummary: "扫码创建已经完成，大部分基础配置已自动处理。",
              remainingManualActions: ["如果需要把 Markdown 预览上传到飞书云盘，还需要额外申请 `drive:drive` 权限。"],
              recommendedNextStep: "capability",
            },
          },
        };
      },
    });

    render(<SetupRoute />);

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(await screen.findByRole("button", { name: "下一步" }));

    expect(await screen.findByText("扫码创建已经完成，大部分基础配置已自动处理。")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "下一步" }));

    expect(await screen.findByText("基础对话与交互")).toBeInTheDocument();
    expect(screen.getByText("现在已经可以开始使用。基础对话与交互已经准备好，增强项可以稍后再补。")).toBeInTheDocument();
    expect(calls.some((call) => call.path === "/api/setup/feishu/onboarding/sessions")).toBe(true);
    expect(calls.some((call) => call.path === "/api/setup/feishu/onboarding/sessions/sess-1/complete")).toBe(true);
  });

  it("shows read-only connect state after environment check", async () => {
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

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));

    expect(await screen.findByText("当前应用由运行时环境变量接管，setup 页面会直接对它做连接测试，但不会修改本地配置。")).toBeInTheDocument();
    expect(screen.getByLabelText("显示名称")).toBeDisabled();
    expect(screen.getByLabelText("App ID")).toBeDisabled();
    expect(screen.getByLabelText("App Secret")).toBeDisabled();
    expect(screen.getByRole("button", { name: "测试并继续" })).toBeEnabled();
  });

  it("blocks capability continue until permission import is confirmed", async () => {
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

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(screen.getByRole("button", { name: /能力检查/ }));

    expect(await screen.findByText("先把基础权限导入好")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "记录并继续" }));

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("请先在飞书平台完成权限导入，并勾选页面上的确认项。");
    expect(calls.some((call) => call.method === "PATCH" && call.path.includes("/wizard"))).toBe(false);
  });

  it("renders the current manifest menu requirements in the capability step", async () => {
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

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(screen.getByRole("button", { name: /能力检查/ }));

    expect(await screen.findByText("继续把飞书应用菜单配好")).toBeInTheDocument();
    expect(screen.getByText("threads")).toBeInTheDocument();
    expect(screen.getByText("切换会话")).toBeInTheDocument();
    expect(screen.getByText("展示最近可见会话，并切换后续输入目标。")).toBeInTheDocument();
    expect(screen.getByText("reason_high")).toBeInTheDocument();
    expect(screen.getByText("只覆盖下一条消息的推理强度为 high。")).toBeInTheDocument();
  });

  it("rechecks environment before continuing and blocks when runtime requirements fail", async () => {
    window.history.replaceState({}, "", "/setup");
    let ready = false;

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": { body: { apps: [makeApp({ wizard: {} })] } },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/runtime-requirements/detect": () => ({
        body: ready
          ? makeRuntimeRequirementsDetect()
          : makeRuntimeRequirementsDetect({
              ready: false,
              summary: "当前机器还不满足基础运行条件，请先修复失败项后再继续。",
              resolvedCodexRealBinary: "",
              checks: [
                {
                  id: "real_codex_binary",
                  title: "真实 Codex 二进制",
                  status: "fail",
                  summary: "当前服务环境下无法解析到可执行的 codex。",
                },
              ],
            }),
      }),
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByText("当前机器还不满足基础运行条件，请先修复失败项后再继续。")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "重新检查" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("当前机器还不满足正常使用要求。请先修复失败项，再重新检查。");

    ready = true;
    await user.click(screen.getByRole("button", { name: "我知道了" }));
    await user.click(screen.getByRole("button", { name: "重新检查" }));

    expect(await screen.findByText("这次要处理哪个飞书应用？")).toBeInTheDocument();
  });

  it("shows the autostart step after capability and applies systemd user autostart", async () => {
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
      "/api/setup/autostart/detect": {
        body: makeAutostartDetect({
          platform: "linux",
          supported: true,
          manager: "systemd_user",
          currentManager: "detached",
          status: "disabled",
          configured: false,
          enabled: false,
          canApply: true,
          installStatePath: "/tmp/install-state.json",
          serviceUnitPath: "/tmp/.config/systemd/user/codex-remote.service",
        }),
      },
      "/api/setup/autostart/apply": {
        body: makeAutostartDetect({
          platform: "linux",
          supported: true,
          manager: "systemd_user",
          currentManager: "systemd_user",
          status: "enabled",
          configured: true,
          enabled: true,
          canApply: true,
          installStatePath: "/tmp/install-state.json",
          serviceUnitPath: "/tmp/.config/systemd/user/codex-remote.service",
        }),
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(screen.getByRole("button", { name: /自动启动/ }));

    expect(await screen.findByText("当前平台支持自动启动")).toBeInTheDocument();
    expect(screen.getByText("当前未启用")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "启用自动启动" }));

    expect(await screen.findByText("已为当前用户启用登录后自动启动。")).toBeInTheDocument();
    expect(calls.some((call) => call.path === "/api/setup/autostart/apply")).toBe(true);
  });

  it("shows unsupported autostart copy on non-linux platforms and lets the user continue", async () => {
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
                menusConfirmedAt: "2026-04-08T00:04:00Z",
                publishedAt: "2026-04-08T00:05:00Z",
              },
            }),
          ],
        },
      },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/autostart/detect": {
        body: makeAutostartDetect({
          platform: "darwin",
          supported: false,
          status: "unsupported",
          configured: false,
          enabled: false,
          canApply: false,
        }),
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(screen.getByRole("button", { name: /自动启动/ }));

    expect(await screen.findByText("当前平台暂不支持自动启动")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "继续" }));

    expect(await screen.findByText("你以后主要怎么使用 VS Code 里的 Codex？")).toBeInTheDocument();
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
          latestBundleEntrypoint: "/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
          recordedBundleEntrypoint: "/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
          candidateBundleEntrypoints: ["/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js"],
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

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(screen.getByRole("button", { name: /VS Code/ }));

    expect(await screen.findByText("你以后主要怎么使用 VS Code 里的 Codex？")).toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /主要去别的 SSH 机器上使用/ }));
    await user.click(screen.getByRole("button", { name: "我会去目标 SSH 机器上处理" }));

    expect(await screen.findByText("已跳过当前机器的 VS Code 接入。等你在目标 SSH 机器上安装 codex-remote 后，再在那里完成 VS Code 接入即可。")).toBeInTheDocument();
    expect(screen.getByText("当前机器未接入；你选择稍后在目标 SSH 机器上处理")).toBeInTheDocument();
    expect(calls.some((call) => call.path === "/api/setup/vscode/apply")).toBe(false);
  });

  it("applies managed shim for current-machine vscode usage", async () => {
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
          latestBundleEntrypoint: "/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
          recordedBundleEntrypoint: "/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
          candidateBundleEntrypoints: ["/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js"],
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
        expect(JSON.parse(String(call.init?.body))).toEqual({ mode: "managed_shim" });
        return {
          body: makeVSCodeDetect({
            currentMode: "managed_shim",
            latestBundleEntrypoint: "/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
            recordedBundleEntrypoint: "/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js",
            candidateBundleEntrypoints: ["/tmp/.vscode/extensions/openai.chatgpt-remote/dist/extension.js"],
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
        };
      },
    });

    render(<SetupRoute />);

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(screen.getByRole("button", { name: /VS Code/ }));

    expect(await screen.findByText("你以后主要怎么使用 VS Code 里的 Codex？")).toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /要在当前这台机器上使用/ }));
    await user.click(screen.getByRole("button", { name: "在这台机器上启用 VS Code" }));

    expect(await screen.findByText("已接管这台机器上的 VS Code 扩展入口。当前策略不会写本机 settings.json；如果扩展升级，回到管理页重新安装扩展入口即可。")).toBeInTheDocument();
    expect(screen.getByText("已在这台机器上接入（扩展入口）")).toBeInTheDocument();
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

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "继续" }));
    await user.click(screen.getByRole("button", { name: /VS Code/ }));

    expect(await screen.findByText("检测到当前是远程 SSH 机器")).toBeInTheDocument();
    expect(screen.getByText("还没检测到这台机器上的 VS Code 扩展。请先在这台机器上打开一次 VS Code Remote 窗口，并确保 Codex 扩展已经安装，然后再回来继续。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "在这台远程机器上启用 VS Code" })).toBeDisabled();
  });
});
