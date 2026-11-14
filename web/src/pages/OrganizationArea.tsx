import { useOrganization } from "@clerk/react";
import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { useMe } from "@/api/queries";
import { Notice } from "@/components/Notice";

/**
 * Shows a page about the whole of the active organization, such as its team
 * or its environments, with whether the user is its admin and the
 * organization's name. It needs no cluster of an environment to be ready.
 * Everyone else goes home, which says what they can do.
 */
export function OrganizationArea({
  children,
}: {
  children: (admin: boolean, organization: string) => ReactNode;
}) {
  const me = useMe();
  const { organization } = useOrganization();
  if (me.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (me.isError) {
    return <Notice title="The console is unavailable">{me.error.message}</Notice>;
  }
  if (me.data.access !== "customer") {
    return <Navigate to="/" replace />;
  }
  return children(me.data.admin ?? false, organization?.name ?? "Your organization");
}
