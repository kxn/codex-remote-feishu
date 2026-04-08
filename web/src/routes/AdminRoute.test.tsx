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

    await user.click(screen.getByRole("radio", { name: /这台机器本地要用，也会 SSH 到别的机器/ }));
    await user.click(screen.getByRole("button", { name: "在这台机器上启用 VS Code" }));

    expect(await screen.findByText(/已接管这台机器上的 VS Code 扩展入口/)).toBeInTheDocument();
  });
});
