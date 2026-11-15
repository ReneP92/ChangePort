import { ClerkProvider } from "@clerk/react";
import { ui } from "@clerk/ui";
import { shadcn } from "@clerk/ui/themes";
import type { ComponentProps } from "react";
import { useQuery } from "@tanstack/react-query";
import { configQuery } from "@/api/config";
import { Console } from "@/Console";
import { Notice } from "@/components/Notice";

/**
 * Clerk's shadcn theme, which takes its colors from the console's own
 * variables, light or dark. @clerk/ui types its optional cssLayerName more
 * loosely than @clerk/react, which exactOptionalPropertyTypes tells apart.
 */
const appearance = { theme: shadcn } as NonNullable<
  ComponentProps<typeof ClerkProvider>["appearance"]
>;

/**
 * The Changeport console. It learns the Clerk instance from the console's
 * API, so one build serves every environment, and fails closed: without a
 * configuration there is nothing to sign in to. Clerk's components are
 * bundled rather than loaded from Clerk's CDN at sign-in, where one hung
 * request left the sign-in form blank.
 */
export function App() {
  const config = useQuery(configQuery);
  if (config.isPending) {
    return <Notice title="Changeport" busy />;
  }
  if (config.isError) {
    return (
      <Notice title="The console is unavailable">
        It could not reach its server. Try again in a moment.
      </Notice>
    );
  }
  if (config.data.state === "not-configured") {
    return (
      <Notice title="The console is not configured">
        It has no sign-in configuration yet, so it refuses every request. Operators set it up with
        just console-config.
      </Notice>
    );
  }
  return (
    // Clerk's sign-in and profile take the console's colors, light or dark.
    <ClerkProvider
      publishableKey={config.data.publishableKey}
      ui={ui}
      appearance={appearance}
      afterSignOutUrl="/"
    >
      <Console />
    </ClerkProvider>
  );
}
