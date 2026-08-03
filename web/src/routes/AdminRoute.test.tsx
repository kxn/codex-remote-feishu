import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AdminRoute } from "./AdminRoute";
import {
  makeApp,
  makeAutoConfigCompleteResponse,
  makeAutoConfigPlan,
  makeAutoConfigPlanResponse,
  makeBootstrap,
  makeClaudeProfile,
  makeCodexProfile,
  makeImageStagingStatus,
  makeLogsStorageStatus,
  makePreviewDriveStatus,
  makeVSCodeDetect,
} from "../test/fixtures";
import { installMockFetch, type MockFetchCall } from "../test/http";

function withClaudeProfiles(
  routes: Record<string, unknown>,
  profiles = [makeClaudeProfile()],
) {
  return {
    "/api/admin/codex/profiles": {
      body: { profiles: [makeCodexProfile()] },
    },
    "/g/demo/api/admin/codex/profiles": {
      body: { profiles: [makeCodexProfile()] },
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

function makeSingleRobotAdminRoutes(
  app = makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
  routes: Record<string, unknown> = {},
) {
  return withClaudeProfiles({
    "/api/admin/bootstrap-state": { body: makeBootstrap() },
    "/api/admin/feishu/apps": {
      body: {
        apps: [app],
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
    [`/api/admin/storage/preview-drive/${app.id}`]: {
      body: makePreviewDriveStatus({
        gatewayId: app.id,
        name: app.name,
      }),
    },
    ...routes,
  });
}

function makeAdminAutoConfigPlan(
  app = makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
  planOverrides: Parameters<typeof makeAutoConfigPlan>[0] = {},
) {
  return makeAutoConfigPlanResponse({
    app,
    plan: makeAutoConfigPlan({
      status: "clean",
      summary: "飞书配置已就绪。",
      blockingRequirements: [],
      degradableRequirements: [],
      ...planOverrides,
    }),
  });
}

async function openAdminArea(
  user: ReturnType<typeof userEvent.setup>,
  name: "总览" | "机器人" | "对话后端" | "系统",
) {
  await user.click(screen.getAllByRole("button", { name })[0]);
}

describe("AdminRoute", () => {
  it("keeps local API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/admin");
    const user = userEvent.setup();

    const { calls } = installMockFetch(withClaudeProfiles({
      "/g/demo/api/admin/bootstrap-state": {
        body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }),
      },
      "/g/demo/api/admin/feishu/apps": {
        body: { apps: [makeApp({ id: "bot-1", name: "Main Bot" })] },
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
    expect(screen.getAllByRole("button", { name: "机器人" }).length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: "对话后端" }).length).toBeGreaterThan(0);
    await openAdminArea(user, "机器人");
    expect(await screen.findByRole("heading", { name: "机器人" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /添加机器人/ })).toBeInTheDocument();
    await openAdminArea(user, "对话后端");
    expect(await screen.findByRole("heading", { name: "Claude" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Codex" }));
    expect(await screen.findByRole("heading", { name: "Codex" })).toBeInTheDocument();
    expect(calls.length).toBeGreaterThan(0);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
    expect(
      calls.some((call) => call.path === "/g/demo/api/admin/bootstrap-state"),
    ).toBe(true);
    expect(calls.some((call) => call.path === "/g/demo/api/admin/claude/profiles")).toBe(
      true,
    );
    expect(calls.some((call) => call.path === "/g/demo/api/admin/codex/profiles")).toBe(
      true,
    );
  });

  it("checks robot permissions through the admin route and displays backend-provided scopes", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    const originalClipboard = navigator.clipboard;
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [
            makeApp({
              id: "bot-permission",
              name: "权限机器人",
              appId: "cli_permission",
            }),
          ],
        },
      },
      "/api/admin/feishu/apps/bot-permission/permissions/check": {
        body: {
          app: makeApp({
            id: "bot-permission",
            name: "权限机器人",
            appId: "cli_permission",
          }),
          ready: false,
          missingScopes: [
            {
              scope: "im:message.group_msg:readonly",
              scopeType: "tenant",
            },
          ],
          grantJSON:
            '{\n  "scopes": {\n    "tenant": [\n      "im:message.group_msg:readonly"\n    ],\n    "user": []\n  }\n}',
          lastCheckedAt: "2026-08-02T06:30:00Z",
        },
      },
      "/api/admin/feishu/apps/bot-permission/auto-config/plan": {
        body: makeAdminAutoConfigPlan(makeApp({
          id: "bot-permission",
          name: "权限机器人",
          appId: "cli_permission",
        })),
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
      "/api/admin/storage/preview-drive/bot-permission": {
        body: makePreviewDriveStatus({
          gatewayId: "bot-permission",
          name: "权限机器人",
        }),
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: "检查权限" }));
    expect(await screen.findByText("还缺少 1 项权限")).toBeInTheDocument();
    expect(screen.getByText("im:message.group_msg:readonly")).toBeInTheDocument();
    expect(screen.getByDisplayValue(/im:message\.group_msg:readonly/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "复制导入 JSON" }));
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining("im:message.group_msg:readonly"));
    expect(await screen.findByText("导入 JSON 已复制。")).toBeInTheDocument();

    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: originalClipboard,
    });
  });

  it("completes permission fixes from the permission card", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    const app = makeApp({
      id: "bot-permission",
      name: "权限机器人",
      appId: "cli_permission",
    });
    let checkCount = 0;
    let completed = false;

    const { calls } = installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [app],
        },
      },
      "/api/admin/feishu/apps/bot-permission/permissions/check": () => {
        checkCount += 1;
        return {
          body: {
            app,
            ready: checkCount > 1,
            missingScopes:
              checkCount > 1
                ? []
                : [
                    {
                      scope: "im:message.group_at_msg:readonly",
                      scopeType: "tenant",
                    },
                  ],
            grantJSON:
              '{\n  "scopes": {\n    "tenant": [\n      "im:message.group_at_msg:readonly"\n    ],\n    "user": []\n  }\n}',
            lastCheckedAt: "2026-08-02T06:30:00Z",
          },
        };
      },
      "/api/admin/feishu/apps/bot-permission/auto-config/plan": () => {
        return {
          body: makeAdminAutoConfigPlan(app, {
            status: completed
              ? "awaiting_review"
              : "apply_required",
            summary: completed
              ? "飞书正在审核发布。"
              : "飞书配置还需要补齐。",
            publish: {
              needsPublish: !completed,
              awaitingReview: completed,
            },
          }),
        };
      },
      "/api/admin/feishu/apps/bot-permission/auto-config/complete": () => {
        completed = true;
        return {
          body: makeAutoConfigCompleteResponse({
            app,
            result: {
              status: "awaiting_review",
              summary: "飞书正在审核发布。",
              blockingReason: "",
              actions: [],
              plan: makeAutoConfigPlan({
                status: "awaiting_review",
                summary: "飞书正在审核发布。",
                publish: {
                  needsPublish: false,
                  awaitingReview: true,
                },
              }),
            },
          }),
        };
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
      "/api/admin/storage/preview-drive/bot-permission": {
        body: makePreviewDriveStatus({
          gatewayId: "bot-permission",
          name: "权限机器人",
        }),
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: "检查权限" }));
    expect(await screen.findByText("还缺少 1 项权限")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "自动补齐" }));
    expect(await screen.findByText("飞书正在审核发布。")).toBeInTheDocument();
    expect(await screen.findByText("飞书正在审核发布")).toBeInTheDocument();
    expect(calls.some((call) => call.path.endsWith("/auto-config/complete"))).toBe(true);
    expect(calls.some((call) => call.path.endsWith("/auto-config/apply"))).toBe(false);
    expect(calls.some((call) => call.path.endsWith("/auto-config/publish"))).toBe(false);
  });

  it("uses automatic completion when missing permissions only need release", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    const app = makeApp({
      id: "bot-publish-permission",
      name: "待发布机器人",
      appId: "cli_publish_permission",
    });
    let completed = false;

    const { calls } = installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [app],
        },
      },
      "/api/admin/feishu/apps/bot-publish-permission/permissions/check": {
        body: {
          app,
          ready: false,
          missingScopes: [
            {
              scope: "drive:drive",
              scopeType: "tenant",
            },
          ],
          grantJSON:
            '{\n  "scopes": {\n    "tenant": [\n      "drive:drive"\n    ],\n    "user": []\n  }\n}',
          lastCheckedAt: "2026-08-02T06:30:00Z",
        },
      },
      "/api/admin/feishu/apps/bot-publish-permission/auto-config/plan": {
        body: makeAdminAutoConfigPlan(app, {
          status: "publish_required",
          summary: "自动补齐已完成，还需要提交发布。",
          publish: {
            needsPublish: true,
            awaitingReview: false,
          },
        }),
      },
      "/api/admin/feishu/apps/bot-publish-permission/auto-config/complete": () => {
        completed = true;
        return {
          body: makeAutoConfigCompleteResponse({
            app,
            result: {
              status: "awaiting_review",
              summary: "飞书正在审核发布。",
              blockingReason: "",
              actions: [],
              plan: makeAutoConfigPlan({
                status: "awaiting_review",
                summary: "飞书正在审核发布。",
                publish: {
                  needsPublish: false,
                  awaitingReview: true,
                },
              }),
            },
          }),
        };
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
      "/api/admin/storage/preview-drive/bot-publish-permission": {
        body: makePreviewDriveStatus({
          gatewayId: "bot-publish-permission",
          name: "待发布机器人",
        }),
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: "检查权限" }));
    expect(await screen.findByText("还缺少 1 项权限")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "自动补齐" }));
    expect(await screen.findByText("飞书正在审核发布。")).toBeInTheDocument();
    expect(completed).toBe(true);
    expect(calls.some((call) => call.path.endsWith("/auto-config/complete"))).toBe(true);
    expect(calls.some((call) => call.path.endsWith("/auto-config/apply"))).toBe(false);
    expect(calls.some((call) => call.path.endsWith("/auto-config/publish"))).toBe(false);
  });

  it("shows a ready permission check result", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    const app = makeApp({
      id: "bot-ready",
      name: "权限就绪机器人",
      appId: "cli_ready",
    });

    installMockFetch(makeSingleRobotAdminRoutes(app, {
      "/api/admin/feishu/apps/bot-ready/permissions/check": {
        body: {
          app,
          ready: true,
          missingScopes: [],
          grantJSON: '{\n  "scopes": {\n    "tenant": [],\n    "user": []\n  }\n}',
          lastCheckedAt: "2026-08-02T06:30:00Z",
        },
      },
      "/api/admin/feishu/apps/bot-ready/auto-config/plan": {
        body: makeAdminAutoConfigPlan(app),
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: "检查权限" }));

    expect(await screen.findByText("权限已就绪")).toBeInTheDocument();
    expect(screen.queryByText(/还缺少/)).not.toBeInTheDocument();
  });

  it("shows an actionable auto-config read failure after permission check", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    const app = makeApp({
      id: "bot-plan-error",
      name: "状态待确认机器人",
      appId: "cli_plan_error",
    });

    installMockFetch(makeSingleRobotAdminRoutes(app, {
      "/api/admin/feishu/apps/bot-plan-error/permissions/check": {
        body: {
          app,
          ready: false,
          missingScopes: [
            {
              scope: "im:message.group_at_msg:readonly",
              scopeType: "tenant",
            },
          ],
          grantJSON:
            '{\n  "scopes": {\n    "tenant": [\n      "im:message.group_at_msg:readonly"\n    ],\n    "user": []\n  }\n}',
          lastCheckedAt: "2026-08-02T06:30:00Z",
        },
      },
      "/api/admin/feishu/apps/bot-plan-error/auto-config/plan": {
        status: 502,
        body: {
          error: {
            code: "feishu_auto_config_plan_failed",
            message: "raw backend verification error",
          },
        },
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: "检查权限" }));

    expect(await screen.findByText("还缺少 1 项权限")).toBeInTheDocument();
    expect(screen.getByText("暂时无法确认飞书自动配置状态。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "自动补齐" })).toBeInTheDocument();
    expect(screen.queryByText("raw backend verification error")).not.toBeInTheDocument();
  });

  it("shows a retryable permission check failure", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();
    const app = makeApp({
      id: "bot-failed",
      name: "权限检查失败机器人",
      appId: "cli_failed",
    });

    installMockFetch(makeSingleRobotAdminRoutes(app, {
      "/api/admin/feishu/apps/bot-failed/permissions/check": {
        status: 502,
        body: {
          error: {
            code: "feishu_permission_check_failed",
            message: "failed to check feishu app permissions",
          },
        },
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: "检查权限" }));

    expect(await screen.findByText("当前还不能完成权限检查，请稍后重试。")).toBeInTheDocument();
    expect(screen.getByText("权限检查没有完成，请稍后重试。")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "重新检查" }).length).toBeGreaterThan(0);
  });

  it("does not show or request admin auto-config details", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();

    const { calls } = installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [
            makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
            makeApp({ id: "bot-2", name: "备用机器人", appId: "cli_backup" }),
          ],
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
      "/api/admin/storage/preview-drive/bot-2": {
        body: makePreviewDriveStatus({ gatewayId: "bot-2", name: "备用机器人" }),
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "机器人");
    await screen.findByRole("heading", { name: "主机器人" });
    await user.click(screen.getByRole("button", { name: /备用机器人/ }));

    expect(await screen.findByRole("heading", { name: "备用机器人" })).toBeInTheDocument();
    expect(
      calls.some((call) => call.path.includes("/auto-config/")),
    ).toBe(false);
    expect(screen.queryByText("自动配置")).not.toBeInTheDocument();
    expect(screen.queryByText("有降级")).not.toBeInTheDocument();
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

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: /添加机器人/ }));
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

    await openAdminArea(user, "机器人");
    await user.click(await screen.findByRole("button", { name: "删除机器人" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("确认删除机器人");
    await user.click(screen.getByRole("button", { name: "确认删除" }));

    expect(await screen.findByRole("heading", { name: "添加机器人" })).toBeInTheDocument();
    expect(await screen.findByText("机器人已删除。")).toBeInTheDocument();
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

    await openAdminArea(user, "系统");
    expect(await screen.findByText("128 个文件，约 860 MB")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "清理一天前日志" }));
    expect(await screen.findByText("58 个文件，约 420 MB")).toBeInTheDocument();
  });

  it("keeps system maintenance errors user-facing", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();

    installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [
            makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" }),
            makeApp({ id: "bot-2", name: "备用机器人", appId: "cli_backup" }),
          ],
        },
      },
      "/api/admin/autostart/detect": {
        status: 500,
        body: {
          error: {
            code: "autostart_detect_failed",
            message: "failed to detect autostart state",
            details: "query task scheduler task \\\\CodexRemoteFeishu\\\\stable: exit status 1: ����",
          },
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
      "/api/admin/storage/preview-drive/bot-2": {
        status: 500,
        body: {
          error: {
            code: "preview_drive_status_failed",
            message: "failed to read preview drive status",
          },
        },
      },
    }));

    render(<AdminRoute />);

    await openAdminArea(user, "系统");
    expect(await screen.findByText("自动运行状态暂时无法读取，请稍后重试。")).toBeInTheDocument();
    expect(screen.getByText("部分机器人预览文件状态暂时无法读取，请稍后重试。")).toBeInTheDocument();
    expect(screen.queryByText(/autostart_detect_failed/)).not.toBeInTheDocument();
    expect(screen.queryByText(/task scheduler/)).not.toBeInTheDocument();
    expect(screen.queryByText(/����/)).not.toBeInTheDocument();
  });

  it("renders the Claude configuration panel on the v1.7.0 admin layout", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();

    installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" })],
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

    await openAdminArea(user, "对话后端");
    const heading = await screen.findByRole("heading", { name: "Claude" });
    const section = heading.closest("section");
    expect(section).not.toBeNull();
    expect(within(section as HTMLElement).getByText("本机默认配置")).toBeInTheDocument();
  });

  it("renders the Codex provider panel on the v1.7.0 admin layout", async () => {
    window.history.replaceState({}, "", "/admin");
    const user = userEvent.setup();

    installMockFetch(withClaudeProfiles({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/feishu/apps": {
        body: {
          apps: [makeApp({ id: "bot-1", name: "主机器人", appId: "cli_main" })],
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

    await openAdminArea(user, "对话后端");
    await user.click(screen.getByRole("button", { name: "Codex" }));
    const heading = await screen.findByRole("heading", { name: "Codex" });
    const section = heading.closest("section");
    expect(section).not.toBeNull();
    expect(within(section as HTMLElement).getByText("本机默认 · 跟随 Codex")).toBeInTheDocument();
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

    await openAdminArea(user, "对话后端");
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
      .getByRole("heading", { name: "Claude" })
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
