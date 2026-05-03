import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { AdminRoute } from "./AdminRoute";
import {
  makeApp,
  makeBootstrap,
  makeClaudeProfile,
  makeCodexProvider,
  makeImageStagingStatus,
  makeLogsStorageStatus,
  makePermissionCheck,
  makePreviewDriveStatus,
  makeVSCodeDetect,
} from "../test/fixtures";
import { installMockFetch, type MockFetchCall } from "../test/http";

function withClaudeProfiles(
  routes: Record<string, unknown>,
  profiles = [makeClaudeProfile()],
) {
  return {
    "/api/admin/codex/providers": {
      body: { providers: [makeCodexProvider()] },
    },
    "/g/demo/api/admin/codex/providers": {
      body: { providers: [makeCodexProvider()] },
    },
    "/api/admin/claude/profiles": {
      body: { profiles },
    },
    "/g/demo/api/admin/claude/profiles": {
      body: { profiles },
    },
    ...routes,
  };
}

describe("AdminRoute", () => {
  it("keeps local API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/admin");

    const { calls } = installMockFetch(withClaudeProfiles({
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
    }));

    render(<AdminRoute />);

    expect(
      await screen.findByRole("heading", {
        name: "Codex Remote Feishu v1.7.0 管理",
      }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "机器人管理" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Claude 配置" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Codex Provider" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /新增机器人/ })).toBeInTheDocument();
    expect(calls.length).toBeGreaterThan(0);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
    expect(
      calls.some((call) => call.path === "/g/demo/api/admin/bootstrap-state"),
    ).toBe(true);
    expect(calls.some((call) => call.path === "/g/demo/api/admin/claude/profiles")).toBe(
      true,
    );
    expect(calls.some((call) => call.path === "/g/demo/api/admin/codex/providers")).toBe(
      true,
    );
  });

  it("marks robots with permission issues and shows the warning in detail", async () => {
    window.history.replaceState({}, "", "/admin");

    installMockFetch(withClaudeProfiles({
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
    }));

    render(<AdminRoute />);

    expect(await screen.findByText("有异常")).toBeInTheDocument();
    expect(await screen.findByText("当前还需要补齐权限。")).toBeInTheDocument();
    expect(screen.getByText("drive:drive")).toBeInTheDocument();
  });

  it("creates a new robot and switches to its status page after verify", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    let appsConfigured = false;

    installMockFetch(withClaudeProfiles({
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
      "/api/admin/feishu/apps": (call: MockFetchCall) => {
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
    }));

    render(<AdminRoute />);

    await user.click(await screen.findByRole("button", { name: /新增机器人/ }));
    expect(await screen.findByRole("button", { name: "扫码创建" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "手动输入" }));
    await user.type(screen.getByLabelText("机器人名称（可选）"), "运营机器人");
    await user.type(screen.getByLabelText("App ID"), "cli_new");
    await user.type(screen.getByLabelText("App Secret"), "secret_new");
    await user.click(screen.getByRole("button", { name: "连接并验证" }));

    expect(await screen.findByRole("heading", { name: "运营机器人" })).toBeInTheDocument();
    expect(await screen.findByText("已完成连接验证。")).toBeInTheDocument();
  });

  it("opens the delete modal and removes the robot after confirmation", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    let removed = false;

    installMockFetch(withClaudeProfiles({
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
    }));

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

    installMockFetch(withClaudeProfiles({
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
    }));

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

    installMockFetch(withClaudeProfiles({
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
    }));

    render(<AdminRoute />);

    expect(await screen.findByText("128 个文件，约 860 MB")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "清理一天前日志" }));
    expect(await screen.findByText("58 个文件，约 420 MB")).toBeInTheDocument();
  });

  it("renders the Claude configuration panel on the v1.7.0 admin layout", async () => {
    window.history.replaceState({}, "", "/admin");

    installMockFetch(withClaudeProfiles({
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
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "主机器人" }),
      },
    }));

    render(<AdminRoute />);

    const heading = await screen.findByRole("heading", { name: "Claude 配置" });
    const section = heading.closest("section");
    expect(section).not.toBeNull();
    expect(within(section as HTMLElement).getByText("本机默认配置")).toBeInTheDocument();
  });

  it("renders the Codex provider panel on the v1.7.0 admin layout", async () => {
    window.history.replaceState({}, "", "/admin");

    installMockFetch(withClaudeProfiles({
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
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "主机器人" }),
      },
    }));

    render(<AdminRoute />);

    const heading = await screen.findByRole("heading", { name: "Codex Provider" });
    const section = heading.closest("section");
    expect(section).not.toBeNull();
    expect(within(section as HTMLElement).getByText("本机默认配置")).toBeInTheDocument();
  });

  it("keeps Claude profile editing user-facing and saves by required name", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    let profile = makeClaudeProfile({
      id: "devseek",
      name: "DevSeek",
      authMode: "auth_token",
      baseURL: "https://proxy.internal/v1",
      hasAuthToken: true,
      model: "mimo-v2.5-pro",
      smallModel: "mimo-v2.5-haiku",
      builtIn: false,
      persisted: true,
      readOnly: false,
    });

    const { calls } = installMockFetch(withClaudeProfiles({
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
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "主机器人" }),
      },
      "/api/admin/claude/profiles": (call: MockFetchCall) => {
        if (call.method === "POST") {
          const body = JSON.parse(String(call.init?.body ?? "{}"));
          profile = makeClaudeProfile({
            id: "test-profile",
            name: body.name,
            authMode: "auth_token",
            baseURL: body.baseURL,
            hasAuthToken: Boolean(body.authToken),
            model: body.model,
            smallModel: body.smallModel,
            reasoningEffort: body.reasoningEffort,
            builtIn: false,
            persisted: true,
            readOnly: false,
          });
          return { status: 201, body: { profile } };
        }
        return { body: { profiles: [makeClaudeProfile(), profile] } };
      },
      "/api/admin/claude/profiles/devseek": (call: MockFetchCall) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        profile = makeClaudeProfile({
          id: "devseek-updated",
          name: body.name,
          authMode: "auth_token",
          baseURL: body.baseURL,
          hasAuthToken: true,
          model: body.model,
          smallModel: body.smallModel,
          reasoningEffort: body.reasoningEffort,
          builtIn: false,
          persisted: true,
          readOnly: false,
        });
        return { body: { profile } };
      },
    }, [makeClaudeProfile(), profile]));

    render(<AdminRoute />);

    await user.click(await screen.findByRole("button", { name: /DevSeek/ }));

    expect(screen.queryByText("认证方式")).not.toBeInTheDocument();
    expect(screen.queryByText("Token 状态")).not.toBeInTheDocument();
    expect(screen.queryByText("Token 处理方式")).not.toBeInTheDocument();
    expect(screen.queryByText(/不会再次回显/)).not.toBeInTheDocument();
    expect(screen.queryByText(/自动生成/)).not.toBeInTheDocument();

    const nameInput = screen.getByLabelText(/名称/);
    await user.clear(nameInput);
    await user.type(nameInput, "DevSeek Updated");
    await user.clear(screen.getByLabelText("端点地址"));
    await user.type(screen.getByLabelText("端点地址"), "https://proxy.updated/v1");
    await user.selectOptions(screen.getByLabelText("推理强度"), "max");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    expect(await screen.findByText("Claude 配置已保存。")).toBeInTheDocument();
    const updateCall = calls.find(
      (call) => call.method === "PUT" && call.path === "/api/admin/claude/profiles/devseek",
    );
    expect(updateCall).toBeDefined();
    expect(JSON.parse(String(updateCall?.init?.body))).toEqual({
      name: "DevSeek Updated",
      baseURL: "https://proxy.updated/v1",
      model: "mimo-v2.5-pro",
      smallModel: "mimo-v2.5-haiku",
      reasoningEffort: "max",
    });
    expect(await screen.findByRole("button", { name: /DevSeek Updated/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /DevSeek$/ })).not.toBeInTheDocument();

    const claudeSection = screen
      .getByRole("heading", { name: "Claude 配置" })
      .closest("section");
    expect(claudeSection).not.toBeNull();

    await user.click(
      within(claudeSection as HTMLElement).getByRole("button", { name: /新增配置/ }),
    );
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByText("请填写名称。")).toBeInTheDocument();

    await user.type(screen.getByLabelText(/名称/), "测试配置");
    await user.type(screen.getByLabelText("认证 Token"), "new-token");
    await user.selectOptions(screen.getByLabelText("推理强度"), "high");
    await user.click(screen.getByRole("button", { name: "保存配置" }));

    const createCall = calls.find(
      (call) => call.method === "POST" && call.path === "/api/admin/claude/profiles",
    );
    expect(createCall).toBeDefined();
    expect(JSON.parse(String(createCall?.init?.body))).toEqual({
      name: "测试配置",
      baseURL: "",
      authToken: "new-token",
      model: "",
      smallModel: "",
      reasoningEffort: "high",
    });
  });
});
