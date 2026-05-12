import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
const { navigateToLocalPathMock } = vi.hoisted(() => ({
  navigateToLocalPathMock: vi.fn(),
}));

vi.mock("../lib/navigation", () => ({
  navigateToLocalPath: navigateToLocalPathMock,
}));

import { SetupRoute } from "./SetupRoute";
import {
  makeApp,
  makeBootstrap,
  makeOnboardingStage,
  makeOnboardingWorkflow,
  makeRuntimeRequirementsDetect,
} from "../test/fixtures";
import { installMockFetch } from "../test/http";

describe("SetupRoute", () => {
  afterEach(() => {
    vi.useRealTimers();
    navigateToLocalPathMock.mockReset();
  });

  it("keeps local API requests dot-relative when mounted under a prefixed path", async () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    const { calls } = installMockFetch({
      "/g/demo/api/setup/bootstrap-state": {
        body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }),
      },
      "/g/demo/api/setup/onboarding/workflow": {
        body: buildConnectWorkflow([]),
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
    expect(calls.length).toBeGreaterThan(0);
    expect(calls.every((call) => call.rawURL.startsWith("./"))).toBe(true);
  });

  it("connects manually, publishes auto-config changes, and stays in auto-config while review is pending", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();

    let workflowState = buildConnectWorkflow([]);
    let appCreated = false;
    const app = makeApp({
      id: "bot-manual",
      name: "团队机器人",
      appId: "cli_manual",
      verifiedAt: "2026-04-25T08:10:00Z",
    });

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/onboarding/workflow": () => ({ body: workflowState }),
      "/api/setup/onboarding/workflow?app=bot-manual": () => ({ body: workflowState }),
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
      "/api/setup/feishu/apps": (call) => {
        if (call.method === "POST") {
          appCreated = true;
          return {
            status: 201,
            body: { app },
          };
        }
        return { body: { apps: appCreated ? [app] : [] } };
      },
      "/api/setup/feishu/apps/bot-manual/verify": () => {
        workflowState = buildAutoConfigWorkflow(app, {
          status: "apply_required",
          summary: "存在待写入的飞书自动配置差异。",
          stageStatus: "pending",
          allowedActions: ["apply", "retry", "defer"],
        });
        return {
          body: {
            app,
            result: { connected: true, duration: 1_000_000_000 },
          },
        };
      },
      "/api/setup/feishu/apps/bot-manual/auto-config/apply": () => {
        workflowState = buildAutoConfigWorkflow(app, {
          status: "publish_required",
          summary: "配置已收敛到待发布版本，仍需提交发布。",
          stageStatus: "pending",
          allowedActions: ["publish", "retry"],
        });
        return {
          body: {
            app,
            result: {
              status: "publish_required",
              summary: "配置已收敛到待发布版本，仍需提交发布。",
              blockingReason: "",
              actions: [],
              plan: workflowState.app?.autoConfig.plan,
            },
          },
        };
      },
      "/api/setup/feishu/apps/bot-manual/auto-config/publish": () => {
        workflowState = buildAutoConfigWorkflow(app, {
          status: "awaiting_review",
          summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
          stageStatus: "blocked",
          allowedActions: ["retry"],
        });
        return {
          body: {
            app,
            result: {
              status: "awaiting_review",
              summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
              blockingReason: "",
              versionId: "oav_1",
              version: "1.8.1",
              actions: [],
              plan: {
                ...workflowState.app?.autoConfig.plan,
                status: "awaiting_review",
                summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
              },
            },
          },
        };
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "手动输入" }));
    await user.type(screen.getByLabelText("机器人名称（可选）"), "团队机器人");
    await user.type(screen.getByLabelText("App ID"), "cli_manual");
    await user.type(screen.getByLabelText("App Secret"), "secret_manual");
    await user.click(screen.getByRole("button", { name: "验证并继续" }));

    expect(await screen.findByRole("heading", { name: "飞书自动配置" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "自动补齐" }));

    expect(await screen.findByRole("button", { name: "继续发布" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "继续发布" }));
    expect(await screen.findByRole("dialog", { name: "确认提交发布" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "确认提交" }));
    expect(
      await screen.findByRole("heading", { name: "已提交发布，正在等待管理员处理" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "先按降级继续" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "菜单确认" })).not.toBeInTheDocument();
  });

  it("allows deferring optional auto-config work and continues to menu", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();

    const app = makeApp({
      id: "bot-manual",
      name: "团队机器人",
      appId: "cli_manual",
      verifiedAt: "2026-04-25T08:10:00Z",
    });
    let workflowState = buildAutoConfigWorkflow(app, {
      status: "apply_required",
      summary: "存在待写入的飞书自动配置差异。",
      stageStatus: "pending",
      allowedActions: ["apply", "retry", "defer"],
    });

    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/onboarding/workflow": { body: workflowState },
      "/api/setup/onboarding/workflow?app=bot-manual": () => ({ body: workflowState }),
      "/api/setup/feishu/apps/bot-manual/onboarding-auto-config/defer": () => {
        workflowState = buildMenuWorkflow(app);
        return { status: 204 };
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "飞书自动配置" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "先按降级继续" }));

    await waitFor(() => {
      expect(
        calls.some(
          (call) =>
            call.path === "/api/setup/feishu/apps/bot-manual/onboarding-auto-config/defer" &&
            call.method === "POST",
        ),
      ).toBe(true);
    });
    expect(await screen.findByRole("heading", { name: "菜单确认" })).toBeInTheDocument();
  });

  it("starts qr onboarding automatically, polls every 5 seconds, and advances to auto-config", async () => {
    window.history.replaceState({}, "", "/setup");
    let workflowState = buildConnectWorkflow([]);
    const app = makeApp({
      id: "bot-qr",
      name: "扫码机器人",
      appId: "cli_qr",
      verifiedAt: "2026-04-25T08:20:00Z",
    });

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/onboarding/workflow": () => ({ body: workflowState }),
      "/api/setup/onboarding/workflow?app=bot-qr": () => ({ body: workflowState }),
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
        workflowState = buildAutoConfigWorkflow(app, {
          status: "apply_required",
          summary: "存在待写入的飞书自动配置差异。",
          stageStatus: "pending",
          allowedActions: ["apply", "retry", "defer"],
        });
        return {
          body: {
            app,
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
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "飞书自动配置" }, { timeout: 7_000 }),
    ).toBeInTheDocument();
  }, 10_000);

  it("summarizes blocking backend failures with user-facing setup actions", async () => {
    window.history.replaceState({}, "", "/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/onboarding/workflow": {
        body: makeOnboardingWorkflow({
          currentStage: "runtime_requirements",
          runtimeRequirements: makeRuntimeRequirementsDetect({
            ready: false,
            summary: "当前机器还不满足基础运行条件，请先保证 Claude 或 Codex 至少一个可用。",
            checks: [
              {
                id: "headless_launcher",
                title: "服务启动器",
                status: "pass",
                summary: "当前服务已经有可用的 codex-remote 启动器。",
              },
              {
                id: "real_codex_binary",
                title: "Codex 可执行文件",
                status: "fail",
                summary: "当前服务环境下无法解析 Codex 可执行文件。",
              },
              {
                id: "claude_binary",
                title: "Claude 可执行文件",
                status: "fail",
                summary: "当前服务环境下无法解析 Claude 可执行文件。",
              },
            ],
          }),
          app: null,
          stages: [
            makeOnboardingStage({
              id: "runtime_requirements",
              title: "环境检查",
              status: "blocked",
              summary: "当前机器还不满足基础运行条件，请先保证 Claude 或 Codex 至少一个可用。",
              blocking: true,
              allowedActions: ["retry"],
            }),
            makeOnboardingStage({
              id: "connect",
              title: "飞书连接",
              status: "blocked",
              summary: "还没有接入可用的飞书应用。",
              blocking: true,
            }),
          ],
        }),
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByText("当前需要处理")).toBeInTheDocument();
    expect(screen.getByText("对话后端")).toBeInTheDocument();
    expect(screen.getByText("请先保证 Claude 或 Codex 至少一个可用。")).toBeInTheDocument();
    expect(screen.queryByText("Codex 可执行文件")).not.toBeInTheDocument();
    expect(screen.queryByText("当前服务环境下无法解析 Codex 可执行文件。")).not.toBeInTheDocument();
  });

  it("posts setup complete from the done step", async () => {
    window.history.replaceState({}, "", "/setup");
    const user = userEvent.setup();

    const { calls } = installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/onboarding/workflow": {
        body: makeOnboardingWorkflow({
          currentStage: "done",
          completion: {
            setupRequired: false,
            canComplete: true,
            summary: "当前 setup 已可完成。",
          },
          app: {
            app: { verifiedAt: "2026-04-25T08:10:00Z" },
            autoConfig: {
              ...makeOnboardingStage({
                id: "auto_config",
                title: "飞书自动配置",
                status: "complete",
                summary: "飞书应用配置已收敛。",
              }),
              plan: {
                status: "clean",
                summary: "飞书应用配置已收敛。",
                current: {},
                target: {
                  scopeRequirements: [],
                  events: [],
                  callbacks: [],
                  policy: {
                    eventSubscriptionType: "long_polling",
                    eventRequestUrl: "",
                    callbackType: "long_polling",
                    callbackRequestUrl: "",
                    messageCardCallbackUrl: "",
                    encryptionKeyRequired: false,
                    verificationTokenRequired: false,
                    botEnabled: true,
                    mobileDefaultAbility: "messages",
                    pcDefaultAbility: "messages",
                  },
                },
                diff: {
                  configPatchRequired: false,
                  abilityPatchRequired: false,
                  missingScopes: [],
                  extraScopes: [],
                  missingEvents: [],
                  extraEvents: [],
                  missingCallbacks: [],
                  extraCallbacks: [],
                  eventSubscriptionTypeMismatch: false,
                  eventRequestUrlMismatch: false,
                  callbackTypeMismatch: false,
                  callbackRequestUrlMismatch: false,
                  publishRequired: false,
                },
                publish: {
                  needsPublish: false,
                  awaitingReview: false,
                },
                blockingRequirements: [],
                degradableRequirements: [],
              },
            },
            menu: makeOnboardingStage({
              id: "menu",
              title: "菜单确认",
              status: "complete",
              summary: "你已确认机器人菜单配置完成。",
            }),
          },
          stages: [
            makeOnboardingStage({
              id: "runtime_requirements",
              title: "环境检查",
              status: "complete",
              summary: "当前机器已满足基础运行条件。",
              blocking: false,
            }),
            makeOnboardingStage({
              id: "connect",
              title: "飞书连接",
              status: "complete",
              summary: "当前飞书应用连接验证已通过。",
              blocking: false,
            }),
            makeOnboardingStage({
              id: "auto_config",
              title: "飞书自动配置",
              status: "complete",
              summary: "飞书应用配置已收敛。",
              blocking: false,
            }),
            makeOnboardingStage({
              id: "menu",
              title: "菜单确认",
              status: "complete",
              summary: "你已确认机器人菜单配置完成。",
              blocking: false,
            }),
            makeOnboardingStage({
              id: "autostart",
              title: "自动启动",
              status: "deferred",
              summary: "你选择稍后再处理自动启动。",
              blocking: false,
            }),
            makeOnboardingStage({
              id: "vscode",
              title: "VS Code 集成",
              status: "deferred",
              summary: "你选择稍后再处理 VS Code 集成。",
              blocking: false,
            }),
            makeOnboardingStage({
              id: "done",
              title: "完成",
              status: "complete",
              summary: "当前 setup 已经可以完成。",
              blocking: false,
            }),
          ],
        }),
      },
      "/api/setup/complete": {
        body: {
          setupRequired: false,
          adminURL: "http://127.0.0.1:9501/admin/",
          message: "ok",
        },
      },
    });

    render(<SetupRoute />);

    expect(await screen.findByRole("heading", { name: "欢迎使用" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "进入管理页面" }));

    await waitFor(() => {
      expect(
        calls.some(
          (call) => call.path === "/api/setup/complete" && call.method === "POST",
        ),
      ).toBe(true);
    });
    expect(navigateToLocalPathMock).toHaveBeenCalledWith("./admin/");
  });
});

function buildConnectWorkflow(apps: ReturnType<typeof makeApp>[]) {
  return makeOnboardingWorkflow({
    app: null,
    apps,
    currentStage: "connect",
    completion: {
      setupRequired: true,
      canComplete: false,
      blockingReason: "还没有接入可用的飞书应用。",
    },
    stages: [
      makeOnboardingStage({
        id: "runtime_requirements",
        title: "环境检查",
        status: "complete",
        summary: "当前机器已满足基础运行条件，可以继续后面的可选配置。",
        blocking: false,
        allowedActions: ["retry"],
      }),
      makeOnboardingStage({
        id: "connect",
        title: "飞书连接",
        status: "blocked",
        summary: "还没有接入可用的飞书应用。",
        blocking: true,
        allowedActions: ["start_qr", "submit_manual"],
      }),
    ],
  });
}

function buildAutoConfigWorkflow(
  app: ReturnType<typeof makeApp>,
  options: {
    status: string;
    summary: string;
    stageStatus: string;
    allowedActions: string[];
  },
) {
  return makeOnboardingWorkflow({
    currentStage: "auto_config",
    app: {
      app,
      connection: {
        status: "complete",
        summary: "当前飞书应用连接验证已通过。",
        allowedActions: ["verify"],
      },
      autoConfig: {
        status: options.stageStatus,
        summary: options.summary,
        allowedActions: options.allowedActions,
        plan: {
          status: options.status,
          summary: options.summary,
          blockingReason: "",
          blockingRequirements: [],
          degradableRequirements: [
            {
              kind: "scope",
              key: "im:message:send_as_bot",
              feature: "core_message_flow",
              required: false,
              present: false,
              degradeMessage: "机器人可能无法主动回消息。",
            },
          ],
          current: {},
          target: {
            scopeRequirements: [],
            events: [],
            callbacks: [],
            policy: {
              eventSubscriptionType: "long_polling",
              eventRequestUrl: "",
              callbackType: "long_polling",
              callbackRequestUrl: "",
              messageCardCallbackUrl: "",
              encryptionKeyRequired: false,
              verificationTokenRequired: false,
              botEnabled: true,
              mobileDefaultAbility: "messages",
              pcDefaultAbility: "messages",
            },
          },
          diff: {
            configPatchRequired: options.status === "apply_required",
            abilityPatchRequired: false,
            missingScopes: [],
            extraScopes: [],
            missingEvents: [],
            extraEvents: [],
            missingCallbacks: [],
            extraCallbacks: [],
            eventSubscriptionTypeMismatch: false,
            eventRequestUrlMismatch: false,
            callbackTypeMismatch: false,
            callbackRequestUrlMismatch: false,
            publishRequired: options.status === "publish_required",
          },
          publish: {
            needsPublish: options.status === "publish_required",
            awaitingReview: options.status === "awaiting_review",
          },
        },
      },
      menu: makeOnboardingStage({
        id: "menu",
        title: "菜单确认",
        status: "blocked",
        summary: "请先完成飞书自动配置。",
        blocking: true,
      }),
    },
    completion: {
      setupRequired: true,
      canComplete: false,
      blockingReason: options.summary,
    },
    stages: [
      makeOnboardingStage({
        id: "runtime_requirements",
        title: "环境检查",
        status: "complete",
        summary: "当前机器已满足基础运行条件。",
        blocking: false,
      }),
      makeOnboardingStage({
        id: "connect",
        title: "飞书连接",
        status: "complete",
        summary: "当前飞书应用连接验证已通过。",
        blocking: false,
      }),
      makeOnboardingStage({
        id: "auto_config",
        title: "飞书自动配置",
        status: options.stageStatus,
        summary: options.summary,
        blocking: false,
        allowedActions: options.allowedActions,
      }),
      makeOnboardingStage({
        id: "menu",
        title: "菜单确认",
        status: "blocked",
        summary: "请先完成飞书自动配置。",
        blocking: true,
      }),
      makeOnboardingStage({
        id: "autostart",
        title: "自动启动",
        status: "pending",
        summary: "当前还没有完成自动启动决策。",
        blocking: false,
      }),
      makeOnboardingStage({
        id: "vscode",
        title: "VS Code 集成",
        status: "pending",
        summary: "当前还没有完成 VS Code 集成决策。",
        blocking: false,
      }),
    ],
  });
}

function buildMenuWorkflow(app: ReturnType<typeof makeApp>) {
  return makeOnboardingWorkflow({
    currentStage: "menu",
    app: {
      app,
      connection: {
        status: "complete",
        summary: "当前飞书应用连接验证已通过。",
      },
      autoConfig: {
        status: "deferred",
        summary: "你已选择先按降级继续，后续仍可回到这里重新查看审核结果。",
        allowedActions: ["retry"],
        plan: {
          status: "awaiting_review",
          summary: "飞书应用变更已进入审核流程，正在等待审核结果。",
          blockingReason: "",
          blockingRequirements: [],
          degradableRequirements: [
            {
              kind: "scope",
              key: "im:message:send_as_bot",
              feature: "core_message_flow",
              required: false,
              present: false,
              degradeMessage: "机器人可能无法主动回消息。",
            },
          ],
          current: {},
          target: {
            scopeRequirements: [],
            events: [],
            callbacks: [],
            policy: {
              eventSubscriptionType: "long_polling",
              eventRequestUrl: "",
              callbackType: "long_polling",
              callbackRequestUrl: "",
              messageCardCallbackUrl: "",
              encryptionKeyRequired: false,
              verificationTokenRequired: false,
              botEnabled: true,
              mobileDefaultAbility: "messages",
              pcDefaultAbility: "messages",
            },
          },
          diff: {
            configPatchRequired: false,
            abilityPatchRequired: false,
            missingScopes: [],
            extraScopes: [],
            missingEvents: [],
            extraEvents: [],
            missingCallbacks: [],
            extraCallbacks: [],
            eventSubscriptionTypeMismatch: false,
            eventRequestUrlMismatch: false,
            callbackTypeMismatch: false,
            callbackRequestUrlMismatch: false,
            publishRequired: false,
          },
          publish: {
            needsPublish: false,
            awaitingReview: true,
          },
        },
      },
      menu: makeOnboardingStage({
        id: "menu",
        title: "菜单确认",
        status: "pending",
        summary: "请在飞书后台确认机器人菜单配置完成，然后回到这里继续。",
        blocking: false,
        allowedActions: ["open_bot", "confirm"],
      }),
    },
    completion: {
      setupRequired: true,
      canComplete: false,
      blockingReason: "还没有确认机器人菜单配置。",
    },
    stages: [
      makeOnboardingStage({
        id: "runtime_requirements",
        title: "环境检查",
        status: "complete",
        summary: "当前机器已满足基础运行条件。",
        blocking: false,
      }),
      makeOnboardingStage({
        id: "connect",
        title: "飞书连接",
        status: "complete",
        summary: "当前飞书应用连接验证已通过。",
        blocking: false,
      }),
      makeOnboardingStage({
        id: "auto_config",
        title: "飞书自动配置",
        status: "deferred",
        summary: "你已选择先按降级继续，后续仍可回到这里重新查看审核结果。",
        blocking: false,
      }),
      makeOnboardingStage({
        id: "menu",
        title: "菜单确认",
        status: "pending",
        summary: "请在飞书后台确认机器人菜单配置完成，然后回到这里继续。",
        blocking: false,
        allowedActions: ["open_bot", "confirm"],
      }),
      makeOnboardingStage({
        id: "autostart",
        title: "自动启动",
        status: "pending",
        summary: "当前还没有完成自动启动决策。",
        blocking: false,
      }),
      makeOnboardingStage({
        id: "vscode",
        title: "VS Code 集成",
        status: "pending",
        summary: "当前还没有完成 VS Code 集成决策。",
        blocking: false,
      }),
    ],
  });
}
