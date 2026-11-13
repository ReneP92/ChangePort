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
      <main className="mx-auto flex max-w-xl flex-col gap-4 p-8">
        <h1 className="text-2xl font-semibold tracking-tight">No Kafka cluster yet</h1>
        <p className="text-muted-foreground">Your organization has no Changeport cluster.</p>
        <TrialLink />
      </main>
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
        <main className="mx-auto flex max-w-xl flex-col gap-4 p-8">
          <h1 className="text-2xl font-semibold tracking-tight">No organization yet</h1>
          <p className="text-muted-foreground">
            Your Kafka cluster belongs to your company&apos;s organization. Ask a colleague for an
            invitation.
          </p>
          <TrialLink />
        </main>
      );
  }
}

/** Where someone without a cluster yet asks for one. */
function TrialLink() {
  return (
    <p className="mt-4 border-t pt-4 text-muted-foreground">
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
