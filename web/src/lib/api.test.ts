import { afterEach, describe, expect, it, vi } from "vitest";
import { requestJSON } from "./api";

describe("api helpers", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

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

  it("times out requests when timeoutMs is provided", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockImplementation((_input, init) => {
      return new Promise<Response>((_resolve, reject) => {
        const signal = init?.signal as AbortSignal | undefined;
        signal?.addEventListener(
          "abort",
          () => reject(new DOMException("The operation was aborted.", "AbortError")),
          { once: true },
        );
      });
    });

    const request = expect(
      requestJSON<{ ok: boolean }>(
        "/api/admin/bootstrap-state",
        undefined,
        { timeoutMs: 1_000 },
      ),
    ).rejects.toMatchObject({
      code: "request_timeout",
      status: 408,
    });

    await vi.advanceTimersByTimeAsync(1_000);
    await request;
  });
});
