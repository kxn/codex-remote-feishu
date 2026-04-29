import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SetupRoute } from "./SetupRoute";
import type { VSCodeDetectResponse } from "../lib/types";
import {
  makeApp,
  makeBootstrap,
  makeFeishuManifest,
  makePermissionCheck,
  makeRuntimeRequirementsDetect,
  makeVSCodeDetect,
} from "../test/fixtures";
import { installMockFetch } from "../test/http";

describe("SetupRoute", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps local API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    const { calls } = installMockFetch({
      "/g/demo/api/setup/bootstrap-state": {
        body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }),
      },
      "/g/demo/api/setup/feishu/manifest": {
        body: makeFeishuManifest(),
      },
      "/g/demo/api/setup/feishu/apps": { body: { apps: [] } },
      "/g/demo/api/setup/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "session-1",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
          },
        },
      },
      "/g/demo/api/setup/runtime-requirements/detect": {
        body: makeRuntimeRequirementsDetect(),
      },
      "/g/demo/api/setup/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "disabled",
          configured: false,
          enabled: false,
          canApply: true,
        },
      },
      "/g/demo/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(
      await screen.findByRole("heading", {
        name: "Codex Remote Feishu v1.7.0 安装程序",
      }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();
    await waitFor(() => {
      expect(
        calls.some((call) => call.path === "/g/demo/api/setup/feishu/onboarding/sessions"),
      ).toBe(true);
    });
    expect(calls.length).toBeGreaterThan(0);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
  });

  it("connects manually, shows missing permissions, and rechecks into events", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    let permissionChecks = 0;
    let appsConfigured = false;

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "session-1",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
          },
        },
      },
      "/api/setup/runtime-requirements/detect": {
        body: makeRuntimeRequirementsDetect(),
      },
      "/api/setup/feishu/apps": (call) => {
        if (call.method === "POST") {
          appsConfigured = true;
          return {
            status: 201,
            body: {
              app: makeApp({
                id: "bot-manual",
                name: "团队机器人",
                appId: "cli_manual",
              }),
            },
          };
        }
        return {
          body: {
            apps: appsConfigured
              ? [
                  makeApp({
                    id: "bot-manual",
                    name: "团队机器人",
                    appId: "cli_manual",
                    verifiedAt: "2026-04-25T08:10:00Z",
                  }),
                ]
              : [],
          },
        };
      },
      "/api/setup/feishu/apps/bot-manual/verify": {
        body: {
          app: makeApp({
            id: "bot-manual",
            name: "团队机器人",
            appId: "cli_manual",
            verifiedAt: "2026-04-25T08:10:00Z",
          }),
          result: { connected: true, duration: 1_000_000_000 },
        },
      },
      "/api/setup/feishu/apps/bot-manual/permission-check": () => {
        permissionChecks += 1;
        if (permissionChecks === 1) {
          return {
            body: makePermissionCheck({
              app: makeApp({ id: "bot-manual", appId: "cli_manual" }),
              ready: false,
              missingScopes: [{ scope: "drive:drive", scopeType: "tenant" }],
              grantJSON: `{
  "scopes": {
    "tenant": [
      "drive:drive"
    ],
    "user": []
  }
}`,
            }),
          };
        }
        return {
          body: makePermissionCheck({
            app: makeApp({ id: "bot-manual", appId: "cli_manual" }),
            ready: true,
          }),
        };
      },
      "/api/setup/feishu/apps/bot-manual/test-events": {
        body: {
          gatewayId: "bot-manual",
          startedAt: "2026-04-25T08:12:00Z",
          expiresAt: "2026-04-25T08:22:00Z",
          phrase: "测试",
          message: "事件订阅测试提示已发送。",
        },
      },
      "/api/setup/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "disabled",
          configured: false,
          enabled: false,
          canApply: true,
        },
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "手动输入" }));
    await user.type(screen.getByLabelText("机器人名称（可选）"), "团队机器人");
    await user.type(screen.getByLabelText("App ID"), "cli_manual");
    await user.type(screen.getByLabelText("App Secret"), "secret_manual");
    await user.click(screen.getByRole("button", { name: "验证并继续" }));

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    expect(await screen.findByText("当前还不能进入下一步，请先补齐缺失权限。")).toBeInTheDocument();
    expect(screen.getByText("drive:drive")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "我已处理，重新检查" }));
    expect(await screen.findByRole("heading", { name: "事件订阅" })).toBeInTheDocument();
    expect(await screen.findByText("事件订阅测试提示已发送。")).toBeInTheDocument();
  });

  it("starts qr onboarding automatically, polls every 5 seconds, and advances to permissions", async () => {
    window.history.replaceState({}, "", "/setup");
    let appsConfigured = false;

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/feishu/apps": () => ({
        body: {
          apps: appsConfigured
            ? [
                makeApp({
                  id: "bot-qr",
                  name: "扫码机器人",
                  appId: "cli_qr",
                  verifiedAt: "2026-04-25T08:20:00Z",
                }),
              ]
            : [],
        },
      }),
      "/api/setup/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "session-qr",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
          },
        },
      },
      "/api/setup/feishu/onboarding/sessions/session-qr": {
        body: {
          session: {
            id: "session-qr",
            status: "ready",
            qrCodeDataUrl: "data:image/png;base64,abc",
            appId: "cli_qr",
            displayName: "扫码机器人",
          },
        },
      },
      "/api/setup/feishu/onboarding/sessions/session-qr/complete": () => {
        appsConfigured = true;
        return {
          body: {
            app: makeApp({
              id: "bot-qr",
              name: "扫码机器人",
              appId: "cli_qr",
              verifiedAt: "2026-04-25T08:20:00Z",
            }),
            result: { connected: true, duration: 1_000_000_000 },
            session: {
              id: "session-qr",
              status: "completed",
              appId: "cli_qr",
              displayName: "扫码机器人",
            },
          },
        };
      },
      "/api/setup/runtime-requirements/detect": {
        body: makeRuntimeRequirementsDetect(),
      },
      "/api/setup/feishu/apps/bot-qr/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-qr", appId: "cli_qr" }),
          ready: false,
          missingScopes: [{ scope: "drive:drive", scopeType: "tenant" }],
        }),
      },
      "/api/setup/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "disabled",
          configured: false,
          enabled: false,
          canApply: true,
        },
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();

    expect(
      await screen.findByRole("heading", { name: "权限检查" }, { timeout: 7_000 }),
    ).toBeInTheDocument();
    expect(await screen.findByText("当前还不能进入下一步，请先补齐缺失权限。")).toBeInTheDocument();
  }, 10_000);

  it("shows the bound-recipient error when event test target is unavailable", async () => {
    window.history.replaceState({}, "", "/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              id: "bot-1",
              name: "主机器人",
              verifiedAt: "2026-04-25T08:30:00Z",
            }),
          ],
        },
      },
      "/api/setup/runtime-requirements/detect": {
        body: makeRuntimeRequirementsDetect(),
      },
      "/api/setup/feishu/apps/bot-1/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-1" }),
          ready: true,
        }),
      },
      "/api/setup/feishu/apps/bot-1/test-events": {
        status: 409,
        body: {
          error: {
            code: "feishu_app_web_test_recipient_unavailable",
            message: "recipient unavailable",
            details:
              "手动添加的机器人无法自动发送测试消息，请直接在飞书后台继续手动配置。",
          },
        },
      },
      "/api/setup/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "disabled",
          configured: false,
          enabled: false,
          canApply: true,
        },
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "事件订阅" }, { timeout: 2_000 }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText(
        "手动添加的机器人无法自动发送测试消息，请直接在飞书后台继续手动配置。",
      ),
    ).toBeInTheDocument();
  });

  it("copies an event name from the requirement table", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              id: "bot-copy",
              name: "复制机器人",
              verifiedAt: "2026-04-25T08:30:00Z",
            }),
          ],
        },
      },
      "/api/setup/runtime-requirements/detect": {
        body: makeRuntimeRequirementsDetect(),
      },
      "/api/setup/feishu/apps/bot-copy/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-copy" }),
          ready: true,
        }),
      },
      "/api/setup/feishu/apps/bot-copy/test-events": {
        body: {
          gatewayId: "bot-copy",
          startedAt: "2026-04-25T08:12:00Z",
          expiresAt: "2026-04-25T08:22:00Z",
          phrase: "测试",
          message: "事件订阅测试提示已发送。",
        },
      },
      "/api/setup/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "disabled",
          configured: false,
          enabled: false,
          canApply: true,
        },
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "事件订阅" }, { timeout: 2_000 }),
    ).toBeInTheDocument();

    const eventRow = screen.getByText("im.message.receive_v1").closest("tr");
    expect(eventRow).not.toBeNull();

    await user.click(
      within(eventRow as HTMLTableRowElement).getByRole("button", {
        name: /复制事件名 im\.message\.receive_v1/,
      }),
    );

    expect(writeText).toHaveBeenCalledWith("im.message.receive_v1");
    expect(await screen.findByText("已复制事件名。")).toBeInTheDocument();
  });

  it("copies a callback name from the requirement table", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [
            makeApp({
              id: "bot-copy",
              name: "复制机器人",
              verifiedAt: "2026-04-25T08:30:00Z",
            }),
          ],
        },
      },
      "/api/setup/runtime-requirements/detect": {
        body: makeRuntimeRequirementsDetect(),
      },
      "/api/setup/feishu/apps/bot-copy/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-copy" }),
          ready: true,
        }),
      },
      "/api/setup/feishu/apps/bot-copy/test-events": {
        body: {
          gatewayId: "bot-copy",
          startedAt: "2026-04-25T08:12:00Z",
          expiresAt: "2026-04-25T08:22:00Z",
          phrase: "测试",
          message: "事件订阅测试提示已发送。",
        },
      },
      "/api/setup/feishu/apps/bot-copy/test-callback": {
        body: {
          gatewayId: "bot-copy",
          startedAt: "2026-04-25T08:13:00Z",
          expiresAt: "2026-04-25T08:23:00Z",
          message: "回调测试卡片已发送。",
        },
      },
      "/api/setup/feishu/apps/bot-copy/install-tests/events/clear": {
        body: {},
      },
      "/api/setup/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "disabled",
          configured: false,
          enabled: false,
          canApply: true,
        },
      },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "事件订阅" }, { timeout: 2_000 }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "下一步" }));

    expect(await screen.findByRole("heading", { name: "回调配置" })).toBeInTheDocument();

    const callbackRow = screen.getByText("card.action.trigger").closest("tr");
    expect(callbackRow).not.toBeNull();

    await user.click(
      within(callbackRow as HTMLTableRowElement).getByRole("button", {
        name: /复制回调名 card\.action\.trigger/,
      }),
    );

    expect(writeText).toHaveBeenCalledWith("card.action.trigger");
    expect(await screen.findByText("已复制回调名。")).toBeInTheDocument();
  });

  it("recovers when vscode apply times out but detect shows ready", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    const initialDetect = makeVSCodeState({
      latestShim: {
        exists: false,
        installed: false,
        matchesBinary: false,
        realBinaryExists: false,
      },
    });
    const readyDetect = makeVSCodeState();

    installSetupRoutesForVSCode({
      initialDetect,
      recoveryDetect: readyDetect,
      applyHandler: () => new Promise(() => {}),
    });

    render(<SetupRoute />);

    await advanceSetupToVSCode(user);

    await user.click(screen.getByRole("button", { name: "确认集成" }));

    expect(
      await screen.findByRole("heading", { name: "欢迎使用" }, { timeout: 12_000 }),
    ).toBeInTheDocument();
    expect(screen.getByText("VS Code 集成已完成。")).toBeInTheDocument();
  }, 15_000);

  it("restores setup interactivity when vscode apply fails and detect is still not ready", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    const initialDetect = makeVSCodeState({
      latestShim: {
        exists: false,
        installed: false,
        matchesBinary: false,
        realBinaryExists: false,
      },
    });

    installSetupRoutesForVSCode({
      initialDetect,
      recoveryDetect: initialDetect,
      applyHandler: {
        status: 500,
        body: {
          error: {
            code: "vscode_apply_failed",
            message: "failed to apply vscode integration",
          },
        },
      },
    });

    render(<SetupRoute />);

    await advanceSetupToVSCode(user);

    const button = screen.getByRole("button", { name: "确认集成" });
    await user.click(button);

    expect(
      await screen.findByText("当前还不能确认 VS Code 集成结果，请稍后重试。"),
    ).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "VS Code 集成" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "确认集成" })).not.toBeDisabled();
  });
});

async function advanceSetupToVSCode(user: ReturnType<typeof userEvent.setup>) {
  expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
  expect(
    await screen.findByRole("heading", { name: "事件订阅" }, { timeout: 2_000 }),
  ).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "下一步" }));
  expect(await screen.findByRole("heading", { name: "回调配置" })).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "下一步" }));
  expect(await screen.findByRole("heading", { name: "菜单确认" })).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "下一步" }));
  expect(await screen.findByRole("heading", { name: "自动启动" })).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "下一步" }));
  expect(await screen.findByRole("heading", { name: "VS Code 集成" })).toBeInTheDocument();
}

function installSetupRoutesForVSCode(options: {
  initialDetect: VSCodeDetectResponse;
  recoveryDetect?: VSCodeDetectResponse;
  applyHandler: Parameters<typeof installMockFetch>[0][string];
}) {
  let detectCalls = 0;

  installMockFetch({
    "/api/setup/bootstrap-state": { body: makeBootstrap() },
    "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
    "/api/setup/feishu/apps": {
      body: {
        apps: [
          makeApp({
            id: "bot-vscode",
            name: "VS Code 机器人",
            verifiedAt: "2026-04-25T09:00:00Z",
          }),
        ],
      },
    },
    "/api/setup/runtime-requirements/detect": {
      body: makeRuntimeRequirementsDetect(),
    },
    "/api/setup/feishu/apps/bot-vscode/permission-check": {
      body: makePermissionCheck({
        app: makeApp({ id: "bot-vscode" }),
        ready: true,
      }),
    },
    "/api/setup/feishu/apps/bot-vscode/test-events": {
      body: {
        gatewayId: "bot-vscode",
        startedAt: "2026-04-25T09:01:00Z",
        expiresAt: "2026-04-25T09:11:00Z",
        phrase: "测试",
        message: "事件订阅测试提示已发送。",
      },
    },
    "/api/setup/feishu/apps/bot-vscode/test-callback": {
      body: {
        gatewayId: "bot-vscode",
        startedAt: "2026-04-25T09:02:00Z",
        expiresAt: "2026-04-25T09:12:00Z",
        message: "回调测试卡片已发送。",
      },
    },
    "/api/setup/feishu/apps/bot-vscode/install-tests/events/clear": {
      body: {},
    },
    "/api/setup/feishu/apps/bot-vscode/install-tests/callback/clear": {
      body: {},
    },
    "/api/setup/autostart/detect": {
      body: {
        platform: "linux",
        supported: true,
        status: "enabled",
        configured: true,
        enabled: true,
        canApply: true,
      },
    },
    "/api/setup/vscode/detect": () => {
      detectCalls += 1;
      return {
        body:
          detectCalls === 1 || !options.recoveryDetect
            ? options.initialDetect
            : options.recoveryDetect,
      };
    },
    "/api/setup/vscode/apply": options.applyHandler,
  });
}

function makeVSCodeState(
  overrides: Omit<Partial<VSCodeDetectResponse>, "latestShim" | "settings"> & {
    latestShim?: Partial<VSCodeDetectResponse["latestShim"]>;
    settings?: Partial<VSCodeDetectResponse["settings"]>;
  } = {},
): VSCodeDetectResponse {
  const base = makeVSCodeDetect();
  return {
    ...base,
    ...overrides,
    settings: {
      ...base.settings,
      ...(overrides.settings || {}),
    },
    latestShim: {
      ...base.latestShim,
      ...(overrides.latestShim || {}),
    },
  };
}
