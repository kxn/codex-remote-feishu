import { describe, expect, it } from "vitest";
import {
  appendOrReplaceProfileItem,
  maxProfileTextLengthMessage,
  removeProfileItem,
  requiredProfileFieldMessage,
} from "./ProfileEditorShared";

describe("ProfileEditorShared", () => {
  it("appends, replaces, and removes profile items by stable IDs", () => {
    const profiles = [
      { id: "default", name: "Default" },
      { id: "team", name: "Team" },
    ];

    expect(
      appendOrReplaceProfileItem(profiles, { id: "team-v2", name: "Team v2" }, "team"),
    ).toEqual([
      { id: "default", name: "Default" },
      { id: "team-v2", name: "Team v2" },
    ]);
    expect(appendOrReplaceProfileItem(profiles, { id: "team", name: "Team 2" })).toEqual([
      { id: "default", name: "Default" },
      { id: "team", name: "Team 2" },
    ]);
    expect(appendOrReplaceProfileItem(profiles, { id: "extra", name: "Extra" })).toEqual([
      { id: "default", name: "Default" },
      { id: "team", name: "Team" },
      { id: "extra", name: "Extra" },
    ]);
    expect(removeProfileItem(profiles, "team")).toEqual([{ id: "default", name: "Default" }]);
  });

  it("keeps profile validation wording centralized without choosing required fields", () => {
    expect(requiredProfileFieldMessage("", "名称")).toBe("请填写名称。");
    expect(requiredProfileFieldMessage("", "API Key")).toBe("请填写 API Key。");
    expect(requiredProfileFieldMessage(" Team ", "名称")).toBe("");
    expect(maxProfileTextLengthMessage("abcdef", 5, "指令")).toBe("指令最多 5 字符。");
    expect(maxProfileTextLengthMessage("abcde", 5, "指令")).toBe("");
  });
});
