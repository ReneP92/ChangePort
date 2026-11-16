import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { configQuery, type ConsoleConfig } from "@/api/config";

/** The customer docs' address in tests. */
export const testDocsUrl = "https://changeport.test/docs";

/**
 * Children with the console's configuration already known, as after it is
 * fetched, for tests of what reads it, such as links to the docs.
 */
export function withConfig(
  children: ReactNode,
  config: ConsoleConfig = { state: "ready", publishableKey: "pk_test", docsUrl: testDocsUrl },
) {
  const client = new QueryClient();
  client.setQueryData(configQuery.queryKey, config);
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
