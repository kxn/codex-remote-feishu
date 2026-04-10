import { describe, expect, it } from "vitest";
import { currentRouteIsSetup, relativeLocalPath } from "./paths";

describe("paths helpers", () => {
  it("converts root-based local paths to dot-relative paths", () => {
    expect(relativeLocalPath("/")).toBe("./");
    expect(relativeLocalPath("/api/admin/bootstrap-state")).toBe("./api/admin/bootstrap-state");
    expect(relativeLocalPath("http://localhost:9501/setup?token=abc")).toBe("./setup?token=abc");
  });

  it("detects setup routes even when the page is mounted under a prefixed path", () => {
    expect(currentRouteIsSetup("/setup")).toBe(true);
    expect(currentRouteIsSetup("/g/demo/setup")).toBe(true);
    expect(currentRouteIsSetup("/g/demo/")).toBe(false);
  });
});
