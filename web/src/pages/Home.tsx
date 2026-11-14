import { Link, Navigate } from "react-router";
import { useMe } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { useOrganizationState } from "@/lib/organization";
import { ToEnvironment } from "@/pages/EnvironmentRoutes";

/** Where a signed-in user lands: their customer, or the operator console. */
export function Home() {
  const me = useMe();
  if (me.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (me.isError) {
    return <Notice title="The console is unavailable">{me.error.message}</Notice>;
  }
  switch (me.data.access) {
    case "operator":
      return <Navigate to="/operator" replace />;
    case "customer":
      return <ToEnvironment />;
    case "several":
      return <SeveralOrganizations />;
    default:
      return <NoCustomer organization={me.data.organizationID !== undefined} />;
  }
}

/**
 * A user who sees no cluster: their organization has none, such as one
 * deprovisioned, or they belong to none yet. A user belongs to one
 * organization, which becomes their active one by itself.
 */
function NoCustomer({ organization }: { organization: boolean }) {
  const state = useOrganizationState();
  if (organization) {
    return (
      <Notice title="No Kafka cluster yet" footer={<TrialLink />}>
        Your organization has no Changeport cluster.
      </Notice>
    );
  }
  switch (state) {
    case "loading":
    case "activating":
    case "active":
      return <Notice title="Loading" busy />;
    case "several":
      return <SeveralOrganizations />;
    case "none":
      return (
        <Notice title="No organization yet" footer={<TrialLink />}>
          Your Kafka cluster belongs to your company&apos;s organization. Ask a colleague for an
          invitation.
        </Notice>
      );
  }
}

/** Where someone without a cluster yet asks for one. */
function TrialLink() {
  return (
    <p className="mt-3 border-t pt-4 text-muted-foreground">
      New to Changeport?{" "}
      <Link to="/trial" className="font-medium text-foreground underline underline-offset-4">
        Request a trial
      </Link>
    </p>
  );
}

/** A person belongs to one organization; one in several sees nothing. */
function SeveralOrganizations() {
  return (
    <Notice title="Your account belongs to several organizations">
      A Changeport account belongs to one organization, its company&apos;s. Ask Changeport support
      to remove it from the others.
    </Notice>
  );
}
