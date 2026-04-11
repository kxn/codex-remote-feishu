import { describe, expect, it } from "vitest";
import { currentRouteIsSetup, relativeLocalPath } from "./paths";

describe("paths helpers", () => {
  it("converts root-based local paths to dot-relative paths", () => {
    expect(relativeLocalPath("/")).toBe("./");
    expect(relativeLocalPath("/api/admin/bootstrap-state")).toBe("./api/admin/bootstrap-state");
    expect(relativeLocalPath("http://localhost:9501/setup?token=abc")).toBe("./setup?token=abc");
  });

  it("keeps current grant-prefixed paths relative to the current external mount", () => {
    window.history.replaceState({}, "", "/g/demo/admin");

    expect(relativeLocalPath("/g/demo/setup?app=legacy-default")).toBe("./setup?app=legacy-default");
    expect(relativeLocalPath(`${window.location.origin}/g/demo/api/admin/bootstrap-state`)).toBe("./api/admin/bootstrap-state");
  });

  it("detects setup routes even when the page is mounted under a prefixed path", () => {
    expect(currentRouteIsSetup("/setup")).toBe(true);
    expect(currentRouteIsSetup("/g/demo/setup")).toBe(true);
    expect(currentRouteIsSetup("/g/demo/")).toBe(false);
  });
});
