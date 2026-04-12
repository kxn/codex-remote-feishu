import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./app";
import { makeApp, makeBootstrap, makeManifest, makeVSCodeDetect } from "./test/fixtures";
import { installMockFetch } from "./test/http";

describe("App", () => {
  it("renders the setup route when mounted under a prefixed setup path", async () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    installMockFetch({
      "/g/demo/api/setup/bootstrap-state": { body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }) },
      "/g/demo/api/setup/feishu/apps": {
        body: {
          apps: [makeApp({ wizard: {} })],
        },
      },
      "/g/demo/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/g/demo/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<App />);

    expect(await screen.findByRole("heading", { name: "环境检查" })).toBeInTheDocument();
  });

  it("renders the setup mock route without backend requests", async () => {
    window.history.replaceState({}, "", "/setup-mock");

    render(<App />);

    expect(await screen.findByText("场景预设")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "扫码等待中" })).toBeInTheDocument();
  });
});
