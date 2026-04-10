import { describe, expect, it, vi } from "vitest";
import { requestJSON } from "./api";

describe("api helpers", () => {
  it("normalizes root-based local requests to dot-relative fetch paths", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
        },
      }),
    );

    await requestJSON<{ ok: boolean }>("/api/admin/bootstrap-state");

    expect(fetchMock).toHaveBeenCalledWith(
      "./api/admin/bootstrap-state",
      expect.objectContaining({
        credentials: "same-origin",
      }),
    );
  });
});
