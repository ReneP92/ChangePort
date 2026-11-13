import { useAuth } from "@clerk/react";
import { useEffect } from "react";
import { Link, Navigate, Outlet, useLocation, useParams } from "react-router";
import { useCustomer, useMe, useTopics } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { defaultEnvironment, environmentPath, rememberEnvironment } from "@/lib/environments";
import { onboardingDone, shouldWelcome } from "@/lib/onboarding";
import { CustomerArea } from "@/pages/CustomerArea";
import { Overview } from "@/pages/overview/Overview";

/**
 * The pages of one of the organization's environments, under
 * /e/<environment>/: the environment must be the organization's, and the
 * viewer starts in it next time.
 */
export function EnvironmentLayout() {
  const { environment = "" } = useParams();
  const organization = useAuth().orgId ?? "none";
  const me = useMe();
  const known = me.data?.environments?.some((e) => e.name === environment) ?? false;
  useEffect(() => {
    if (known) {
      rememberEnvironment(organization, environment);
    }
  }, [known, organization, environment]);
  if (me.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (me.isError) {
    return <Notice title="The console is unavailable">{me.error.message}</Notice>;
  }
  if (me.data.access !== "customer") {
    return <Navigate to="/" replace />;
  }
  if (!known) {
    const environments = me.data.environments ?? [];
    return (
      <main className="mx-auto flex max-w-xl flex-col gap-4 p-8">
        <h1 className="text-2xl font-semibold tracking-tight">No such environment</h1>
        <p className="text-muted-foreground">
          Your organization has no environment {environment}. Its environments are:
        </p>
        <ul className="flex flex-col gap-1">
          {environments.map((e) => (
            <li key={e.name}>
              <Link
                to={environmentPath(e.name, "/")}
                className="font-medium underline underline-offset-4"
              >
                {e.name}
              </Link>
            </li>
          ))}
        </ul>
      </main>
    );
  }
  return <Outlet />;
}

/**
 * An environment's overview, or its welcome page while its cluster is new:
 * not ready yet, or without topics, unless the viewer is done with it.
 */
export function EnvironmentHome() {
  const customer = useCustomer();
  const topics = useTopics(customer.data?.ready === true);
  if (customer.data && !onboardingDone(customer.data.id)) {
    if (shouldWelcome(false, customer.data.ready, topics.data)) {
      return <Navigate to="welcome" replace />;
    }
    if (topics.isPending) {
      // Not knowing yet whether the cluster has topics, it waits rather
      // than flash the overview.
      return <Notice title="Loading" busy />;
    }
  }
  return (
    <CustomerArea>
      {(customer, admin) => <Overview customer={customer} admin={admin} />}
    </CustomerArea>
  );
}

/**
 * A page from before environments, such as /topics/orders, or the home
 * page: the same page in the environment the viewer starts in.
 */
export function ToEnvironment() {
  const location = useLocation();
  const organization = useAuth().orgId ?? "none";
  const me = useMe();
  if (me.isPending) {
    return <Notice title="Loading" busy />;
  }
  const environment = defaultEnvironment(organization, me.data?.environments ?? []);
  if (me.data?.access !== "customer" || environment === undefined) {
    return <Navigate to="/" replace />;
  }
  return (
    <Navigate
      to={environmentPath(environment, location.pathname) + location.search + location.hash}
      replace
    />
  );
}
