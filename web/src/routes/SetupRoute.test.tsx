import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SetupRoute } from "./SetupRoute";
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

  it("starts qr onboarding automatically, polls every 2 seconds, and advances to permissions", async () => {
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
      await screen.findByRole("heading", { name: "权限检查" }, { timeout: 4_000 }),
    ).toBeInTheDocument();
    expect(await screen.findByText("当前还不能进入下一步，请先补齐缺失权限。")).toBeInTheDocument();
  });

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
              "当前机器人还没有可用的飞书测试接收者。请优先使用扫码创建完成一次连接，或直接在飞书后台继续手动配置。",
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
        "当前机器人还没有可用的飞书测试接收者。请优先使用扫码创建完成一次连接，或直接在飞书后台继续手动配置。",
      ),
    ).toBeInTheDocument();
  });
});
