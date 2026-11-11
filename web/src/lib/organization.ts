import { useAuth, useOrganizationList } from "@clerk/react";
import { useEffect } from "react";

/**
 * Where the signed-in user stands with organizations: a user belongs to
 * one, their company's, which this makes their active one when it is not
 * yet, as after signing in. Several is a mistake to report, never to
 * choose from.
 */
export type OrganizationState = "loading" | "active" | "activating" | "none" | "several";

export function useOrganizationState(): OrganizationState {
  const { orgId, isLoaded: authLoaded } = useAuth();
  const { isLoaded, userMemberships, setActive } = useOrganizationList({ userMemberships: true });
  const memberships = userMemberships.data;
  const only = memberships?.length === 1 ? memberships[0]?.organization.id : undefined;
  useEffect(() => {
    if (authLoaded && !orgId && only && setActive) {
      void setActive({ organization: only });
    }
  }, [authLoaded, orgId, only, setActive]);
  if (!authLoaded) {
    return "loading";
  }
  if (orgId) {
    return "active";
  }
  if (!isLoaded || userMemberships.isLoading) {
    return "loading";
  }
  switch (memberships?.length ?? 0) {
    case 0:
      return "none";
    case 1:
      return "activating";
    default:
      return "several";
  }
}
