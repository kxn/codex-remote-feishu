import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { AdminRoute } from "./AdminRoute";
import {
  makeApp,
  makeBootstrap,
  makeClaudeProfile,
  makeFeishuManifest,
  makeImageStagingStatus,
  makeLogsStorageStatus,
  makeOnboardingWorkflow,
  makePreviewDriveStatus,
} from "../test/fixtures";
import { installMockFetch, type MockFetchCall } from "../test/http";

function withClaudeProfiles(
  routes: Record<string, unknown>,
  profiles = [makeClaudeProfile()],
) {
  return {
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
  it("keeps local workflow API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/admin");

    const { calls } = installMockFetch(withClaudeProfiles({
      "/g/demo/api/admin/bootstrap-state": {
        body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }),
      },
      "/g/demo/api/admin/feishu/apps": {
        body: { apps: [makeApp({ id: "bot-1", name: "Main Bot" })] },
      },
      "/g/demo/api/admin/feishu/manifest": {
        body: makeFeishuManifest(),
      },
      "/g/demo/api/admin/onboarding/workflow?app=bot-1": {
        body: makeOnboardingWorkflow(),
      },
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
    expect(await screen.findByRole("heading", { name: "系统集成" })).toBeInTheDocument();
    expect(screen.queryByText("当前机器人 onboarding 与补救流程。")).not.toBeInTheDocument();
    expect(screen.queryByText("设置流程")).not.toBeInTheDocument();
    expect(screen.queryByText(/workflow/i)).not.toBeInTheDocument();
    expect(calls.length).toBeGreaterThan(0);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
    expect(
      calls.some((call) => call.path === "/g/demo/api/admin/onboarding/workflow?app=bot-1"),
    ).toBe(true);
    expect(calls.some((call) => call.path === "/g/demo/api/admin/claude/profiles")).toBe(
      true,
    );
    expect(calls.some((call) => call.path.includes("/permission-check"))).toBe(false);
    expect(calls.some((call) => call.path.endsWith("/autostart/detect"))).toBe(false);
    expect(calls.some((call) => call.path.endsWith("/vscode/detect"))).toBe(false);
  });

  it("shows permission remediation inside the admin robot detail", async () => {
    window.history.replaceState({}, "", "/admin");

    const { calls } = installMockFetch(withClaudeProfiles({
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
      "/api/admin/feishu/manifest": { body: makeFeishuManifest() },
      "/api/admin/onboarding/workflow?app=bot-team": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-team",
          currentStage: "permission",
          app: {
            app: makeApp({
              id: "bot-team",
              name: "协作机器人",
              appId: "cli_team",
            }),
            permission: {
              status: "pending",
              summary: "当前还缺少建议补齐的权限。你可以补齐后继续，或者先跳过这一步。",
              missingScopes: [{ scope: "drive:drive", scopeType: "tenant" }],
            },
          },
        }),
      },
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

    expect(await screen.findByText("权限检查")).toBeInTheDocument();
    expect(
      await screen.findByText(
        "如果当前企业权限暂时申请不到，你也可以先跳过这一步，后面再回来补齐。",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "强制跳过这一步" })).toBeInTheDocument();
    expect(screen.getByText("drive:drive")).toBeInTheDocument();
    expect(calls.some((call) => call.path.includes("/permission-check"))).toBe(false);
  });

  it("creates a new robot and switches to its workflow detail after verify", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    let appsConfigured = false;

    installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
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
      "/api/admin/feishu/manifest": { body: makeFeishuManifest() },
      "/api/admin/onboarding/workflow?app=bot-1": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-1",
          currentStage: "permission",
          app: {
            app: makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
          },
        }),
      },
      "/api/admin/onboarding/workflow?app=bot-new": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-new",
          currentStage: "permission",
          app: {
            app: makeApp({
              id: "bot-new",
              name: "运营机器人",
              appId: "cli_new",
              verifiedAt: "2026-04-25T09:10:00Z",
            }),
          },
        }),
      },
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
    await user.click(screen.getByRole("button", { name: "验证并保存" }));

    expect(await screen.findByRole("heading", { name: "运营机器人" })).toBeInTheDocument();
    expect(await screen.findByText("权限检查")).toBeInTheDocument();
    expect(await screen.findByText("当前还有待处理项，完成后这里会恢复为正常状态。")).toBeInTheDocument();
    expect(screen.queryByText("当前飞书应用已经接入，下面请继续补齐剩余联调与机器决策。")).not.toBeInTheDocument();
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
      "/api/admin/feishu/manifest": { body: makeFeishuManifest() },
      "/api/admin/onboarding/workflow?app=bot-delete": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-delete",
          currentStage: "permission",
          app: {
            app: makeApp({ id: "bot-delete", name: "待删除机器人", appId: "cli_delete" }),
          },
        }),
      },
      "/api/admin/feishu/apps/bot-delete": () => {
        removed = true;
        return { body: {} };
      },
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
      "/api/admin/feishu/manifest": { body: makeFeishuManifest() },
      "/api/admin/onboarding/workflow?app=bot-1": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-1",
          currentStage: "events",
          app: {
            app: makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
            permission: {
              status: "complete",
              summary: "当前基础权限已经齐全。",
              missingScopes: [],
              grantJSON: "",
            },
            events: {
              status: "pending",
              allowedActions: ["start_test", "confirm"],
            },
          },
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

    expect(await screen.findByText("事件订阅")).toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "重新发送测试提示" })[0]);
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
      "/api/admin/feishu/manifest": { body: makeFeishuManifest() },
      "/api/admin/onboarding/workflow?app=bot-1": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-1",
          currentStage: "permission",
          app: {
            app: makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
          },
        }),
      },
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

  it("shows the built-in default Claude profile as read-only", async () => {
    window.history.replaceState({}, "", "/admin");

    installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" })],
        },
      },
      "/api/admin/feishu/manifest": { body: makeFeishuManifest() },
      "/api/admin/onboarding/workflow?app=bot-1": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-1",
          currentStage: "permission",
          app: {
            app: makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
          },
        }),
      },
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

    expect(await screen.findByRole("heading", { name: "Claude 配置" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "默认" })).toBeInTheDocument();
    expect(await screen.findByText("系统默认配置")).toBeInTheDocument();
    expect(
      await screen.findByText("这个配置会沿用当前 Claude 在本机上的默认认证、端点和模型设置。"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "新增自定义配置" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "删除配置" })).not.toBeInTheDocument();
  });

  it("copies a Claude profile without reusing the old token, then clears and deletes it", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    let profiles = [
      makeClaudeProfile(),
      makeClaudeProfile({
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
      }),
    ];
    let createCount = 0;
    let updateCount = 0;

    installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" })],
        },
      },
      "/api/admin/claude/profiles": (call: MockFetchCall) => {
        if (call.method === "POST") {
          createCount += 1;
          const payload = JSON.parse(String(call.init?.body ?? "{}")) as Record<string, unknown>;
          expect(payload.name).toBe("DevSeek 副本");
          expect(payload.baseURL).toBe("https://proxy.internal/v1");
          expect("authToken" in payload).toBe(false);
          const created = makeClaudeProfile({
            id: "devseek-copy",
            name: "DevSeek 副本",
            authMode: "auth_token",
            baseURL: "https://proxy.internal/v1",
            hasAuthToken: false,
            model: "mimo-v2.5-pro",
            smallModel: "mimo-v2.5-haiku",
            builtIn: false,
            persisted: true,
            readOnly: false,
          });
          profiles = [profiles[0], profiles[1], created];
          return {
            status: 201,
            body: { profile: created },
          };
        }
        return {
          body: { profiles },
        };
      },
      "/api/admin/claude/profiles/devseek": (call: MockFetchCall) => {
        if (call.method === "PUT") {
          updateCount += 1;
          const payload = JSON.parse(String(call.init?.body ?? "{}")) as Record<string, unknown>;
          expect(payload.clearAuthToken).toBe(true);
          const updated = makeClaudeProfile({
            id: "devseek",
            name: "DevSeek",
            authMode: "auth_token",
            baseURL: "https://proxy.internal/v1",
            hasAuthToken: false,
            model: "mimo-v2.5-pro",
            smallModel: "mimo-v2.5-haiku",
            builtIn: false,
            persisted: true,
            readOnly: false,
          });
          profiles = profiles.map((profile) =>
            profile.id === "devseek" ? updated : profile,
          );
          return {
            body: { profile: updated },
          };
        }
        return { body: {} };
      },
      "/api/admin/claude/profiles/devseek-copy": () => {
        profiles = profiles.filter((profile) => profile.id !== "devseek-copy");
        return {
          status: 204,
          body: {},
        };
      },
      "/api/admin/feishu/manifest": { body: makeFeishuManifest() },
      "/api/admin/onboarding/workflow?app=bot-1": {
        body: makeOnboardingWorkflow({
          selectedAppId: "bot-1",
          currentStage: "permission",
          app: {
            app: makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
          },
        }),
      },
      "/api/admin/storage/image-staging": {
        body: makeImageStagingStatus(),
      },
      "/api/admin/storage/logs": {
        body: makeLogsStorageStatus(),
      },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "主机器人" }),
      },
    }, profiles));

    render(<AdminRoute />);

    const originalProfileButton = (await screen.findByText("DevSeek")).closest(
      "button",
    );
    expect(originalProfileButton).not.toBeNull();
    await user.click(originalProfileButton!);
    await user.click(screen.getByRole("button", { name: "复制为新配置" }));
    expect(await screen.findByText("已带入可见字段。你可以补充新的 Token，也可以先留空保存。")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByText("Claude 配置已创建。")).toBeInTheDocument();
    expect(createCount).toBe(1);
    expect(await screen.findByRole("heading", { name: "DevSeek 副本" })).toBeInTheDocument();
    expect(await screen.findByText("正在等待新的 Token")).toBeInTheDocument();

    const originalProfileButtonAgain = screen.getByText("DevSeek").closest("button");
    expect(originalProfileButtonAgain).not.toBeNull();
    await user.click(originalProfileButtonAgain!);
    await user.click(screen.getByLabelText(/清除已保存 Token/));
    await user.click(screen.getByRole("button", { name: "保存修改" }));
    expect(await screen.findByText("Claude 配置已保存。")).toBeInTheDocument();
    expect(updateCount).toBe(1);

    const copiedProfileButton = screen.getByText("DevSeek 副本").closest("button");
    expect(copiedProfileButton).not.toBeNull();
    await user.click(copiedProfileButton!);
    await user.click(screen.getByRole("button", { name: "删除配置" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("确认删除 Claude 配置");
    await user.click(screen.getByRole("button", { name: "确认删除" }));
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: /DevSeek 副本/ }),
      ).not.toBeInTheDocument();
    });
  });
});
