function isAbsoluteURL(value: string): boolean {
  return /^[a-z][a-z\d+\-.]*:/i.test(value) || value.startsWith("//");
}

function currentMountPrefix(pathname: string = window.location.pathname): string {
  const match = pathname.match(/^\/g\/[^/]+(?:\/|$)/);
  if (!match) {
    return "/";
  }
  return match[0].endsWith("/") ? match[0] : `${match[0]}/`;
}

function relativePathWithinCurrentMount(pathname: string): string | null {
  const mountPrefix = currentMountPrefix();
  if (mountPrefix === "/") {
    if (pathname === "/" || pathname === "") {
      return "./";
    }
    return `.${pathname}`;
  }

  const barePrefix = mountPrefix.slice(0, -1);
  if (pathname === barePrefix || pathname === mountPrefix) {
    return "./";
  }
  if (!pathname.startsWith(mountPrefix)) {
    return null;
  }
  return `./${pathname.slice(mountPrefix.length)}`;
}

export function relativeLocalPath(value: string): string {
  const trimmed = value.trim();
  if (trimmed === "" || trimmed === "/") {
    return "./";
  }
  if (trimmed.startsWith("./") || trimmed.startsWith("../")) {
    return trimmed;
  }
  if (!trimmed.startsWith("/") && !isAbsoluteURL(trimmed)) {
    return `./${trimmed}`;
  }

  const parsed = new URL(trimmed, window.location.href);
  if (parsed.origin === window.location.origin) {
    const relativeWithinMount = relativePathWithinCurrentMount(parsed.pathname);
    if (relativeWithinMount !== null) {
      return `${relativeWithinMount}${parsed.search}${parsed.hash}`;
    }
  }

  const suffix = `${parsed.pathname}${parsed.search}${parsed.hash}`;
  if (suffix === "/" || suffix === "") {
    return "./";
  }
  return `.${suffix}`;
}

export function currentRouteIsSetup(pathname: string = window.location.pathname): boolean {
  const normalized = pathname.replace(/\/+$/, "");
  return normalized.endsWith("/setup");
}

export function currentRouteIsSetupMock(pathname: string = window.location.pathname): boolean {
  const normalized = pathname.replace(/\/+$/, "");
  return normalized.endsWith("/setup-mock");
}
