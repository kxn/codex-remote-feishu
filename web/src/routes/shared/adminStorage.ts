type AdminStorageCleanupOptions<TResponse> = {
  busyKey: string;
  setActionBusy: (value: string) => void;
  request: () => Promise<TResponse>;
  onSuccess: (response: TResponse) => void;
  onError: () => void;
};

export async function runAdminStorageCleanup<TResponse>(
  options: AdminStorageCleanupOptions<TResponse>,
) {
  const { busyKey, setActionBusy, request, onSuccess, onError } = options;
  setActionBusy(busyKey);
  try {
    const response = await request();
    onSuccess(response);
  } catch {
    onError();
  } finally {
    setActionBusy("");
  }
}
