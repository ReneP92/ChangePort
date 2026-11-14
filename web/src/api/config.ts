import { useQuery } from "@tanstack/react-query";
import createClient from "openapi-fetch";
import { ApiError } from "@/api/client";
import type { paths } from "@/api/schema";

/** What the web app learns from the console before anyone signs in. */
export type ConsoleConfig =
  | { state: "ready"; publishableKey: string; supportEmail?: string | undefined }
  // The console has no Clerk configuration, so it refuses everything.
  | { state: "not-configured" };

/** Fetches the console's public configuration. */
export async function fetchConfig(): Promise<ConsoleConfig> {
  const api = createClient<paths>({ baseUrl: window.location.origin });
  const { data, error, response } = await api.GET("/api/config");
  if (data) {
    return {
      state: "ready",
      publishableKey: data.clerkPublishableKey,
      supportEmail: data.supportEmail,
    };
  }
  if (response.status === 503) {
    return { state: "not-configured" };
  }
  throw new ApiError(response.status, error);
}

/** The query of the console's configuration, fetched once. */
export const configQuery = {
  queryKey: ["config"],
  queryFn: fetchConfig,
  staleTime: Infinity,
  retry: 1,
};

/** Where customers contact support, when the console names an address. */
export function useSupportEmail(): string | undefined {
  const config = useQuery(configQuery).data;
  return config?.state === "ready" ? config.supportEmail : undefined;
}
