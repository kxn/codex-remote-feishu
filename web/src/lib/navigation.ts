export function navigateToLocalPath(target: string) {
  try {
    window.location.assign(target);
    return;
  } catch {
    // Fallback for restricted environments where assign is unavailable.
  }
  try {
    window.history.replaceState({}, "", target);
  } catch {
    // Ignore fallback failures and keep the current page usable.
  }
}
