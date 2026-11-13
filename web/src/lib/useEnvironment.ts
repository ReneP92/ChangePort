import { useAuth } from "@clerk/react";
import { useLocation } from "react-router";
import { useMe } from "@/api/queries";
import { defaultEnvironment, environmentFromPath, environmentPath } from "@/lib/environments";

/**
 * The environment the page shows: the one in its URL, or on a page of the
 * whole organization, such as its team, the one the viewer starts in. It
 * reads the URL rather than the route, so that the header has it too.
 */
export function useEnvironment(): string | undefined {
  const { pathname } = useLocation();
  const organization = useAuth().orgId ?? "none";
  const me = useMe();
  return (
    environmentFromPath(pathname) ?? defaultEnvironment(organization, me.data?.environments ?? [])
  );
}

/** Paths of pages in the environment the page shows, for links. */
export function useEnvironmentPath(): (page: string) => string {
  const environment = useEnvironment();
  return (page) => (environment === undefined ? page : environmentPath(environment, page));
}
