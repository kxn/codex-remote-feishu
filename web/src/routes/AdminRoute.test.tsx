import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { AdminRoute } from "./AdminRoute";
import {
  makeApp,
  makeBootstrap,
  makeImageStagingStatus,
  makeLogsStorageStatus,
  makePermissionCheck,
  makePreviewDriveStatus,
  makeVSCodeDetect,
} from "../test/fixtures";
import { installMockFetch } from "../test/http";

describe("AdminRoute", () => {
  it("keeps local API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/admin");

    const { calls } = installMockFetch({
      "/g/demo/api/admin/bootstrap-state": {
        body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }),
      },
      "/g/demo/api/admin/feishu/apps": {
        body: { apps: [makeApp({ id: "bot-1", name: "Main Bot" })] },
      },
      "/g/demo/api/admin/feishu/apps/bot-1/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-1", name: "Main Bot" }),
          ready: true,
        }),
      },
      "/g/demo/api/admin/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "enabled",
          configured: true,
          enabled: true,
          canApply: true,
        },
      },
      "/g/demo/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/g/demo/api/admin/storage/image-staging": {
        body: makeImageStagingStatus(),
      },
      "/g/demo/api/admin/storage/logs": {
        body: makeLogsStorageStatus(),
      },
      "/g/demo/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
    });

    render(<AdminRoute />);

    expect(
      await screen.findByRole("heading", {
        name: "Codex Remote Feishu v1.7.0 管理",
      }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "机器人管理" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /新增机器人/ })).toBeInTheDocument();
    expect(calls.length).toBeGreaterThan(0);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
    expect(
      calls.some((call) => call.path === "/g/demo/api/admin/bootstrap-state"),
    ).toBe(true);
  });

  it("marks robots with permission issues and shows the warning in detail", async () => {
    window.history.replaceState({}, "", "/admin");

    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [
            makeApp({
              id: "bot-team",
              name: "协作机器人",
              appId: "cli_team",
            }),
          ],
        },
      },
      "/api/admin/feishu/apps/bot-team/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-team", name: "协作机器人", appId: "cli_team" }),
          ready: false,
          missingScopes: [{ scope: "drive:drive", scopeType: "tenant" }],
        }),
      },
      "/api/admin/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "enabled",
          configured: true,
          enabled: true,
          canApply: true,
        },
      },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/storage/image-staging": {
        body: makeImageStagingStatus(),
      },
      "/api/admin/storage/logs": {
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-team": {
        body: makePreviewDriveStatus({ gatewayId: "bot-team", name: "协作机器人" }),
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByText("有异常")).toBeInTheDocument();
    expect(await screen.findByText("当前还需要补齐权限。")).toBeInTheDocument();
    expect(screen.getByText("drive:drive")).toBeInTheDocument();
  });

  it("creates a new robot and switches to its status page after verify", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    let appsConfigured = false;

    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "session-admin-new",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
          },
        },
      },
      "/api/admin/feishu/apps": (call) => {
        if (call.method === "POST") {
          appsConfigured = true;
          return {
            status: 201,
            body: {
              app: makeApp({
                id: "bot-new",
                name: "运营机器人",
                appId: "cli_new",
              }),
            },
          };
        }
        return {
          body: {
            apps: appsConfigured
              ? [
                  makeApp({
                    id: "bot-new",
                    name: "运营机器人",
                    appId: "cli_new",
                    verifiedAt: "2026-04-25T09:10:00Z",
                  }),
                ]
              : [makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" })],
          },
        };
      },
      "/api/admin/feishu/apps/bot-1/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-1", name: "主机器人" }),
          ready: true,
        }),
      },
      "/api/admin/feishu/apps/bot-new/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-new", name: "运营机器人", appId: "cli_new" }),
          ready: true,
        }),
      },
      "/api/admin/feishu/apps/bot-new/verify": {
        body: {
          app: makeApp({
            id: "bot-new",
            name: "运营机器人",
            appId: "cli_new",
            verifiedAt: "2026-04-25T09:10:00Z",
          }),
          result: { connected: true, duration: 1_000_000_000 },
        },
      },
      "/api/admin/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "enabled",
          configured: true,
          enabled: true,
          canApply: true,
        },
      },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/storage/image-staging": {
        body: makeImageStagingStatus(),
      },
      "/api/admin/storage/logs": {
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "主机器人" }),
      },
      "/api/admin/storage/preview-drive/bot-new": {
        body: makePreviewDriveStatus({ gatewayId: "bot-new", name: "运营机器人" }),
      },
    });

    render(<AdminRoute />);

    await user.click(await screen.findByRole("button", { name: /新增机器人/ }));
    expect(await screen.findByRole("button", { name: "扫码创建" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "手动输入" }));
    await user.type(screen.getByLabelText("机器人名称（可选）"), "运营机器人");
    await user.type(screen.getByLabelText("App ID"), "cli_new");
    await user.type(screen.getByLabelText("App Secret"), "secret_new");
    await user.click(screen.getByRole("button", { name: "验证并保存" }));

    expect(await screen.findByRole("heading", { name: "运营机器人" })).toBeInTheDocument();
    expect(await screen.findByText("已完成连接验证。")).toBeInTheDocument();
  });

  it("opens the delete modal and removes the robot after confirmation", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    let removed = false;

    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "session-admin-delete",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
          },
        },
      },
      "/api/admin/feishu/apps": () => ({
        body: {
          apps: removed ? [] : [makeApp({ id: "bot-delete", name: "待删除机器人", appId: "cli_delete" })],
        },
      }),
      "/api/admin/feishu/apps/bot-delete/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-delete", name: "待删除机器人" }),
          ready: true,
        }),
      },
      "/api/admin/feishu/apps/bot-delete": () => {
        removed = true;
        return { body: {} };
      },
      "/api/admin/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "enabled",
          configured: true,
          enabled: true,
          canApply: true,
        },
      },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/storage/image-staging": {
        body: makeImageStagingStatus(),
      },
      "/api/admin/storage/logs": {
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-delete": {
        body: makePreviewDriveStatus({ gatewayId: "bot-delete", name: "待删除机器人" }),
      },
    });

    render(<AdminRoute />);

    await user.click(await screen.findByRole("button", { name: "删除机器人" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("确认删除机器人");
    await user.click(screen.getByRole("button", { name: "确认删除" }));

    expect(await screen.findByRole("heading", { name: "新增机器人" })).toBeInTheDocument();
    expect(await screen.findByText("机器人已删除。")).toBeInTheDocument();
  });

  it("shows the bound-recipient error when event test cannot find a target", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();

    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" })],
        },
      },
      "/api/admin/feishu/apps/bot-1/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-1", name: "主机器人" }),
          ready: true,
        }),
      },
      "/api/admin/feishu/apps/bot-1/test-events": {
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
      "/api/admin/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "enabled",
          configured: true,
          enabled: true,
          canApply: true,
        },
      },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/storage/image-staging": {
        body: makeImageStagingStatus(),
      },
      "/api/admin/storage/logs": {
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "主机器人" }),
      },
    });

    render(<AdminRoute />);

    await user.click(await screen.findByRole("button", { name: "测试事件订阅" }));
    expect(
      await screen.findByText(
        "手动添加的机器人无法自动发送测试消息，请直接在飞书后台继续手动配置。",
      ),
    ).toBeInTheDocument();
  });

  it("cleans up logs and updates the visible count", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();

    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" })],
        },
      },
      "/api/admin/feishu/apps/bot-1/permission-check": {
        body: makePermissionCheck({
          app: makeApp({ id: "bot-1", name: "主机器人" }),
          ready: true,
        }),
      },
      "/api/admin/autostart/detect": {
        body: {
          platform: "linux",
          supported: true,
          status: "enabled",
          configured: true,
          enabled: true,
          canApply: true,
        },
      },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/storage/image-staging": {
        body: makeImageStagingStatus(),
      },
      "/api/admin/storage/logs": {
        body: makeLogsStorageStatus({ fileCount: 128, totalBytes: 860 * 1024 * 1024 }),
      },
      "/api/admin/storage/logs/cleanup": {
        body: {
          rootDir: "/tmp/logs",
          olderThanHours: 24,
          deletedFiles: 70,
          deletedBytes: 440 * 1024 * 1024,
          remainingFileCount: 58,
          remainingBytes: 420 * 1024 * 1024,
        },
      },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "主机器人" }),
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByText("128 个文件，约 860 MB")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "清理一天前日志" }));
    expect(await screen.findByText("58 个文件，约 420 MB")).toBeInTheDocument();
  });
});
