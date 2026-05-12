import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { ClaudeProfileSection } from "./ClaudeProfileSection";
import { makeClaudeProfile } from "../../test/fixtures";
import { installMockFetch } from "../../test/http";

describe("ClaudeProfileSection", () => {
  it("keeps editing user-facing and saves only the allowed fields", async () => {
    const user = userEvent.setup();
    const initialProfiles = [
      makeClaudeProfile(),
      makeClaudeProfile({
        id: "devseek",
        name: "DevSeek",
        authMode: "auth_token",
        baseURL: "https://proxy.internal/v1",
        hasAuthToken: true,
        model: "mimo-v2.5-pro",
        smallModel: "mimo-v2.5-haiku",
        reasoningEffort: "high",
        builtIn: false,
        persisted: true,
        readOnly: false,
      }),
    ];
    const { calls } = installMockFetch({
      "/api/admin/claude/profiles/devseek": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          body: {
            profile: makeClaudeProfile({
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
            }),
          },
        };
      },
      "/api/admin/claude/profiles": (call) => {
        const body = JSON.parse(String(call.init?.body ?? "{}"));
        return {
          status: 201,
          body: {
            profile: makeClaudeProfile({
              id: "new-profile",
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
            }),
          },
        };
      },
    });

    function Harness() {
      const [profiles, setProfiles] = useState(initialProfiles);
      return (
        <ClaudeProfileSection
          profiles={profiles}
          loadError=""
          setProfiles={setProfiles}
          onReload={async () => {}}
        />
      );
    }

    render(<Harness />);

    await user.click(await screen.findByRole("button", { name: /DevSeek/ }));

    expect(screen.queryByText("认证方式")).not.toBeInTheDocument();
    expect(screen.queryByText("Token 状态")).not.toBeInTheDocument();
    expect(screen.queryByText("Token 处理方式")).not.toBeInTheDocument();
    expect(screen.queryByText(/不会再次回显/)).not.toBeInTheDocument();

    await user.clear(screen.getByLabelText(/名称/));
    await user.type(screen.getByLabelText(/名称/), "DevSeek 2");
    await user.clear(screen.getByLabelText("端点地址"));
    await user.type(screen.getByLabelText("端点地址"), "https://proxy.second/v1");
    const authTokenInput = screen.getByPlaceholderText("输入认证 Token") as HTMLInputElement;
    await user.type(authTokenInput, "updated-token");
    expect(authTokenInput.value).toBe("updated-token");
    await user.clear(screen.getByLabelText("主模型"));
    await user.type(screen.getByLabelText("主模型"), "mimo-v2.6");
    await user.clear(screen.getByLabelText("轻量模型"));
    await user.type(screen.getByLabelText("轻量模型"), "mimo-v2.6-mini");
    await user.selectOptions(screen.getByLabelText("推理强度"), "max");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    expect(await screen.findByText("Claude 配置已保存。")).toBeInTheDocument();
    const updateCall = calls.find(
      (call) => call.method === "PUT" && call.path === "/api/admin/claude/profiles/devseek",
    );
    expect(updateCall).toBeDefined();
    expect(JSON.parse(String(updateCall?.init?.body))).toEqual({
      name: "DevSeek 2",
      baseURL: "https://proxy.second/v1",
      authToken: "updated-token",
      model: "mimo-v2.6",
      smallModel: "mimo-v2.6-mini",
      reasoningEffort: "max",
    });
    expect(await screen.findByRole("button", { name: /DevSeek 2/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /新增配置/ }));
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByText("请填写名称。")).toBeInTheDocument();

    await user.type(screen.getByLabelText(/名称/), "新配置");
    await user.type(screen.getByLabelText("端点地址"), "https://proxy.new/v1");
    await user.type(screen.getByLabelText("认证 Token"), "new-token");
    await user.type(screen.getByLabelText("主模型"), "sonnet-4");
    await user.type(screen.getByLabelText("轻量模型"), "haiku-4");
    await user.selectOptions(screen.getByLabelText("推理强度"), "medium");
    await user.click(screen.getByRole("button", { name: "保存配置" }));

    const createCall = calls.find(
      (call) => call.method === "POST" && call.path === "/api/admin/claude/profiles",
    );
    expect(createCall).toBeDefined();
    expect(JSON.parse(String(createCall?.init?.body))).toEqual({
      name: "新配置",
      baseURL: "https://proxy.new/v1",
      authToken: "new-token",
      model: "sonnet-4",
      smallModel: "haiku-4",
      reasoningEffort: "medium",
    });
  });
});
