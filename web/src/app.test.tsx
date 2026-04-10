import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./app";
import { makeApp, makeBootstrap, makeManifest, makeVSCodeDetect } from "./test/fixtures";
import { installMockFetch } from "./test/http";

describe("App", () => {
  it("renders the setup route when mounted under a prefixed setup path", async () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    installMockFetch({
      "/api/setup/bootstrap-state": { body: makeBootstrap() },
      "/api/setup/feishu/apps": {
        body: {
          apps: [makeApp({ wizard: {} })],
        },
      },
      "/api/setup/feishu/manifest": { body: { manifest: makeManifest() } },
      "/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<App />);

    expect(await screen.findByText("你想怎么接入飞书应用？")).toBeInTheDocument();
  });
});
