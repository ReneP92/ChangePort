import { Link } from "react-router";
import { errorText, type TrialRequest } from "@/api/client";
import { useTrialRequests } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { Badge } from "@/components/ui/badge";
import { formatDay, retentionLabel, throughputLabel } from "@/lib/trials";
import { OperatorsOnly } from "@/pages/operator/OperatorCustomers";
import { ApproveTrialDialog, DeclineTrialDialog } from "@/pages/operator/TrialDecisionDialogs";

/** Every trial request, the pending ones first, for operators to decide. */
export function OperatorTrialRequests() {
  return (
    <OperatorsOnly>
      <TrialRequestList />
    </OperatorsOnly>
  );
}

function TrialRequestList() {
  const requests = useTrialRequests();
  if (requests.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (requests.isError) {
    return <Notice title="The console is unavailable">{errorText(requests.error)}</Notice>;
  }
  const pending = requests.data.filter((request) => request.phase === "Pending");
  const decided = requests.data.filter((request) => request.phase !== "Pending");
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-8 p-6">
      <h1 className="text-2xl font-semibold tracking-tight">Trial requests</h1>
      <section aria-labelledby="pending-requests" className="flex flex-col gap-3">
        <h2 id="pending-requests" className="text-sm font-medium text-muted-foreground">
          Waiting for a decision
        </h2>
        {pending.length === 0 ? (
          <p className="text-muted-foreground">No request waits for a decision.</p>
        ) : (
          <ul className="flex flex-col gap-3">
            {pending.map((request) => (
              <TrialRequestItem key={request.id} request={request} />
            ))}
          </ul>
        )}
      </section>
      {decided.length > 0 && (
        <section aria-labelledby="decided-requests" className="flex flex-col gap-3">
          <h2 id="decided-requests" className="text-sm font-medium text-muted-foreground">
            Decided
          </h2>
          <ul className="flex flex-col gap-3">
            {decided.map((request) => (
              <TrialRequestItem key={request.id} request={request} />
            ))}
          </ul>
        </section>
      )}
    </main>
  );
}

/** One request: who asked for what, and its decision or the actions. */
export function TrialRequestItem({ request }: { request: TrialRequest }) {
  return (
    <li
      aria-label={`${request.company}, ${request.id}`}
      className="flex flex-col gap-4 rounded-lg border p-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{request.company}</span>
            <PhaseBadge request={request} />
          </div>
          {/* On phones the address and the role take a line each. */}
          <span className="flex flex-col text-sm text-muted-foreground sm:flex-row sm:gap-1.5">
            <span className="[overflow-wrap:anywhere]">{request.email}</span>
            <span aria-hidden className="max-sm:hidden">
              ·
            </span>
            <span>{request.role}</span>
          </span>
        </div>
        {request.phase === "Pending" && (
          <div className="flex shrink-0 gap-2">
            <DeclineTrialDialog request={request} />
            <ApproveTrialDialog request={request} />
          </div>
        )}
      </div>
      <p className="text-sm whitespace-pre-line break-words">{request.useCase}</p>
      <dl className="flex flex-wrap gap-x-6 gap-y-1 text-sm">
        <Detail term="Peak throughput" value={throughputLabel(request.throughput)} />
        <Detail term="Messages kept" value={retentionLabel(request.retention)} />
        <Detail
          term="Start"
          value={request.preferredStart ? formatDay(request.preferredStart) : "As soon as possible"}
        />
        <Detail term="Requested" value={formatDay(new Date(request.createdAt))} />
        <Detail term="ID" value={request.id} />
      </dl>
      <Decision request={request} />
    </li>
  );
}

function PhaseBadge({ request }: { request: TrialRequest }) {
  switch (request.phase) {
    case "Approved":
      return <Badge>Approved</Badge>;
    case "Declined":
      return <Badge variant="destructive">Declined</Badge>;
    default:
      return <Badge variant="secondary">Pending</Badge>;
  }
}

function Detail({ term, value }: { term: string; value: string }) {
  return (
    <div className="flex gap-1.5">
      <dt className="text-muted-foreground">{term}</dt>
      <dd>{value}</dd>
    </div>
  );
}

/** What an operator decided, with when. */
function Decision({ request }: { request: TrialRequest }) {
  const when = request.decidedAt ? ` on ${formatDay(new Date(request.decidedAt))}` : "";
  if (request.phase === "Approved" && request.customerID) {
    return (
      <p className="text-sm text-muted-foreground">
        Approved{when} as customer{" "}
        <Link
          to={`/operator/customers/${request.customerID}`}
          className="font-medium text-foreground underline-offset-4 hover:underline"
        >
          {request.customerID}
        </Link>
        .
      </p>
    );
  }
  if (request.phase === "Declined") {
    return (
      <div className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Declined{when}:</span>
        <blockquote className="border-l-2 pl-3 whitespace-pre-line">{request.reason}</blockquote>
      </div>
    );
  }
  return null;
}
