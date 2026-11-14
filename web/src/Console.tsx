import { ClerkFailed, ClerkLoading, Show, SignIn, SignUp } from "@clerk/react";
import { Route, Routes, useLocation } from "react-router";
import { ApiProvider } from "@/api/ApiProvider";
import { ConsoleSidebar, MobileTopBar } from "@/components/ConsoleSidebar";
import { Notice } from "@/components/Notice";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { EnvironmentHome, EnvironmentLayout, ToEnvironment } from "@/pages/EnvironmentRoutes";
import { Home } from "@/pages/Home";
import { AlertsPage } from "@/pages/alerts/AlertsPage";
import { EnvironmentsPage } from "@/pages/environments/EnvironmentsPage";
import { TeamPage } from "@/pages/team/TeamPage";
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
    <TooltipProvider>
      <SidebarProvider defaultOpen={sidebarOpen()}>
        <ConsoleSidebar />
        {/* As a flex item the page would otherwise grow to its widest
            content, such as a table that scrolls on its own, past a phone. */}
        <SidebarInset className="min-w-0">
          <MobileTopBar />
          {/* Pages centre their main with auto margins, which in the inset's
              flex column would size it to its content rather than the
              window: a plain block gives them the window's width. */}
          <div className="min-w-0 flex-1">
            <Routes>
              <Route path="/" element={<Home />} />
              {/* Each of an organization's environments, a cluster of its own. */}
              <Route path="/e/:environment" element={<EnvironmentLayout />}>
                <Route index element={<EnvironmentHome />} />
                <Route path="welcome" element={<Welcome />} />
                <Route path="alerts" element={<AlertsPage />} />
                <Route path="topics/:name" element={<TopicPage />} />
                <Route path="consumer-groups/:group" element={<ConsumerGroupPage />} />
                <Route path="sources" element={<Sources />} />
                <Route path="sources/new" element={<NewSource />} />
                <Route path="sources/:name" element={<SourcePage />} />
              </Route>
              <Route path="/team" element={<TeamPage />} />
              <Route path="/environments" element={<EnvironmentsPage />} />
              {/* Links from before environments open in the default one. */}
              {[
                "/welcome",
                "/alerts",
                "/topics/:name",
                "/consumer-groups/:group",
                "/sources",
                "/sources/new",
                "/sources/:name",
              ].map((path) => (
                <Route key={path} path={path} element={<ToEnvironment />} />
              ))}
              <Route path="/trial" element={<TrialPage />} />
              <Route path="/operator" element={<OperatorCustomers />} />
              <Route path="/operator/trials" element={<OperatorTrialRequests />} />
              <Route path="/operator/customers/:customerID" element={<OperatorCustomer />} />
              <Route path="*" element={<Notice title="There is no such page" />} />
            </Routes>
          </div>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}

/**
 * Whether the sidebar was open when the viewer last left it, which the
 * sidebar keeps in a cookie: open unless collapsed.
 */
function sidebarOpen(): boolean {
  return !document.cookie.split("; ").includes("sidebar_state=false");
}
