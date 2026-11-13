import type { Environment } from "@/api/client";

/**
 * An organization has a cluster per environment, such as production and
 * staging, and the console shows one at a time, under /e/<environment>/ in
 * its URL. Customer routes of the API act on the environment the request's
 * Changeport-Environment header names, which the API client takes from the
 * URL.
 */
export const environmentHeader = "Changeport-Environment";

/** The environment a path is in, such as staging for /e/staging/topics/x. */
export function environmentFromPath(pathname: string): string | undefined {
  const match = /^\/e\/([^/]+)/.exec(pathname);
  return match?.[1] === undefined ? undefined : decodeURIComponent(match[1]);
}

/**
 * The header naming the environment the page is in, for requests the API
 * client does not send, such as event streams.
 */
export function environmentHeaders(): Record<string, string> {
  const environment = environmentFromPath(window.location.pathname);
  return environment === undefined ? {} : { [environmentHeader]: environment };
}

/** The path of page in environment, such as /e/staging/topics/x. */
export function environmentPath(environment: string, page: string): string {
  const prefix = `/e/${encodeURIComponent(environment)}`;
  return page === "/" || page === "" ? prefix : prefix + page;
}

function storageKey(organization: string): string {
  return `changeport.environment.${organization}`;
}

/** Remembers in this browser the environment the viewer last used. */
export function rememberEnvironment(organization: string, environment: string) {
  try {
    localStorage.setItem(storageKey(organization), environment);
  } catch {
    // Storage can be off, as in a private window.
  }
}

/**
 * The environment a viewer starts in: the one they last used in this
 * browser, while the organization still has it, or else the organization's
 * default, the first.
 */
export function defaultEnvironment(
  organization: string,
  environments: Environment[],
): string | undefined {
  let remembered: string | null = null;
  try {
    remembered = localStorage.getItem(storageKey(organization));
  } catch {
    // As if nothing were remembered.
  }
  if (remembered !== null && environments.some((e) => e.name === remembered)) {
    return remembered;
  }
  return environments[0]?.name;
}
