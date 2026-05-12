import { describe, expect, it } from "vitest";
import {
  describeAutoConfigRequirementDisplay,
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
      detail: "需要在飞书开放平台补齐对应权限后才能继续。",
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
      label: "机器人基础消息能力",
      detail: "需要先在飞书后台开通对应事件。",
    });
  });

  it("keeps onboarding stage tone mapping explicit in shared helpers", () => {
    expect(onboardingAutoConfigNoticeTone("complete")).toBe("good");
    expect(onboardingAutoConfigNoticeTone("deferred")).toBe("warn");
    expect(onboardingAutoConfigNoticeTone("blocked")).toBe("danger");
    expect(onboardingAutoConfigNoticeTone("pending")).toBe("warn");
  });
});
