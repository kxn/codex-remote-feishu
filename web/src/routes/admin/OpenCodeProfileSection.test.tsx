import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { OpenCodeProfileSection } from "./OpenCodeProfileSection";
import { makeOpenCodeProfile } from "../../test/fixtures";
import { installMockFetch } from "../../test/http";

describe("OpenCodeProfileSection", () => {
  it("saves only visible API fields while preserving edit API keys", async () => {
    const user = userEvent.setup();
    const initialProfiles = [
      makeOpenCodeProfile(),
      makeOpenCodeProfile({
        id: "op_team",
        revision: 7,
        etag: '"opencode-profile-definition:op_team:7"',
        name: "Team OpenCode",
        providerType: "google_gemini",
        baseURL: "https://api.example.com/v1",
        hasAPIKey: true,
        model: "gemini-2.5-pro",
        smallModel: "gemini-2.5-flash",
        reviewModel: "hidden-review",
        subagentModel: "gemini-agent",
        instruction: "be precise",
        reasoningEffort: "high",
        projectConfigMode: "disable",
        dataIsolationMode: "process",
        permissionMode: "ask",
        builtIn: false,
        persisted: true,
        readOnly: false,
      }),
    ];
    const { calls } = installMockFetch({
      "/api/admin/opencode/profiles/op_team": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            profile: makeOpenCodeProfile({
              id: "op_team",
              revision: 8,
              etag: '"opencode-profile-definition:op_team:8"',
              name: body.name,
              providerType: body.providerType,
              baseURL: body.baseURL,
              hasAPIKey: true,
              model: body.model,
              smallModel: body.smallModel,
              subagentModel: body.subagentModel,
              instruction: body.instruction,
              reasoningEffort: body.reasoningEffort,
              builtIn: false,
              persisted: true,
              readOnly: false,
            }),
          },
        };
      },
      "/api/admin/opencode/profiles": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          status: 201,
          body: {
            profile: makeOpenCodeProfile({
              id: "op_created",
              revision: 1,
              etag: '"opencode-profile-definition:op_created:1"',
              name: body.name,
              providerType: body.providerType,
              baseURL: body.baseURL,
              hasAPIKey: Boolean(body.apiKey),
              model: body.model,
              smallModel: body.smallModel,
              subagentModel: body.subagentModel,
              instruction: body.instruction,
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
      const [profiles, setProfiles] = useState(initialProfiles);
      return (
        <OpenCodeProfileSection
          profiles={profiles}
          loadError=""
          setProfiles={setProfiles}
          onReload={async () => {}}
        />
      );
    }

    render(<Harness />);

    await user.click(await screen.findByRole("button", { name: /Team OpenCode/ }));

    expect(screen.getByRole("heading", { name: "OpenCode" })).toBeInTheDocument();
    expect(screen.queryByText("审阅模型")).not.toBeInTheDocument();
    expect(screen.queryByText("hidden-review")).not.toBeInTheDocument();
    expect(screen.queryByText("projectConfigMode")).not.toBeInTheDocument();
    expect(screen.queryByText("dataIsolationMode")).not.toBeInTheDocument();
    expect(screen.queryByText("permissionMode")).not.toBeInTheDocument();
    expect(screen.queryByText("OPENCODE_CONFIG_CONTENT")).not.toBeInTheDocument();
    expect(screen.queryByText("ACP")).not.toBeInTheDocument();
    expect(screen.getByLabelText(/协议类型/)).toHaveValue("google_gemini");
    expect(screen.getByPlaceholderText("输入新的 API Key")).toHaveValue("");

    await user.clear(screen.getByLabelText(/名称/));
    await user.type(screen.getByLabelText(/名称/), "Team OpenCode 2");
    await user.selectOptions(screen.getByLabelText(/协议类型/), "openai_compatible_chat");
    await user.clear(screen.getByLabelText(/端点地址/));
    await user.type(screen.getByLabelText(/端点地址/), "https://proxy.second/v1");
    await user.clear(screen.getByLabelText("主模型"));
    await user.type(screen.getByLabelText("主模型"), "kimi-k2-pro");
    await user.clear(screen.getByLabelText("轻量模型"));
    await user.type(screen.getByLabelText("轻量模型"), "kimi-small-2");
    await user.clear(screen.getByLabelText("子代理模型"));
    await user.type(screen.getByLabelText("子代理模型"), "kimi-agent-2");
    await user.clear(screen.getByLabelText("推理强度"));
    await user.type(screen.getByLabelText("推理强度"), "medium");
    await user.clear(screen.getByLabelText(/指令/));
    await user.type(screen.getByLabelText(/指令/), "be exact");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    expect(await screen.findByText("OpenCode 配置已保存。")).toBeInTheDocument();
    const updateCall = calls.find(
      (call) => call.method === "PUT" && call.path === "/api/admin/opencode/profiles/op_team",
    );
    expect(updateCall).toBeDefined();
    expect((updateCall?.init?.headers as Record<string, string>)["If-Match"]).toBe(
      '"opencode-profile-definition:op_team:7"',
    );
    expect(JSON.parse(String(updateCall?.init?.body))).toEqual({
      name: "Team OpenCode 2",
      providerType: "openai_compatible_chat",
      baseURL: "https://proxy.second/v1",
      model: "kimi-k2-pro",
      smallModel: "kimi-small-2",
      subagentModel: "kimi-agent-2",
      instruction: "be exact",
      reasoningEffort: "medium",
      visionSupported: false,
    });

    await user.click(screen.getByRole("button", { name: /新增配置/ }));
    await user.type(screen.getByLabelText(/名称/), "新 OpenCode");
    expect(screen.getByLabelText(/协议类型/)).toHaveValue("openai_compatible_chat");
    await user.type(screen.getByLabelText(/端点地址/), "https://proxy.new/v1");
    await user.type(screen.getByLabelText(/API Key/), "new-secret");
    await user.type(screen.getByLabelText("主模型"), "kimi-k2");
    await user.type(screen.getByLabelText("轻量模型"), "kimi-small");
    await user.type(screen.getByLabelText("子代理模型"), "kimi-agent");
    await user.type(screen.getByLabelText("推理强度"), "high");
    await user.type(screen.getByLabelText(/指令/), "be useful");
    await user.click(screen.getByRole("button", { name: "保存配置" }));

    expect(await screen.findByText("OpenCode 配置已创建。")).toBeInTheDocument();
    const createCall = calls.find(
      (call) => call.method === "POST" && call.path === "/api/admin/opencode/profiles",
    );
    expect(createCall).toBeDefined();
    expect(JSON.parse(String(createCall?.init?.body))).toEqual({
      name: "新 OpenCode",
      providerType: "openai_compatible_chat",
      baseURL: "https://proxy.new/v1",
      apiKey: "new-secret",
      model: "kimi-k2",
      smallModel: "kimi-small",
      subagentModel: "kimi-agent",
      instruction: "be useful",
      reasoningEffort: "high",
      visionSupported: false,
    });
  });

  it("keeps the built-in profile read-only and deletes custom profiles with references", async () => {
    const user = userEvent.setup();
    const initialProfiles = [
      makeOpenCodeProfile(),
      makeOpenCodeProfile({
        id: "op_team",
        revision: 7,
        etag: '"opencode-profile-definition:op_team:7"',
        name: "Team OpenCode",
        baseURL: "https://api.example.com/v1",
        hasAPIKey: true,
        model: "kimi-k2",
        builtIn: false,
        persisted: true,
        readOnly: false,
      }),
    ];
    const { calls } = installMockFetch({
      "/api/admin/opencode/profiles/op_team/references": {
        body: {
          profileID: "op_team",
          references: [
            {
              kind: "active_instance",
              name: "当前会话",
              reason: "observed instance admission",
            },
          ],
        },
      },
      "/api/admin/opencode/profiles/op_team": {
        status: 204,
      },
    });

    function Harness() {
      const [profiles, setProfiles] = useState(initialProfiles);
      return (
        <OpenCodeProfileSection
          profiles={profiles}
          loadError=""
          setProfiles={setProfiles}
          onReload={async () => {}}
        />
      );
    }

    render(<Harness />);

    expect(await screen.findByText("系统默认配置")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "删除配置" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "保存修改" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Team OpenCode/ }));
    await user.click(screen.getByRole("button", { name: "删除配置" }));

    expect(await screen.findByText("当前仍有使用中的会话。")).toBeInTheDocument();
    expect(screen.getByText("会话 · 当前会话")).toBeInTheDocument();
    expect(screen.queryByText(/observed instance admission/)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "确认删除" }));

    expect(await screen.findByText("OpenCode 配置已删除。")).toBeInTheDocument();
    const deleteCall = calls.find(
      (call) => call.method === "DELETE" && call.path === "/api/admin/opencode/profiles/op_team",
    );
    expect(deleteCall).toBeDefined();
    expect((deleteCall?.init?.headers as Record<string, string>)["If-Match"]).toBe(
      '"opencode-profile-definition:op_team:7"',
    );
  });
});
