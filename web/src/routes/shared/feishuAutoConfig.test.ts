import { describe, expect, it } from "vitest";
import {
  buildMissingScopesImportJSON,
  describeAutoConfigBlockingReason,
  describeAutoConfigRequirementDisplay,
  groupAutoConfigRequirements,
  onboardingAutoConfigNoticeTone,
} from "./feishuAutoConfig";
import { makeAutoConfigPlan } from "../../test/fixtures";

describe("feishu auto-config shared helpers", () => {
  it("builds a missing-only scopes import JSON grouped by token type", () => {
    const plan = makeAutoConfigPlan({
      diff: {
        configPatchRequired: true,
        abilityPatchRequired: false,
        missingScopes: [
          { scope: "im:message", scopeType: "tenant" },
          { scope: "calendar:calendar:read", scopeType: "user" },
          { scope: "drive:drive", scopeType: "tenant" },
        ],
        extraScopes: [],
        missingEvents: [],
        extraEvents: [],
        missingCallbacks: [],
        extraCallbacks: [],
        callbackTypeMismatch: false,
        callbackRequestUrlMismatch: false,
        publishRequired: true,
      },
    });
    expect(buildMissingScopesImportJSON(plan)).toBe(
      JSON.stringify(
        {
          scopes: {
            tenant: ["im:message", "drive:drive"],
            user: ["calendar:calendar:read"],
          },
        },
        null,
        2,
      ),
    );
  });

  it("returns empty tenant/user groups when no scopes are missing", () => {
    expect(buildMissingScopesImportJSON(makeAutoConfigPlan())).toBe(
      JSON.stringify({ scopes: { tenant: [], user: [] } }, null, 2),
    );
  });

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
          key: "im:message.group_msg",
          scopeType: "tenant",
          feature: "core_message_flow",
          required: true,
          present: false,
        },
      ]),
    ).toEqual([
      {
        key: "scope:tenant:bitable:app",
        kind: "scope",
        label: "权限 bitable:app",
        copyValue: "bitable:app",
        meta: "权限 · tenant",
        impacts: ["/cron 多维表格"],
      },
      {
        key: "scope:tenant:im:message.group_msg",
        kind: "scope",
        label: "权限 im:message.group_msg",
        copyValue: "im:message.group_msg",
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

  it("maps blocking reasons through a user-facing allowlist", () => {
    expect(describeAutoConfigBlockingReason("feishu_read_failed")).toContain("读取飞书应用配置");
    expect(describeAutoConfigBlockingReason("permission_denied")).toContain("没有修改飞书应用配置的权限");
    expect(describeAutoConfigBlockingReason("credential_invalid")).toContain("凭证已经失效");
  });

  it("does not echo unknown internal blocking reasons into visible copy", () => {
    const rawReasons = [
      "feishu_api_error",
      "application.v6.application.get",
      "99992402 field validation failed",
    ];

    for (const reason of rawReasons) {
      const copy = describeAutoConfigBlockingReason(reason);
      expect(copy).not.toContain(reason);
      expect(copy).not.toContain("99992402");
      expect(copy).not.toContain("field validation failed");
      expect(copy).not.toContain("application.v6.application.get");
    }
  });
});
