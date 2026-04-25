import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./app";
import {
  makeBootstrap,
  makeFeishuManifest,
  makeRuntimeRequirementsDetect,
  makeVSCodeDetect,
} from "./test/fixtures";
import { installMockFetch } from "./test/http";

describe("App", () => {
  it("renders the setup route when mounted under a prefixed setup path", async () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    installMockFetch({
      "/g/demo/api/setup/bootstrap-state": {
        body: makeBootstrap({ admin: { setupURL: "/g/demo/setup" } }),
      },
      "/g/demo/api/setup/feishu/manifest": {
        body: makeFeishuManifest(),
      },
      "/g/demo/api/setup/feishu/apps": {
        body: { apps: [] },
      },
      "/g/demo/api/setup/feishu/onboarding/sessions": {
        status: 201,
        body: {
          session: {
            id: "session-1",
            status: "pending",
            qrCodeDataUrl: "data:image/png;base64,abc",
          },
        },
      },
      "/g/demo/api/setup/runtime-requirements/detect": {
        body: makeRuntimeRequirementsDetect(),
      },
      "/g/demo/api/setup/autostart/detect": {
        body: { platform: "linux", supported: true, status: "disabled", configured: false, enabled: false, canApply: true },
      },
      "/g/demo/api/setup/vscode/detect": { body: makeVSCodeDetect() },
    });

    render(<App />);

    expect(await screen.findByRole("heading", { name: "飞书连接" })).toBeInTheDocument();
  });
});
