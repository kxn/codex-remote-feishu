import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { App } from "./app";

vi.mock("./routes/SetupRoute", () => ({
  SetupRoute: () => <h1>Mock Setup Route</h1>,
}));

vi.mock("./routes/AdminRoute", () => ({
  AdminRoute: () => <h1>Mock Admin Route</h1>,
}));

describe("App", () => {
  it("renders the setup route when mounted under a prefixed setup path", () => {
    window.history.replaceState({}, "", "/g/demo/setup");

    render(<App />);

    expect(screen.getByRole("heading", { name: "Mock Setup Route" })).toBeInTheDocument();
  });

  it("renders the admin route outside setup paths", () => {
    window.history.replaceState({}, "", "/g/demo/admin");

    render(<App />);

    expect(screen.getByRole("heading", { name: "Mock Admin Route" })).toBeInTheDocument();
  });
});
