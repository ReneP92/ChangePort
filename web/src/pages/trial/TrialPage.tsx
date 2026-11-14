import { useClerk } from "@clerk/react";
import { useEffect, useRef, useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { errorText, type TrialRequest } from "@/api/client";
import { useMe, useMyTrialRequests, useRequestTrial } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { Page } from "@/components/Page";
import { Button } from "@/components/ui/button";
import { askAgainFrom } from "@/lib/trials";
import { TrialForm } from "@/pages/trial/TrialForm";
import { DeclinedRequest, PendingRequest } from "@/pages/trial/TrialStatus";

/**
 * Where a signed-in user without a cluster requests a trial and follows
 * their request until an operator decides; once approved, it takes them to
 * their new cluster.
 */
export function TrialPage() {
  const me = useMe();
  if (me.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (me.isError) {
    return <Notice title="The console is unavailable">{me.error.message}</Notice>;
  }
  if (me.data.access === "customer") {
    return <Navigate to="/" replace />;
  }
  if (me.data.access === "operator") {
    return (
      <Notice title="Operators review trials">
        Customers&apos; requests are under{" "}
        <Link to="/operator/trials" className="underline underline-offset-4">
          Trial requests
        </Link>
        . To request one yourself, switch away from the staff organization.
      </Notice>
    );
  }
  return <RequesterTrial />;
}

function RequesterTrial() {
  const mine = useMyTrialRequests();
  const requestTrial = useRequestTrial();
  if (mine.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (mine.isError) {
    return <Notice title="The console is unavailable">{errorText(mine.error)}</Notice>;
  }
  const latest = mine.data.at(0);
  if (latest?.phase === "Approved") {
    return <OpenCluster request={latest} />;
  }
  const again = latest ? askAgainFrom(latest) : undefined;
  const mayAsk = !latest || (latest.phase === "Declined" && (!again || new Date() >= again));
  const title =
    latest?.phase === "Pending"
      ? "Your request is in"
      : latest?.phase === "Declined"
        ? "Your request was declined"
        : "Request a trial";
  const ask =
    "Tell us about your team and what you want Kafka for. An operator reviews each request and sets up a dedicated Kafka cluster for you.";
  return (
    <Page title={title} description={latest ? undefined : ask} className="gap-10 [&>*]:max-w-xl">
      {latest?.phase === "Pending" && <PendingRequest request={latest} />}
      {latest?.phase === "Declined" && <DeclinedRequest request={latest} />}
      {mayAsk && (
        <section aria-label={latest ? "Ask again" : title} className="flex flex-col gap-6">
          {latest && (
            <div className="flex flex-col gap-2">
              <h2 className="text-base font-semibold">Ask again</h2>
              <p className="text-sm text-muted-foreground">{ask}</p>
            </div>
          )}
          <TrialForm
            pending={requestTrial.isPending}
            error={requestTrial.error}
            onSubmit={(request) => {
              requestTrial.mutate(request);
            }}
          />
        </section>
      )}
    </Page>
  );
}

/**
 * An approved request: switches the user to their new organization, whose
 * admin they are, and opens its cluster, which shows how far it is.
 */
function OpenCluster({ request }: { request: TrialRequest }) {
  const { setActive } = useClerk();
  const navigate = useNavigate();
  const [failed, setFailed] = useState(false);
  const switched = useRef(false);
  const organization = request.organizationID;
  useEffect(() => {
    if (!organization || switched.current) {
      return;
    }
    switched.current = true;
    setActive({ organization }).then(
      () => navigate("/", { replace: true }),
      () => {
        setFailed(true);
      },
    );
  }, [organization, setActive, navigate]);
  if (organization && !failed) {
    return (
      <Notice title="Your trial is approved" busy>
        Opening your Kafka cluster.
      </Notice>
    );
  }
  return (
    <Page
      title="Your trial is approved"
      description={`Your cluster belongs to the organization ${request.company}, which could not be opened.`}
    >
      <div>
        <Button
          onClick={() => {
            window.location.assign("/");
          }}
        >
          Open your cluster
        </Button>
      </div>
    </Page>
  );
}
