import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { AdminRoute } from "./AdminRoute";
import {
  makeApp,
  makeBootstrap,
  makeImageStagingStatus,
  makeManifest,
  makePreviewDriveStatus,
  makeRuntimeStatus,
  makeVSCodeDetect,
} from "../test/fixtures";
import { installMockFetch } from "../test/http";

describe("AdminRoute", () => {
  it("toggles the shell section navigation and closes it after selecting a section", async () => {
    const user = userEvent.setup();
    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": { body: { apps: [makeApp()] } },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
    });

    render(<AdminRoute />);

    const toggle = screen.getByRole("button", { name: "打开分区导航" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");

    await user.click(screen.getByRole("link", { name: "飞书机器人" }));
    expect(toggle).toHaveAttribute("aria-expanded", "false");
  });

  it("shows the admin error state when bootstrap loading fails", async () => {
    installMockFetch({
      "/api/admin/bootstrap-state": {
        status: 500,
        body: { error: { message: "bootstrap load failed" } },
      },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": { body: { apps: [] } },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
    });

    render(<AdminRoute />);

    expect(screen.getByText("正在读取最新状态")).toBeInTheDocument();
    expect(await screen.findByText("无法加载管理页状态")).toBeInTheDocument();
    expect(screen.getByText("bootstrap load failed")).toBeInTheDocument();
  });

  it("shows read-only app state and disables save controls", async () => {
    const app = makeApp({
      id: "bot-readonly",
      name: "Readonly Bot",
      readOnly: true,
      readOnlyReason: "当前由启动参数接管，只能查看状态，不能在管理页修改。",
    });

    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": { body: { apps: [app] } },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-readonly": {
        body: makePreviewDriveStatus({ gatewayId: "bot-readonly", name: "Readonly Bot" }),
      },
    });

    render(<AdminRoute />);

    expect(await screen.findAllByText("当前由启动参数接管，只能查看状态，不能在管理页修改。")).not.toHaveLength(0);
    expect(screen.getByLabelText("机器人名称")).toBeDisabled();
    expect(screen.getByRole("button", { name: "保存更改" })).toBeDisabled();
  });

  it("reloads and shows pending runtime apply state after saved-but-not-applied error", async () => {
    let appListCalls = 0;
    const user = userEvent.setup();
    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": () => {
        appListCalls += 1;
        if (appListCalls === 1) {
          return { body: { apps: [makeApp()] } };
        }
        return {
          body: {
            apps: [
              makeApp({
                runtimeApply: {
                  pending: true,
                  action: "upsert",
                  error: "dial tcp 127.0.0.1:443: connect refused",
                  retryAvailable: true,
                },
              }),
            ],
          },
        };
      },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
      "/api/admin/feishu/apps/bot-1": {
        status: 500,
        body: {
          error: {
            code: "gateway_apply_failed",
            message: "feishu config saved but runtime apply failed",
            retryable: true,
            details: {
              gatewayId: "bot-1",
              app: makeApp({
                runtimeApply: {
                  pending: true,
                  action: "upsert",
                  error: "dial tcp 127.0.0.1:443: connect refused",
                  retryAvailable: true,
                },
              }),
            },
          },
        },
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByRole("button", { name: "保存更改" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "保存更改" }));

    expect(await screen.findByText("更改已保存到本地配置，但运行时还没应用成功。页面已刷新为“未生效”状态，请重试应用。")).toBeInTheDocument();
    expect(screen.getAllByText("未生效").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "重试应用" })).toBeInTheDocument();
  });

  it("shows existing-app manual connect flow when adding a new bot from admin", async () => {
    const user = userEvent.setup();
    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": { body: { apps: [makeApp()] } },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByRole("button", { name: "新增机器人" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "新增机器人" }));
    await user.click(screen.getByRole("button", { name: "下一步" }));

    expect(await screen.findByText("已有应用怎么接")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存并验证" })).toBeInTheDocument();
  });

  it("creates a new admin bot through qr onboarding", async () => {
    const user = userEvent.setup();
    let appListCalls = 0;
    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": () => {
        appListCalls += 1;
        if (appListCalls === 1) {
          return { body: { apps: [makeApp()] } };
        }
        return {
          body: {
            apps: [
              makeApp(),
              makeApp({
                id: "bot-qr",
                name: "扫码 Bot",
                appId: "cli_qr",
                wizard: {
                  credentialsSavedAt: "2026-04-10T09:00:00Z",
                  connectionVerifiedAt: "2026-04-10T09:00:05Z",
                },
              }),
            ],
          },
        };
      },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
      "/api/admin/storage/preview-drive/bot-qr": {
        body: makePreviewDriveStatus({ gatewayId: "bot-qr", name: "扫码 Bot" }),
      },
      "/api/admin/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "session-admin-1",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
            verificationUrl: "https://example.test/qr",
            pollIntervalSeconds: 2,
          },
        },
      },
      "/api/admin/feishu/onboarding/sessions/session-admin-1": {
        body: {
          session: {
            id: "session-admin-1",
            status: "ready",
            qrCodeDataUrl: "data:image/png;base64,abc",
            verificationUrl: "https://example.test/qr",
            appId: "cli_qr",
            displayName: "扫码 Bot",
            pollIntervalSeconds: 2,
          },
        },
      },
      "/api/admin/feishu/onboarding/sessions/session-admin-1/complete": {
        body: {
          app: makeApp({
            id: "bot-qr",
            name: "扫码 Bot",
            appId: "cli_qr",
            wizard: {
              credentialsSavedAt: "2026-04-10T09:00:00Z",
              connectionVerifiedAt: "2026-04-10T09:00:05Z",
            },
          }),
          result: {
            connected: true,
            duration: 1,
          },
          session: {
            id: "session-admin-1",
            status: "completed",
            appId: "cli_qr",
            displayName: "扫码 Bot",
          },
        },
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByRole("button", { name: "新增机器人" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "新增机器人" }));
    await user.click(screen.getByRole("radio", { name: /新建飞书应用/ }));
    await user.click(screen.getByRole("button", { name: "下一步" }));

    expect(await screen.findByText("扫码创建飞书应用")).toBeInTheDocument();
    expect(await screen.findByText(/页面会每 2 秒自动检查一次扫码结果/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "刷新二维码状态" }));
    expect(await screen.findByText("扫码创建已经完成")).toBeInTheDocument();
    expect(screen.getByText(/drive:drive/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText(/连接测试成功/)).toBeInTheDocument();
  });

  it("applies managed shim for local plus remote ssh usage from the admin panel", async () => {
    const user = userEvent.setup();
    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": { body: { apps: [makeApp()] } },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": {
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
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
      "/api/admin/vscode/apply": (call) => {
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
              matchesBinary: true,
            },
          }),
        };
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByText("你以后主要怎么使用 VS Code 里的 Codex？")).toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /要在当前这台机器上使用/ }));
    await user.click(screen.getByRole("button", { name: "在这台机器上启用 VS Code" }));

    expect(await screen.findByText(/已接管这台机器上的 VS Code 扩展入口/)).toBeInTheDocument();
  });

  it("hides manual managed-instance controls from the admin panel", async () => {
    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": { body: { apps: [makeApp()] } },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByText(/后台恢复实例由系统自动管理/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "新建实例" })).not.toBeInTheDocument();
    expect(screen.queryByText("可由管理页删除")).not.toBeInTheDocument();
  });

  it("does not show preview reconcile controls in the admin panel", async () => {
    installMockFetch({
      "/api/admin/bootstrap-state": { body: makeBootstrap() },
      "/api/admin/runtime-status": { body: makeRuntimeStatus() },
      "/api/admin/feishu/apps": { body: { apps: [makeApp()] } },
      "/api/admin/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/admin/vscode/detect": { body: makeVSCodeDetect() },
      "/api/admin/instances": { body: { instances: [] } },
      "/api/admin/storage/image-staging": { body: makeImageStagingStatus() },
      "/api/admin/storage/preview-drive/bot-1": {
        body: makePreviewDriveStatus({ gatewayId: "bot-1", name: "Main Bot" }),
      },
    });

    render(<AdminRoute />);

    expect(await screen.findByText(/固定的预览 inventory 根目录/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "检查目录一致性" })).not.toBeInTheDocument();
  });
});
