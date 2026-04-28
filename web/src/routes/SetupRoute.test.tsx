import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SetupRoute } from "./SetupRoute";
import {
  makeApp,
  makeBootstrap,
  makeFeishuManifest,
  makeOnboardingStage,
  makeOnboardingWorkflow,
} from "../test/fixtures";
import { installMockFetch } from "../test/http";

describe("SetupRoute", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("keeps local workflow API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    const { calls } = installMockFetch({
      "/g/demo/api/setup/bootstrap-state": {
        body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }),
      },
      "/g/demo/api/setup/feishu/manifest": {
        body: makeFeishuManifest(),
      },
      "/g/demo/api/setup/onboarding/workflow": {
        body: makeConnectWorkflow(),
      },
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
    expect(calls.some((call) => call.path === "/g/demo/api/setup/onboarding/workflow")).toBe(
      true,
    );
    expect(
      calls.some((call) =>
        call.path.endsWith("/runtime-requirements/detect") ||
        call.path.endsWith("/autostart/detect") ||
        call.path.endsWith("/vscode/detect"),
      ),
    ).toBe(false);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
  });

  it("connects manually, refreshes workflow, and no longer uses standalone permission read endpoints", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    let workflowReads = 0;
    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/onboarding/workflow": () => {
        workflowReads += 1;
        switch (workflowReads) {
          case 1:
            return { body: makeConnectWorkflow() };
          case 2:
            return {
              body: makeOnboardingWorkflow({
                currentStage: "permission",
                app: {
                  app: {
                    id: "bot-manual",
                    name: "团队机器人",
                    appId: "cli_manual",
                    verifiedAt: "2026-04-25T08:10:00Z",
                  },
                },
              }),
            };
          default:
            return {
              body: makeOnboardingWorkflow({
                currentStage: "events",
                app: {
                  app: {
                    id: "bot-manual",
                    name: "团队机器人",
                    appId: "cli_manual",
                    verifiedAt: "2026-04-25T08:10:00Z",
                  },
                  permission: {
                    status: "complete",
                    summary: "当前基础权限已经齐全。",
                    missingScopes: [],
                    grantJSON: "",
                  },
                  events: {
                    status: "pending",
                  },
                },
                guide: {
                  remainingManualActions: [
                    "完成一次事件订阅联调。",
                    "完成一次回调联调。",
                    "确认飞书应用菜单已经配置。",
                  ],
                },
              }),
            };
        }
      },
      "/api/setup/feishu/apps": {
        status: 201,
        body: {
          app: makeApp({
            id: "bot-manual",
            name: "团队机器人",
            appId: "cli_manual",
          }),
        },
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
      "/api/setup/feishu/apps/bot-manual/test-events": {
        body: {
          gatewayId: "bot-manual",
          startedAt: "2026-04-25T08:12:00Z",
          expiresAt: "2026-04-25T08:22:00Z",
          phrase: "测试",
          message: "事件订阅测试提示已发送。",
        },
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "手动输入" }));
    await user.type(screen.getByLabelText("机器人名称（可选）"), "团队机器人");
    await user.type(screen.getByLabelText("App ID"), "cli_manual");
    await user.type(screen.getByLabelText("App Secret"), "secret_manual");
    await user.click(screen.getByRole("button", { name: "验证并继续" }));

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    expect(
      await screen.findByText(
        "如果当前企业权限暂时申请不到，你也可以先跳过这一步，后面再回来补齐。",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "检查并继续" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "强制跳过这一步" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "检查并继续" }));
    expect(await screen.findByRole("heading", { name: "事件订阅" })).toBeInTheDocument();
    expect(await screen.findByText("事件订阅测试提示已发送。")).toBeInTheDocument();
    expect(calls.some((call) => call.path.includes("/permission-check"))).toBe(false);
  });

  it("supports force-skipping the permission step and resetting it on recheck", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    let workflowReads = 0;
    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/onboarding/workflow": () => {
        workflowReads += 1;
        switch (workflowReads) {
          case 1:
            return {
              body: makeOnboardingWorkflow({
                currentStage: "permission",
              }),
            };
          case 2:
            return {
              body: makeOnboardingWorkflow({
                currentStage: "events",
                app: {
                  permission: {
                    status: "deferred",
                    summary: "你已选择先跳过这一步，后续仍可回到这里重新检查。",
                    allowedActions: ["open_auth", "recheck"],
                  },
                },
                guide: {
                  remainingManualActions: [
                    "完成一次事件订阅联调。",
                    "完成一次回调联调。",
                    "确认飞书应用菜单已经配置。",
                  ],
                },
              }),
            };
          default:
            return {
              body: makeOnboardingWorkflow({
                currentStage: "permission",
              }),
            };
        }
      },
      "/api/setup/feishu/apps/bot-1/onboarding-permission/skip": {
        status: 200,
        body: {},
      },
      "/api/setup/feishu/apps/bot-1/onboarding-permission/reset": {
        status: 200,
        body: {},
      },
      "/api/setup/feishu/apps/bot-1/test-events": {
        body: {
          gatewayId: "bot-1",
          startedAt: "2026-04-25T08:12:00Z",
          expiresAt: "2026-04-25T08:22:00Z",
          phrase: "测试",
          message: "事件订阅测试提示已发送。",
        },
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "强制跳过这一步" }));

    expect(await screen.findByRole("heading", { name: "事件订阅" })).toBeInTheDocument();
    expect(await screen.findByText("已跳过这一步，你可以继续后面的设置。")).toBeInTheDocument();
    expect(
      calls.some((call) => call.path === "/api/setup/feishu/apps/bot-1/onboarding-permission/skip"),
    ).toBe(true);

    const rail = screen.getByText("设置流程").closest("aside");
    expect(rail).not.toBeNull();
    await user.click(
      within(rail as HTMLElement).getByRole("button", {
        name: /权限检查/,
      }),
    );
    await user.click(screen.getByRole("button", { name: "重新检查" }));

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    expect(
      calls.some((call) => call.path === "/api/setup/feishu/apps/bot-1/onboarding-permission/reset"),
    ).toBe(true);
    expect(screen.getByRole("button", { name: "检查并继续" })).toBeInTheDocument();
  });

  it("starts qr onboarding automatically, polls, and advances according to refreshed workflow", async () => {
    window.history.replaceState({}, "", "/setup");
    let workflowReads = 0;

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/onboarding/workflow": () => {
        workflowReads += 1;
        if (workflowReads === 1) {
          return { body: makeConnectWorkflow() };
        }
        return {
          body: makeOnboardingWorkflow({
            currentStage: "permission",
            app: {
              app: {
                id: "bot-qr",
                name: "扫码机器人",
                appId: "cli_qr",
                verifiedAt: "2026-04-25T08:20:00Z",
              },
            },
          }),
        };
      },
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
      "/api/setup/feishu/onboarding/sessions/session-qr/complete": {
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
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "权限检查" }, { timeout: 7_000 }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("飞书应用连接成功，已进入下一步。这一步只验证当前凭证可连接。"),
    ).toBeInTheDocument();
  }, 10_000);

  it("shows completion CTA when setup can finish even if optional pending items remain", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    const assign = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({
      ...window.location,
      assign,
    } as Location);
    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/onboarding/workflow": {
        body: makeOnboardingWorkflow({
          currentStage: "permission",
          completion: {
            setupRequired: false,
            canComplete: true,
            summary: "当前 setup 已可完成，你也可以先继续处理建议补齐项。",
          },
          autostart: {
            status: "deferred",
            summary: "你选择稍后再处理自动启动。",
          },
          vscode: {
            status: "deferred",
            summary: "你选择稍后再处理 VS Code 集成。",
          },
          guide: {
            remainingManualActions: [
              "补齐基础权限并重新检查。",
              "完成一次事件订阅联调。",
            ],
          },
        }),
      },
      "/api/setup/complete": {
        body: {
          setupRequired: false,
          adminURL: "/admin/",
          message: "setup access disabled; continue in the local admin page",
        },
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "权限检查" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "完成设置并进入管理页面" }),
    ).toBeInTheDocument();
    expect(screen.getByText("补齐基础权限并重新检查。")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "完成设置并进入管理页面" }));

    expect(calls.some((call) => call.path === "/api/setup/complete")).toBe(true);
    expect(assign).toHaveBeenCalledWith("./admin/");
  });

  it("records deferred autostart decisions through the onboarding workflow endpoint", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();
    let workflowReads = 0;

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/manifest": { body: makeFeishuManifest() },
      "/api/setup/onboarding/workflow": () => {
        workflowReads += 1;
        if (workflowReads === 1) {
          return {
            body: makeOnboardingWorkflow({
              currentStage: "autostart",
            }),
          };
        }
        return {
          body: makeOnboardingWorkflow({
            currentStage: "vscode",
            autostart: {
              status: "deferred",
              summary: "你选择稍后再处理自动启动。",
              allowedActions: ["apply", "record_enabled"],
              decision: {
                value: "deferred",
                decidedAt: "2026-04-25T08:20:00Z",
              },
            },
            guide: {
              remainingManualActions: ["决定如何处理这台机器上的 VS Code 集成。"],
            },
          }),
        };
      },
      "/api/setup/onboarding/machine-decisions/autostart": {
        status: 200,
        body: {},
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "自动启动" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "稍后处理" }));

    expect(await screen.findByRole("heading", { name: "VS Code 集成" })).toBeInTheDocument();
    expect(await screen.findByText("自动启动已留待稍后处理。")).toBeInTheDocument();
  });

  it("lands directly on an existing app's current pending stage and still supports copying event names", async () => {
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
      "/api/setup/onboarding/workflow": {
        body: makeOnboardingWorkflow({
          currentStage: "events",
          app: {
            permission: {
              status: "complete",
              summary: "当前基础权限已经齐全。",
              missingScopes: [],
              grantJSON: "",
            },
          },
        }),
      },
      "/api/setup/feishu/apps/bot-1/test-events": {
        body: {
          gatewayId: "bot-1",
          startedAt: "2026-04-25T08:12:00Z",
          expiresAt: "2026-04-25T08:22:00Z",
          phrase: "测试",
          message: "事件订阅测试提示已发送。",
        },
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "事件订阅" })).toBeInTheDocument();
    expect(await screen.findByText("事件订阅测试提示已发送。")).toBeInTheDocument();

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
});

function makeConnectWorkflow() {
  const runtimeStage = makeOnboardingStage({
    id: "runtime_requirements",
    title: "环境检查",
    status: "complete",
    summary: "当前机器已满足基础运行条件，可以继续后面的可选配置。",
    blocking: false,
    allowedActions: ["retry"],
  });
  const connectStage = makeOnboardingStage({
    id: "connect",
    title: "飞书连接",
    status: "blocked",
    summary: "还没有接入可用的飞书应用。",
    blocking: true,
    allowedActions: ["start_qr", "submit_manual"],
  });
  const blockedOptional = (id: "permission" | "events" | "callback" | "menu", title: string) =>
    makeOnboardingStage({
      id,
      title,
      status: "blocked",
      summary: "请先完成基础接入。",
      blocking: false,
      optional: true,
    });

  return makeOnboardingWorkflow({
    apps: [],
    app: null,
    selectedAppId: "",
    currentStage: "connect",
    machineState: "blocked",
    completion: {
      setupRequired: true,
      canComplete: false,
      summary: "当前 setup 还不能完成，请先处理阻塞项。",
      blockingReason: "还没有完成飞书连接验证。",
    },
    guide: {
      autoConfiguredSummary: "请先让这台机器和一个可用飞书应用进入可继续联调的状态。",
      remainingManualActions: ["接入并验证一个可用的飞书应用。"],
      recommendedNextStep: "connect",
    },
    stages: [
      runtimeStage,
      connectStage,
      makeOnboardingStage({
        id: "permission",
        title: "权限检查",
        status: "blocked",
        summary: "请先完成连接验证。",
        blocking: true,
      }),
      blockedOptional("events", "事件订阅"),
      blockedOptional("callback", "回调配置"),
      blockedOptional("menu", "菜单确认"),
      makeOnboardingStage({
        id: "autostart",
        title: "自动启动",
        status: "pending",
        summary: "当前还没有完成自动启动决策。",
        optional: true,
        blocking: false,
        allowedActions: ["apply", "defer"],
      }),
      makeOnboardingStage({
        id: "vscode",
        title: "VS Code 集成",
        status: "pending",
        summary: "当前还没有完成 VS Code 集成决策。",
        optional: true,
        blocking: false,
        allowedActions: ["apply", "defer", "remote_only"],
      }),
    ],
  });
}
