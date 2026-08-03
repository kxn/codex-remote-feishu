import { expect, test, type Page } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await installAdminMocks(page);
});

test("admin core flows work on desktop and mobile", async ({ page }) => {
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: /管理/ })).toBeVisible();

  await openAdminArea(page, "机器人");
  await expect(page.getByRole("heading", { name: "机器人", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "权限机器人" })).toBeVisible();
  await page.getByRole("button", { name: "重新检查配置" }).click();
  await expect(page.getByText("飞书配置还需要补齐。")).toBeVisible();
  await expect(
    page.getByText("权限 im:message.group_msg:readonly"),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "复制导入 JSON" })).toBeVisible();
  await expect(page.getByLabel("权限导入 JSON")).toHaveValue(
    JSON.stringify(
      { scopes: { tenant: ["im:message.group_msg:readonly"], user: [] } },
      null,
      2,
    ),
  );

  await openAdminArea(page, "系统");
  await expect(page.getByRole("heading", { name: "系统", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "自动运行" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "VS Code 集成" })).toBeVisible();
  await page.setViewportSize({ width: 812, height: 393 });
  await expect(page.getByRole("heading", { name: "系统", exact: true })).toBeVisible();
  await page.setViewportSize({ width: 393, height: 812 });
  await expect(page.getByRole("heading", { name: "系统", exact: true })).toBeVisible();

  await openAdminArea(page, "对话后端");
  await page.getByRole("button", { name: "Codex" }).click();

  const codexSection = page
    .locator("section.profile-section")
    .filter({ has: page.getByRole("heading", { name: "Codex", exact: true }) })
    .first();
  await expect(codexSection.getByRole("heading", { name: "Codex", exact: true })).toBeVisible();
  await codexSection.getByRole("button", { name: /新增配置/ }).click();
  await codexSection.getByLabel(/名称/).fill("E2E 配置");
  await codexSection.getByLabel(/端点地址/).fill("https://api.example.com/v1");
  await codexSection.getByLabel(/API Key/).fill("e2e-secret");
  await codexSection.getByLabel("主模型").fill("gpt-5.5");
  await codexSection.getByLabel("推理强度").fill("high");
  await codexSection.getByRole("button", { name: "保存配置" }).click();
  await expect(codexSection.getByText("Codex 配置已创建。")).toBeVisible();
  await expect(codexSection.getByRole("button", { name: /E2E 配置/ })).toBeVisible();
  await codexSection.getByRole("button", { name: "删除配置" }).click();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(codexSection.getByText("Codex 配置已删除。")).toBeVisible();
  await expect(codexSection.getByRole("button", { name: /E2E 配置/ })).toHaveCount(0);

  await codexSection.getByRole("button", { name: /Team Proxy/ }).click();
  await expect(codexSection.getByLabel("上下文大小")).toBeVisible();
  await codexSection.getByLabel("上下文大小").selectOption("price_guard_272k");
  await expect(codexSection.getByText("按费用优先请求 272K；这不是单次请求费用硬上限。")).toBeVisible();
  await codexSection.getByRole("button", { name: "保存修改" }).click();
  await expect(codexSection.getByText("Codex 配置已保存。")).toBeVisible();

  await page.getByRole("button", { name: "Claude" }).click();
  const claudeSection = page
    .locator("section.profile-section")
    .filter({ has: page.getByRole("heading", { name: "Claude", exact: true }) })
    .first();
  await expect(claudeSection.getByRole("heading", { name: "Claude", exact: true })).toBeVisible();
  await claudeSection.getByRole("button", { name: /DevSeek/ }).click();
  await claudeSection.getByLabel("使用 1M 上下文").check();
  await claudeSection.getByRole("button", { name: "保存修改" }).click();
  await expect(claudeSection.getByText("Claude 配置已保存。")).toBeVisible();
});

type CodexProfile = ReturnType<typeof codexProfiles>[number];

async function openAdminArea(page: Page, name: string) {
  await page
    .locator(".side-nav button:visible, .bottom-tabs button:visible")
    .filter({ hasText: name })
    .click();
}

async function installAdminMocks(page: Page) {
  const codexProfileState = codexProfiles();
  await page.route("**/api/admin/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    const method = route.request().method();
    const body = route.request().postDataJSON?.() as Record<string, unknown> | undefined;

    if (path.endsWith("/api/admin/bootstrap-state")) {
      await route.fulfill({ json: bootstrapState() });
      return;
    }
    if (path.endsWith("/api/admin/feishu/onboarding/sessions")) {
      await route.fulfill({
        status: 201,
        json: {
          session: {
            id: "session-admin-e2e",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
          },
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/feishu/apps")) {
      await route.fulfill({ json: { apps: adminApps() } });
      return;
    }
    if (path.endsWith("/api/admin/feishu/apps/e2e-bot/auto-config/plan")) {
      await route.fulfill({
        json: {
          app: adminApps()[0],
          plan: {
            status: "apply_required",
            summary: "飞书配置还需要补齐。",
            blockingReason: "",
            blockingRequirements: [
              {
                kind: "scope",
                key: "im:message.group_msg:readonly",
                scopeType: "tenant",
                feature: "room_admin",
                required: true,
                present: false,
              },
            ],
            degradableRequirements: [],
            current: {
              configuredScopes: [],
              configuredEvents: [],
              botEnabled: true,
            },
            target: {
              scopeRequirements: [],
              events: [],
              callbacks: [],
              policy: {},
            },
            diff: {
              configPatchRequired: true,
              abilityPatchRequired: false,
              missingScopes: [
                { scope: "im:message.group_msg:readonly", scopeType: "tenant" },
              ],
              extraScopes: [],
              missingEvents: [],
              extraEvents: [],
              publishRequired: false,
            },
            publish: {
              needsPublish: false,
              awaitingReview: false,
            },
          },
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/codex/profiles") && method === "GET") {
      await route.fulfill({ json: { profiles: codexProfileState } });
      return;
    }
    if (path.endsWith("/api/admin/codex/profiles") && method === "POST") {
      const profile: CodexProfile = {
        id: "e2e-profile",
        revision: 1,
        etag: '"codex-profile-definition:e2e-profile:1"',
        kind: "api",
        name: String(body?.name ?? ""),
        baseURL: String(body?.baseURL ?? ""),
        model: String(body?.model ?? ""),
        reviewModel: String(body?.reviewModel ?? ""),
        reasoningEffort: String(body?.reasoningEffort ?? ""),
        available: true,
        hasAPIKey: true,
        editable: true,
        deletable: true,
        contextEditable: true,
        contextPreference: {
          profileID: "e2e-profile",
          revision: 1,
          etag: '"codex-context-preference:e2e-profile:1"',
          mode: "codex_default",
        },
      };
      codexProfileState.push(profile);
      await route.fulfill({ status: 201, json: { profile } });
      return;
    }
    if (path.endsWith("/api/admin/codex/profiles/e2e-profile/references")) {
      await route.fulfill({ json: { profileID: "e2e-profile", references: [] } });
      return;
    }
    if (path.endsWith("/api/admin/codex/profiles/e2e-profile") && method === "DELETE") {
      const index = codexProfileState.findIndex((profile) => profile.id === "e2e-profile");
      if (index >= 0) {
        codexProfileState.splice(index, 1);
      }
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    if (path.endsWith("/api/admin/codex/profiles/team-proxy") && method === "PUT") {
      await route.fulfill({
        json: {
          profile: {
            ...codexProfileState.find((profile) => profile.id === "team-proxy"),
            name: body?.name,
            contextPreference: {
              profileID: "team-proxy",
              revision: 1,
              etag: '"codex-context-preference:team-proxy:1"',
              mode: "codex_default",
            },
          },
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/codex/profiles/team-proxy/context-preference")) {
      await route.fulfill({
        json: {
          contextPreference: {
            profileID: "team-proxy",
            revision: 2,
            etag: '"codex-context-preference:team-proxy:2"',
            mode: body?.mode,
          },
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/claude/profiles") && method === "GET") {
      await route.fulfill({ json: { profiles: claudeProfiles() } });
      return;
    }
    if (path.endsWith("/api/admin/claude/profiles/devseek") && method === "PUT") {
      await route.fulfill({
        json: {
          profile: {
            ...claudeProfiles()[1],
            name: body?.name,
          },
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/claude/profiles/devseek/context-preference")) {
      await route.fulfill({
        json: {
          contextPreference: {
            profileID: "devseek",
            revision: 2,
            etag: '"claude-context-preference:devseek:2"',
            mode: body?.mode,
          },
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/autostart/detect")) {
      await route.fulfill({
        json: {
          platform: "linux",
          supported: false,
          status: "unsupported",
          configured: false,
          enabled: false,
          canApply: false,
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/vscode/detect")) {
      await route.fulfill({
        json: {
          sshSession: false,
          recommendedMode: "managed_shim",
          currentMode: "managed_shim",
          currentBinary: "/usr/local/bin/codex",
          installStatePath: "",
          settings: {
            path: "",
            exists: true,
            cliExecutable: "/usr/local/bin/codex",
            matchesBinary: false,
          },
          latestShim: {
            entrypoint: "",
            exists: true,
            realBinaryPath: "/usr/local/bin/codex",
            realBinaryExists: true,
            installed: true,
            matchesBinary: true,
          },
          needsShimReinstall: false,
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/storage/image-staging")) {
      await route.fulfill({
        json: {
          rootDir: "",
          fileCount: 0,
          totalBytes: 0,
          activeFileCount: 0,
          activeBytes: 0,
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/storage/logs")) {
      await route.fulfill({
        json: {
          rootDir: "",
          fileCount: 0,
          totalBytes: 0,
        },
      });
      return;
    }
    if (path.endsWith("/api/admin/storage/preview-drive/e2e-bot")) {
      await route.fulfill({
        json: {
          gatewayId: "e2e-bot",
          name: "权限机器人",
          summary: {
            fileCount: 0,
            scopeCount: 0,
            estimatedBytes: 0,
            unknownSizeFileCount: 0,
          },
        },
      });
      return;
    }
    await route.fulfill({ status: 404, json: { error: { message: `unmocked ${path}` } } });
  });
}

function bootstrapState() {
  return {
    phase: "ready",
    setupRequired: false,
    sshSession: false,
    product: { name: "Codex Remote Feishu", version: "v1.7.0" },
    session: { authenticated: true, trustedLoopback: true },
    config: { path: "", version: 1 },
    relay: { listenHost: "127.0.0.1", listenPort: "9500", serverURL: "ws://127.0.0.1:9500/ws/agent" },
    admin: { listenHost: "127.0.0.1", listenPort: "9501", url: "/admin/", setupURL: "/setup", setupTokenRequired: false },
    feishu: { appCount: 1, enabledAppCount: 1, configuredAppCount: 1, runtimeConfiguredApps: 1 },
    gateways: [],
  };
}

function adminApps() {
  return [
    {
      id: "e2e-bot",
      name: "权限机器人",
      appId: "cli_permission",
      hasSecret: true,
      enabled: true,
      disabled: false,
      readOnly: false,
      runtimeOnly: false,
      verifiedAt: "2026-08-02T06:30:00Z",
      status: {
        id: "e2e-bot",
        state: "connected",
        label: "已连接",
        connected: true,
      },
      onboarding: {},
    },
  ];
}

function codexProfiles() {
  return [
    {
      id: "codex-native",
      kind: "native",
      name: "本机默认",
      available: true,
      editable: false,
      deletable: false,
      contextEditable: true,
      contextPreference: { profileID: "codex-native", revision: 1, etag: '"codex-context-preference:codex-native:1"', mode: "codex_default" },
    },
    {
      id: "team-proxy",
      revision: 1,
      etag: '"codex-profile-definition:team-proxy:1"',
      kind: "api",
      name: "Team Proxy",
      baseURL: "https://api.example.com/v1",
      model: "gpt-5.5",
      reviewModel: "",
      reasoningEffort: "high",
      available: true,
      hasAPIKey: true,
      editable: true,
      deletable: true,
      contextEditable: true,
      contextPreference: { profileID: "team-proxy", revision: 1, etag: '"codex-context-preference:team-proxy:1"', mode: "codex_default" },
    },
  ];
}

function claudeProfiles() {
  return [
    {
      id: "default",
      name: "默认",
      authMode: "inherit",
      hasAuthToken: false,
      builtIn: true,
      persisted: false,
      readOnly: true,
      contextPreference: { profileID: "default", revision: 1, etag: '"claude-context-preference:default:1"', mode: "default" },
    },
    {
      id: "devseek",
      name: "DevSeek",
      authMode: "api_key",
      baseURL: "https://api.example.com/v1",
      hasAuthToken: true,
      model: "claude-sonnet-4-5",
      smallModel: "claude-haiku-4-5",
      reasoningEffort: "medium",
      builtIn: false,
      persisted: true,
      readOnly: false,
      contextPreference: { profileID: "devseek", revision: 1, etag: '"claude-context-preference:devseek:1"', mode: "default" },
    },
  ];
}
