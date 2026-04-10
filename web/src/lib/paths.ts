function isAbsoluteURL(value: string): boolean {
  return /^[a-z][a-z\d+\-.]*:/i.test(value) || value.startsWith("//");
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
