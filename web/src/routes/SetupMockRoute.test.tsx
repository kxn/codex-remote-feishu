import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { SetupMockRoute } from "./SetupMockRoute";

describe("SetupMockRoute", () => {
  it("switches into qr and vscode remote presets without backend data", async () => {
    render(<SetupMockRoute />);

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "扫码等待中" }));
    expect(await screen.findByText("扫码创建飞书应用")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "VS Code 远程" }));
    expect(await screen.findByText("检测到你正在远程机器上完成设置")).toBeInTheDocument();
  });
});
