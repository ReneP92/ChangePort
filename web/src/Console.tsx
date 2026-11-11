import { ClerkFailed, ClerkLoading, Show, SignIn, SignUp, UserButton } from "@clerk/react";
import { Link, NavLink, Route, Routes, useLocation } from "react-router";
import { ApiProvider } from "@/api/ApiProvider";
import { useAlerts, useMe, useTrialRequests } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { OrganizationButton } from "@/components/OrganizationButton";
import { Home } from "@/pages/Home";
import { AlertsPage } from "@/pages/alerts/AlertsPage";
import { Welcome } from "@/pages/welcome/Welcome";
import { ConsumerGroupPage } from "@/pages/consumer-groups/ConsumerGroupPage";
import { OperatorCustomer } from "@/pages/operator/OperatorCustomer";
import { OperatorCustomers } from "@/pages/operator/OperatorCustomers";
import { OperatorTrialRequests } from "@/pages/operator/OperatorTrialRequests";
import { NewSource } from "@/pages/sources/NewSource";
import { SourcePage } from "@/pages/sources/SourcePage";
import { Sources } from "@/pages/sources/Sources";
import { TopicPage } from "@/pages/topics/TopicPage";
import { TrialPage } from "@/pages/trial/TrialPage";
import { cn } from "@/lib/utils";

/** The console behind Clerk sign-in. */
export function Console() {
  return (
    <>
      <ClerkLoading>
        <Notice title="Changeport" busy />
      </ClerkLoading>
      {/* Clerk's script comes from Clerk; without it nobody can sign in. */}
      <ClerkFailed>
        <Notice title="Sign-in could not load">
          The console could not reach its sign-in service. Check your connection and reload the
          page.
        </Notice>
      </ClerkFailed>
      <Show when="signed-out">
        <SignedOut />
      </Show>
      <Show when="signed-in">
        <ApiProvider>
          <SignedIn />
        </ApiProvider>
      </Show>
    </>
  );
}

/**
 * Sign-in, or at /trial the sign-up of someone who wants a trial; each links
 * to the other. After signing up, the trial page asks what they need.
 */
function SignedOut() {
  const trial = useLocation().pathname === "/trial";
  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-8 p-8">
      <div className="flex flex-col items-center gap-2 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">
          {trial ? "Try Changeport" : "Changeport"}
        </h1>
        {trial && (
          <p className="text-balance text-muted-foreground">
            Sign up, then tell us what you want Kafka for.
          </p>
        )}
      </div>
      {trial ? (
        <SignUp routing="hash" signInUrl="/" forceRedirectUrl="/trial" />
      ) : (
        <SignIn routing="hash" signUpUrl="/trial" />
      )}
    </main>
  );
}

function SignedIn() {
  return (
    <div className="min-h-svh">
      <header className="border-b">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-4 gap-y-2 px-6 py-3">
          <Link to="/" className="flex shrink-0 items-center gap-2 font-semibold tracking-tight">
            <img src="/favicon.svg" alt="" className="size-6" />
            {/* On phones the mark alone leaves room for the organization's name. */}
            <span className="max-sm:sr-only">Changeport</span>
          </Link>
          <CustomerNav />
          <OperatorNav />
          <div className="ml-auto flex min-w-0 items-center gap-3">
            <OrganizationButton />
            <UserButton />
          </div>
        </div>
      </header>
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/welcome" element={<Welcome />} />
        <Route path="/alerts" element={<AlertsPage />} />
        <Route path="/topics/:name" element={<TopicPage />} />
        <Route path="/consumer-groups/:group" element={<ConsumerGroupPage />} />
        <Route path="/sources" element={<Sources />} />
        <Route path="/sources/new" element={<NewSource />} />
        <Route path="/sources/:name" element={<SourcePage />} />
        <Route path="/trial" element={<TrialPage />} />
        <Route path="/operator" element={<OperatorCustomers />} />
        <Route path="/operator/trials" element={<OperatorTrialRequests />} />
        <Route path="/operator/customers/:customerID" element={<OperatorCustomer />} />
        <Route path="*" element={<Notice title="There is no such page" />} />
      </Routes>
    </div>
  );
}

const navLink = ({ isActive }: { isActive: boolean }) =>
  cn(
    "rounded-md px-2 py-1 text-sm transition-colors hover:text-foreground",
    isActive ? "font-medium text-foreground" : "text-muted-foreground",
  );

/** The pages of a customer's console, for its members. */
function CustomerNav() {
  const me = useMe();
  if (me.data?.access !== "customer") {
    return null;
  }
  return (
    // On phones the links get a row of their own, under the organization.
    <nav
      aria-label="Console"
      className="flex items-center gap-1 max-sm:order-last max-sm:-ml-2 max-sm:w-full"
    >
      <NavLink to="/" end className={navLink}>
        Cluster
      </NavLink>
      <NavLink to="/sources" className={navLink}>
        Sources
      </NavLink>
      <NavLink to="/alerts" className={navLink}>
        Alerts
        <AlertCount />
      </NavLink>
    </nav>
  );
}

/** The pages of the operator console, for operators. */
function OperatorNav() {
  const me = useMe();
  if (me.data?.access !== "operator") {
    return null;
  }
  return (
    <nav
      aria-label="Operator console"
      className="flex items-center gap-1 max-sm:order-last max-sm:-ml-2 max-sm:w-full"
    >
      <NavLink to="/operator" end className={navLink}>
        Customers
      </NavLink>
      <NavLink to="/operator/trials" className={navLink}>
        Trial requests
        <PendingTrialCount />
      </NavLink>
    </nav>
  );
}

/** How many trial requests wait for a decision; nothing without. */
function PendingTrialCount() {
  const requests = useTrialRequests();
  const count = requests.data?.filter((request) => request.phase === "Pending").length ?? 0;
  return <Count count={count} label="waiting" />;
}

/** How many alerts are active, next to the Alerts link; nothing without. */
function AlertCount() {
  const alerts = useAlerts();
  return <Count count={alerts.data?.length ?? 0} label="active" />;
}

/** A count in a pill next to a link, read out with its label. */
function Count({ count, label }: { count: number; label: string }) {
  if (count === 0) {
    return null;
  }
  return (
    <>
      <span
        aria-hidden
        className="ml-1.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[0.65rem] font-medium text-white tabular-nums"
      >
        {count}
      </span>
      <span className="sr-only">
        , {count} {label}
      </span>
    </>
  );
}
