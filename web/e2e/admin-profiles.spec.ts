import { expect, test, type Page } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await installAdminMocks(page);
});

test("admin profiles can be managed on desktop and mobile", async ({ page }) => {
  await page.goto("/admin");

  const codexSection = page
    .locator("section.panel")
    .filter({ has: page.getByRole("heading", { name: "Codex Profile", exact: true }) })
    .first();
  await expect(codexSection.getByRole("heading", { name: "Codex Profile", exact: true })).toBeVisible();
  await codexSection.getByRole("button", { name: /新增 Profile/ }).click();
  await codexSection.getByLabel(/名称/).fill("E2E Profile");
  await codexSection.getByLabel(/端点地址/).fill("https://api.example.com/v1");
  await codexSection.getByLabel(/API Key/).fill("e2e-secret");
  await codexSection.getByLabel("主模型").fill("gpt-5.4");
  await codexSection.getByLabel("推理强度").fill("high");
  await codexSection.getByRole("button", { name: "保存 Profile" }).click();
  await expect(codexSection.getByText("Codex Profile 已创建。")).toBeVisible();
  await expect(codexSection.getByRole("button", { name: /E2E Profile/ })).toBeVisible();
  await codexSection.getByRole("button", { name: "删除 Profile" }).click();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(codexSection.getByText("Codex Profile 已删除。")).toBeVisible();
  await expect(codexSection.getByRole("button", { name: /E2E Profile/ })).toHaveCount(0);

  await codexSection.getByRole("button", { name: /Team Proxy/ }).click();
  await expect(codexSection.getByLabel("上下文大小")).toBeVisible();
  await codexSection.getByLabel("上下文大小").selectOption("price_guard_272k");
  await expect(codexSection.getByText("按费用优先请求 272K；这不是单次请求费用硬上限。")).toBeVisible();
  await codexSection.getByRole("button", { name: "保存修改" }).click();
  await expect(codexSection.getByText("Codex Profile 已保存。")).toBeVisible();

  const claudeSection = page
    .locator("section.panel")
    .filter({ has: page.getByRole("heading", { name: "Claude Profile", exact: true }) })
    .first();
  await expect(claudeSection.getByRole("heading", { name: "Claude Profile", exact: true })).toBeVisible();
  await claudeSection.getByRole("button", { name: /DevSeek/ }).click();
  await claudeSection.getByLabel("使用 1M 上下文").check();
  await claudeSection.getByRole("button", { name: "保存修改" }).click();
  await expect(claudeSection.getByText("Claude 配置已保存。")).toBeVisible();
});

type CodexProfile = ReturnType<typeof codexProfiles>[number];

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
      await route.fulfill({ json: { apps: [] } });
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
    feishu: { appCount: 0, enabledAppCount: 0, configuredAppCount: 0, runtimeConfiguredApps: 0 },
    gateways: [],
  };
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
      model: "gpt-5.4",
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
