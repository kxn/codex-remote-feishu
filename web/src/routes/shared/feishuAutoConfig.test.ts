import { describe, expect, it } from "vitest";
import {
  describeAutoConfigRequirementDisplay,
  groupAutoConfigRequirements,
  onboardingAutoConfigNoticeTone,
} from "./feishuAutoConfig";

describe("feishu auto-config shared helpers", () => {
  it("builds requirement display rows from the shared label/detail rules", () => {
    expect(
      describeAutoConfigRequirementDisplay({
        kind: "scope",
        key: "im:message",
        scopeType: "tenant",
        required: true,
        present: false,
      }),
    ).toEqual({
      label: "权限 im:message",
      detail: "",
    });

    expect(
      describeAutoConfigRequirementDisplay({
        kind: "event",
        key: "message.receive_v1",
        feature: "core_message_flow",
        required: true,
        present: false,
      }),
    ).toEqual({
      label: "事件 message.receive_v1",
      detail: "机器人基础消息能力",
    });
  });

  it("groups requirement rows by missing config and merges impact labels", () => {
    expect(
      groupAutoConfigRequirements([
        {
          kind: "scope",
          key: "bitable:app",
          scopeType: "tenant",
          feature: "cron_bitable",
          purpose: "/cron 多维表格",
          required: true,
          present: false,
        },
        {
          kind: "scope",
          key: "bitable:app",
          scopeType: "tenant",
          feature: "cron_bitable",
          purpose: "/cron 多维表格",
          required: true,
          present: false,
        },
        {
          kind: "scope",
          key: "im:message.group_msg:readonly",
          scopeType: "tenant",
          feature: "core_message_flow",
          required: true,
          present: false,
        },
      ]),
    ).toEqual([
      {
        key: "scope:tenant:bitable:app",
        label: "权限 bitable:app",
        meta: "权限 · tenant",
        impacts: ["/cron 多维表格"],
      },
      {
        key: "scope:tenant:im:message.group_msg:readonly",
        label: "权限 im:message.group_msg:readonly",
        meta: "权限 · tenant",
        impacts: ["机器人基础消息能力"],
      },
    ]);
  });

  it("keeps onboarding stage tone mapping explicit in shared helpers", () => {
    expect(onboardingAutoConfigNoticeTone("complete")).toBe("good");
    expect(onboardingAutoConfigNoticeTone("deferred")).toBe("warn");
    expect(onboardingAutoConfigNoticeTone("blocked")).toBe("danger");
    expect(onboardingAutoConfigNoticeTone("pending")).toBe("warn");
  });
});
