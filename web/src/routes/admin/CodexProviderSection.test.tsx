import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { CodexProviderSection } from "./CodexProviderSection";
import { makeCodexProfile } from "../../test/fixtures";
import { installMockFetch } from "../../test/http";

describe("CodexProviderSection", () => {
  it("uses canonical profile APIs, etags, review model, and context preference", async () => {
    const user = userEvent.setup();
    const initialProfiles = [
      makeCodexProfile(),
      makeCodexProfile({
        id: "team-proxy",
        name: "Team Proxy",
        kind: "api",
        etag: '"codex-profile-definition:team-proxy:7"',
        baseURL: "https://api.example.com/v1",
        hasAPIKey: true,
        model: "gpt-5.5",
        reviewModel: "gpt-5.5-review",
        reasoningEffort: "high",
        editable: true,
        deletable: true,
        contextEditable: true,
        contextPreference: {
          profileID: "team-proxy",
          revision: 3,
          etag: '"codex-context-preference:team-proxy:3"',
          mode: "codex_default",
        },
      }),
    ];
    const { calls } = installMockFetch({
      "/api/admin/codex/profiles/team-proxy": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            profile: makeCodexProfile({
              id: "team-proxy",
              name: body.name,
              kind: "api",
              etag: '"codex-profile-definition:team-proxy:8"',
              baseURL: body.baseURL,
              hasAPIKey: true,
              model: body.model,
              reviewModel: body.reviewModel,
              reasoningEffort: body.reasoningEffort,
              editable: true,
              deletable: true,
              contextEditable: true,
              contextPreference: {
                profileID: "team-proxy",
                revision: 3,
                etag: '"codex-context-preference:team-proxy:3"',
                mode: "codex_default",
              },
            }),
          },
        };
      },
      "/api/admin/codex/profiles/team-proxy/context-preference": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            contextPreference: {
              profileID: "team-proxy",
              revision: 4,
              etag: '"codex-context-preference:team-proxy:4"',
              mode: body.mode,
            },
          },
        };
      },
    });

    function Harness() {
      const [profiles, setProfiles] = useState(initialProfiles);
      return (
        <CodexProviderSection
          providers={profiles}
          loadError=""
          setProviders={setProfiles}
          onReload={async () => {}}
        />
      );
    }

    render(<Harness />);

    await user.click(await screen.findByRole("button", { name: /Team Proxy/ }));

    expect(screen.getByRole("heading", { name: "Codex" })).toBeInTheDocument();
    expect(screen.queryByText("model_provider")).not.toBeInTheDocument();
    expect(screen.queryByText("env_key")).not.toBeInTheDocument();
    expect(screen.queryByText("requires_openai_auth")).not.toBeInTheDocument();
    expect(screen.queryByText("auth.json")).not.toBeInTheDocument();

    await user.clear(screen.getByLabelText(/名称/));
    await user.type(screen.getByLabelText(/名称/), "Team Proxy 2");
    await user.clear(screen.getByLabelText(/端点地址/));
    await user.type(screen.getByLabelText(/端点地址/), "https://proxy.second/v1");
    await user.clear(screen.getByLabelText("主模型"));
    await user.type(screen.getByLabelText("主模型"), "gpt-5.5");
    await user.clear(screen.getByLabelText("审阅模型"));
    await user.type(screen.getByLabelText("审阅模型"), "gpt-5.5-review");
    await user.clear(screen.getByLabelText("推理强度"));
    await user.type(screen.getByLabelText("推理强度"), "xhigh");
    await user.selectOptions(screen.getByLabelText("上下文大小"), "extended_1m");
    const apiKeyInput = screen.getByPlaceholderText("输入新的 API Key") as HTMLInputElement;
    await user.type(apiKeyInput, "updated-secret");
    expect(apiKeyInput.value).toBe("updated-secret");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    expect(await screen.findByText("Codex 配置已保存。")).toBeInTheDocument();
    const updateCall = calls.find(
      (call) => call.method === "PUT" && call.path === "/api/admin/codex/profiles/team-proxy",
    );
    expect(updateCall).toBeDefined();
    expect((updateCall?.init?.headers as Record<string, string>)["If-Match"]).toBe(
      '"codex-profile-definition:team-proxy:7"',
    );
    expect(JSON.parse(String(updateCall?.init?.body))).toEqual({
      name: "Team Proxy 2",
      baseURL: "https://proxy.second/v1",
      apiKey: "updated-secret",
      model: "gpt-5.5",
      reviewModel: "gpt-5.5-review",
      reasoningEffort: "xhigh",
    });
    const preferenceCall = calls.find(
      (call) =>
        call.method === "PUT" &&
        call.path === "/api/admin/codex/profiles/team-proxy/context-preference",
    );
    expect(preferenceCall).toBeDefined();
    expect((preferenceCall?.init?.headers as Record<string, string>)["If-Match"]).toBe(
      '"codex-context-preference:team-proxy:3"',
    );
    expect(JSON.parse(String(preferenceCall?.init?.body))).toEqual({ mode: "extended_1m" });
  });

  it("keeps api keys empty on edit and allows readonly profiles to change context", async () => {
    const user = userEvent.setup();
    const initialProfiles = [
      makeCodexProfile({
        id: "codex-native",
        name: "本机默认",
        kind: "native",
        available: true,
        editable: false,
        deletable: false,
        contextEditable: true,
        contextPreference: {
          profileID: "codex-native",
          revision: 1,
          etag: '"codex-context-preference:codex-native:1"',
          mode: "codex_default",
        },
      }),
      makeCodexProfile({
        id: "team-proxy",
        name: "Team Proxy",
        kind: "api",
        etag: '"codex-profile-definition:team-proxy:7"',
        baseURL: "https://api.example.com/v1",
        hasAPIKey: true,
        model: "gpt-5.5",
        reasoningEffort: "high",
        editable: true,
        deletable: true,
        contextEditable: true,
      }),
    ];
    const { calls } = installMockFetch({
      "/api/admin/codex/profiles/team-proxy": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            profile: makeCodexProfile({
              id: "team-proxy",
              name: body.name,
              kind: "api",
              baseURL: body.baseURL,
              hasAPIKey: true,
              model: body.model,
              reviewModel: body.reviewModel,
              reasoningEffort: body.reasoningEffort,
              editable: true,
              deletable: true,
              contextEditable: true,
            }),
          },
        };
      },
      "/api/admin/codex/profiles/codex-native/context-preference": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            contextPreference: {
              profileID: "codex-native",
              revision: 2,
              etag: '"codex-context-preference:codex-native:2"',
              mode: body.mode,
            },
          },
        };
      },
    });

    function Harness() {
      const [profiles, setProfiles] = useState(initialProfiles);
      return (
        <CodexProviderSection
          providers={profiles}
          loadError=""
          setProviders={setProfiles}
          onReload={async () => {}}
        />
      );
    }

    render(<Harness />);

    await user.click(await screen.findByRole("button", { name: /Team Proxy/ }));
    expect(screen.getByPlaceholderText("输入新的 API Key")).toHaveValue("");

    await user.clear(screen.getByLabelText(/名称/));
    await user.type(screen.getByLabelText(/名称/), "Team Proxy 2");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    expect(await screen.findByText("Codex 配置已保存。")).toBeInTheDocument();
    const updateCall = calls.find(
      (call) => call.method === "PUT" && call.path === "/api/admin/codex/profiles/team-proxy",
    );
    expect(updateCall).toBeDefined();
    expect(JSON.parse(String(updateCall?.init?.body))).toEqual({
      name: "Team Proxy 2",
      baseURL: "https://api.example.com/v1",
      model: "gpt-5.5",
      reviewModel: "",
      reasoningEffort: "high",
    });

    await user.click(await screen.findByRole("button", { name: /本机默认/ }));
    expect(screen.getByText("连接身份由本机 Codex 管理。")).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText("上下文大小"), "price_guard_272k");
    expect(screen.getByText("按费用优先请求 272K；这不是单次请求费用硬上限。")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "保存上下文偏好" }));

    expect(await screen.findByText("上下文偏好已保存。")).toBeInTheDocument();
    const contextCall = calls.find(
      (call) =>
        call.method === "PUT" &&
        call.path === "/api/admin/codex/profiles/codex-native/context-preference",
    );
    expect(contextCall).toBeDefined();
    expect(JSON.parse(String(contextCall?.init?.body))).toEqual({
      mode: "price_guard_272k",
    });
  });

  it("replaces the previous list item when the saved profile id changes", async () => {
    const user = userEvent.setup();
    const initialProfiles = [
      makeCodexProfile({
        id: "team-proxy",
        name: "Team Proxy",
        kind: "api",
        etag: '"codex-profile-definition:team-proxy:7"',
        baseURL: "https://api.example.com/v1",
        hasAPIKey: true,
        model: "gpt-5.5",
        reasoningEffort: "high",
        editable: true,
        deletable: true,
        contextEditable: true,
        contextPreference: {
          profileID: "team-proxy",
          revision: 3,
          etag: '"codex-context-preference:team-proxy:3"',
          mode: "codex_default",
        },
      }),
    ];
    installMockFetch({
      "/api/admin/codex/profiles/team-proxy": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            profile: makeCodexProfile({
              id: "team-proxy-renamed",
              name: body.name,
              kind: "api",
              etag: '"codex-profile-definition:team-proxy-renamed:1"',
              baseURL: body.baseURL,
              hasAPIKey: true,
              model: body.model,
              reasoningEffort: body.reasoningEffort,
              editable: true,
              deletable: true,
              contextEditable: true,
              contextPreference: {
                profileID: "team-proxy-renamed",
                revision: 1,
                etag: '"codex-context-preference:team-proxy-renamed:1"',
                mode: "codex_default",
              },
            }),
          },
        };
      },
    });

    function Harness() {
      const [profiles, setProfiles] = useState(initialProfiles);
      return (
        <CodexProviderSection
          providers={profiles}
          loadError=""
          setProviders={setProfiles}
          onReload={async () => {}}
        />
      );
    }

    render(<Harness />);

    await user.click(await screen.findByRole("button", { name: /Team Proxy/ }));
    await user.clear(screen.getByLabelText(/名称/));
    await user.type(screen.getByLabelText(/名称/), "Renamed Proxy");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    expect(await screen.findByRole("button", { name: /Renamed Proxy/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Team Proxy/ })).not.toBeInTheDocument();
  });

  it("does not offer a no-op save for readonly profiles without context editing", async () => {
    render(
      <CodexProviderSection
        providers={[
          makeCodexProfile({
            id: "old-codex",
            name: "旧版 Codex",
            kind: "native",
            editable: false,
            deletable: false,
            contextEditable: false,
            statusCode: "codex_capability_unsupported",
          }),
        ]}
        loadError=""
        setProviders={() => {}}
        onReload={async () => {}}
      />,
    );

    await userEvent.click(await screen.findByRole("button", { name: /旧版 Codex/ }));

    expect(screen.getByText("连接身份和上下文偏好不可编辑。")).toBeInTheDocument();
    expect(screen.getByLabelText("上下文大小")).toBeDisabled();
    expect(screen.getByRole("button", { name: "保存上下文偏好" })).toBeDisabled();
  });

  it("loads references before delete and maps conflicts without leaking paths", async () => {
    const user = userEvent.setup();
    installMockFetch({
      "/api/admin/codex/profiles/team-proxy/references": {
        body: {
          profileID: "team-proxy",
          references: [
            {
              kind: "active_instance",
              name: "当前会话",
              reason: "observed instance admission",
            },
          ],
        },
      },
      "/api/admin/codex/profiles/team-proxy": {
        status: 409,
        body: {
          error: {
            code: "codex_profile_in_use",
            message: "codex profile is still referenced",
            details: {
              profileID: "team-proxy",
              references: [
                {
                  kind: "surface_recovery",
                  name: "/tmp/private/workspace",
                  reason: "retained for recovery",
                },
              ],
            },
          },
        },
      },
    });

    render(
      <CodexProviderSection
        providers={[
          makeCodexProfile(),
          makeCodexProfile({
            id: "team-proxy",
            name: "Team Proxy",
            kind: "api",
            etag: '"codex-profile-definition:team-proxy:7"',
            editable: true,
            deletable: true,
            contextEditable: true,
          }),
        ]}
        loadError=""
        setProviders={() => {}}
        onReload={async () => {}}
      />,
    );

    await user.click(await screen.findByRole("button", { name: /Team Proxy/ }));
    await user.click(screen.getByRole("button", { name: "删除配置" }));

    expect(await screen.findByText("当前仍有使用中的会话。")).toBeInTheDocument();
    expect(screen.getByText("会话 · 当前会话")).toBeInTheDocument();
    expect(screen.queryByText(/active_instance/)).not.toBeInTheDocument();
    expect(screen.queryByText(/observed instance admission/)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "确认删除" }));

    expect(await screen.findByText(/这个配置仍在使用中/)).toBeInTheDocument();
    expect(screen.queryByText("/tmp/private/workspace")).not.toBeInTheDocument();
  });

  it("shows context request and observed clamp feedback without guessing effect", async () => {
    const user = userEvent.setup();
    render(
      <CodexProviderSection
        providers={[
          makeCodexProfile({
            id: "long-context",
            name: "Long Context",
            kind: "api",
            editable: true,
            deletable: true,
            contextEditable: true,
            contextPreference: {
              profileID: "long-context",
              revision: 1,
              etag: '"codex-context-preference:long-context:1"',
              mode: "extended_1m",
            },
          }),
          makeCodexProfile({
            id: "clamped-context",
            name: "Clamped Context",
            kind: "api",
            editable: true,
            deletable: true,
            contextEditable: true,
            contextPreference: {
              profileID: "clamped-context",
              revision: 1,
              etag: '"codex-context-preference:clamped-context:1"',
              mode: "extended_1m",
            },
            contextStatus: "context_preference_clamped",
            effectiveContextWindow: 258400,
          }),
        ]}
        loadError=""
        setProviders={() => {}}
        onReload={async () => {}}
      />,
    );

    await user.click(await screen.findByRole("button", { name: /Long Context/ }));
    expect(
      screen.getByText("新会话开始后确认实际生效值；可能受模型上限影响。"),
    ).toBeInTheDocument();

    await user.click(await screen.findByRole("button", { name: /Clamped Context/ }));
    expect(screen.getByText("目标模型限制为约 258K。")).toBeInTheDocument();
  });

  it("requires model and open reasoning for API profiles", async () => {
    const user = userEvent.setup();
    render(
      <CodexProviderSection
        providers={[makeCodexProfile()]}
        loadError=""
        setProviders={() => {}}
        onReload={async () => {}}
      />,
    );

    await user.click(await screen.findByRole("button", { name: /新增配置/ }));
    await user.type(screen.getByLabelText(/名称/), "新代理");
    await user.type(screen.getByLabelText(/端点地址/), "https://proxy.new/v1");
    await user.type(screen.getByLabelText(/API Key/), "new-secret");
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByText("请填写主模型。")).toBeInTheDocument();

    await user.type(screen.getByLabelText("主模型"), "gpt-5.5");
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByText("请填写推理强度。")).toBeInTheDocument();

    await user.type(screen.getByLabelText("推理强度"), "vendor-custom-effort");
    expect(screen.getByLabelText("推理强度")).toHaveValue("vendor-custom-effort");
  });
});
