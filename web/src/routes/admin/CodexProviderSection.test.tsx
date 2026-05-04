import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { CodexProviderSection } from "./CodexProviderSection";
import { makeCodexProvider } from "../../test/fixtures";
import { installMockFetch } from "../../test/http";

describe("CodexProviderSection", () => {
  it("keeps editing user-facing and saves only the allowed fields", async () => {
    const user = userEvent.setup();
    const initialProviders = [
      makeCodexProvider(),
      makeCodexProvider({
        id: "team-proxy",
        name: "Team Proxy",
        baseURL: "https://proxy.internal/v1",
        hasApiKey: true,
        model: "gpt-5.4",
        reasoningEffort: "high",
        builtIn: false,
        persisted: true,
        readOnly: false,
      }),
    ];
    const { calls } = installMockFetch({
      "/api/admin/codex/providers/team-proxy": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            provider: makeCodexProvider({
              id: "team-proxy-2",
              name: body.name,
              baseURL: body.baseURL,
              hasApiKey: true,
              model: body.model,
              reasoningEffort: body.reasoningEffort,
              builtIn: false,
              persisted: true,
              readOnly: false,
            }),
          },
        };
      },
      "/api/admin/codex/providers": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          status: 201,
          body: {
            provider: makeCodexProvider({
              id: "new-provider",
              name: body.name,
              baseURL: body.baseURL,
              hasApiKey: true,
              model: body.model,
              reasoningEffort: body.reasoningEffort,
              builtIn: false,
              persisted: true,
              readOnly: false,
            }),
          },
        };
      },
    });

    function Harness() {
      const [providers, setProviders] = useState(initialProviders);
      return (
        <CodexProviderSection
          providers={providers}
          loadError=""
          setProviders={setProviders}
          onReload={async () => {}}
        />
      );
    }

    render(<Harness />);

    await user.click(await screen.findByRole("button", { name: /Team Proxy/ }));

    expect(screen.queryByText("model_provider")).not.toBeInTheDocument();
    expect(screen.queryByText("env_key")).not.toBeInTheDocument();
    expect(screen.queryByText("requires_openai_auth")).not.toBeInTheDocument();
    expect(screen.queryByText("auth.json")).not.toBeInTheDocument();

    await user.clear(screen.getByLabelText(/名称/));
    await user.type(screen.getByLabelText(/名称/), "Team Proxy 2");
    await user.clear(screen.getByLabelText(/端点地址/));
    await user.type(screen.getByLabelText(/端点地址/), "https://proxy.second/v1");
    await user.clear(screen.getByLabelText("默认模型"));
    await user.type(screen.getByLabelText("默认模型"), "gpt-5.5");
    await user.selectOptions(screen.getByLabelText("默认推理强度"), "xhigh");
    const apiKeyInput = screen.getByPlaceholderText("输入 API Key") as HTMLInputElement;
    await user.type(apiKeyInput, "updated-secret");
    expect(apiKeyInput.value).toBe("updated-secret");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    expect(await screen.findByText("Codex Provider 已保存。")).toBeInTheDocument();
    const updateCall = calls.find(
      (call) => call.method === "PUT" && call.path === "/api/admin/codex/providers/team-proxy",
    );
    expect(updateCall).toBeDefined();
    expect(JSON.parse(String(updateCall?.init?.body))).toEqual({
      name: "Team Proxy 2",
      baseURL: "https://proxy.second/v1",
      apiKey: "updated-secret",
      model: "gpt-5.5",
      reasoningEffort: "xhigh",
    });
    expect(await screen.findByRole("button", { name: /Team Proxy 2/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /新增配置/ }));
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByText("请填写名称。")).toBeInTheDocument();

    await user.type(screen.getByLabelText(/名称/), "新代理");
    await user.type(screen.getByLabelText(/端点地址/), "https://proxy.new/v1");
    await user.type(screen.getByLabelText(/API Key/), "new-secret");
    await user.type(screen.getByLabelText("默认模型"), "gpt-5.4");
    await user.selectOptions(screen.getByLabelText("默认推理强度"), "medium");
    await user.click(screen.getByRole("button", { name: "保存配置" }));

    const createCall = calls.find(
      (call) => call.method === "POST" && call.path === "/api/admin/codex/providers",
    );
    expect(createCall).toBeDefined();
    expect(JSON.parse(String(createCall?.init?.body))).toEqual({
      name: "新代理",
      baseURL: "https://proxy.new/v1",
      apiKey: "new-secret",
      model: "gpt-5.4",
      reasoningEffort: "medium",
    });
  });

  it("maps backend errors to user-facing copy", async () => {
    const user = userEvent.setup();
    installMockFetch({
      "/api/admin/codex/providers": {
        status: 409,
        body: {
          error: {
            code: "codex_provider_reserved_name",
            message: "this codex provider name cannot be used",
          },
        },
      },
    });

    render(
      <CodexProviderSection
        providers={[makeCodexProvider()]}
        loadError=""
        setProviders={() => {}}
        onReload={async () => {}}
      />,
    );

    await user.click(await screen.findByRole("button", { name: /新增配置/ }));
    await user.type(screen.getByLabelText(/名称/), "OpenAI");
    await user.type(screen.getByLabelText(/端点地址/), "https://proxy.new/v1");
    await user.type(screen.getByLabelText(/API Key/), "new-secret");
    await user.click(screen.getByRole("button", { name: "保存配置" }));

    expect(await screen.findByText("保存没有完成：这个名称不能使用，请换一个名字。")).toBeInTheDocument();
  });
});
