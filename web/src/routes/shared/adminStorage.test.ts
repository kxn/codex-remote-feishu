import { describe, expect, it, vi } from "vitest";
import { runAdminStorageCleanup } from "./adminStorage";

describe("admin storage cleanup helper", () => {
  it("wraps cleanup requests with busy state and success handling", async () => {
    const setActionBusy = vi.fn();
    const onSuccess = vi.fn();
    const onError = vi.fn();

    await runAdminStorageCleanup({
      busyKey: "cleanup-logs",
      setActionBusy,
      request: async () => ({ remainingFileCount: 3 }),
      onSuccess,
      onError,
    });

    expect(setActionBusy).toHaveBeenNthCalledWith(1, "cleanup-logs");
    expect(setActionBusy).toHaveBeenNthCalledWith(2, "");
    expect(onSuccess).toHaveBeenCalledWith({ remainingFileCount: 3 });
    expect(onError).not.toHaveBeenCalled();
  });

  it("clears busy state and reports error when cleanup fails", async () => {
    const setActionBusy = vi.fn();
    const onSuccess = vi.fn();
    const onError = vi.fn();

    await runAdminStorageCleanup({
      busyKey: "cleanup-preview",
      setActionBusy,
      request: async () => {
        throw new Error("boom");
      },
      onSuccess,
      onError,
    });

    expect(setActionBusy).toHaveBeenNthCalledWith(1, "cleanup-preview");
    expect(setActionBusy).toHaveBeenNthCalledWith(2, "");
    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(1);
  });
});
