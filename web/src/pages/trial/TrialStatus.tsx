import type { TrialRequest } from "@/api/client";
import { askAgainFrom, formatDay, retentionLabel, throughputLabel } from "@/lib/trials";

/** A pending request: that it is in, and what it asked for. */
export function PendingRequest({ request }: { request: TrialRequest }) {
  return (
    <section aria-labelledby="pending-title" className="flex flex-col gap-4">
      <h1 id="pending-title" className="text-2xl font-semibold tracking-tight">
        Your request is in
      </h1>
      <p className="text-muted-foreground">
        An operator reviews it and sets up your Kafka cluster. This page moves on by itself once
        they have decided.
      </p>
      <RequestSummary request={request} />
    </section>
  );
}

/**
 * A declined request: the operator's reason, and when the user may ask
 * again, if they may not yet.
 */
export function DeclinedRequest({
  request,
  now = new Date(),
}: {
  request: TrialRequest;
  now?: Date;
}) {
  const again = askAgainFrom(request);
  const waiting = again !== undefined && now < again;
  return (
    <section aria-labelledby="declined-title" className="flex flex-col gap-4">
      <h1 id="declined-title" className="text-2xl font-semibold tracking-tight">
        Your request was declined
      </h1>
      {request.reason && (
        <blockquote className="border-l-2 pl-4 whitespace-pre-line">{request.reason}</blockquote>
      )}
      {waiting && (
        <p className="text-muted-foreground">You may ask again from {formatDay(again)}.</p>
      )}
    </section>
  );
}

/** What a request asked for. */
export function RequestSummary({ request }: { request: TrialRequest }) {
  const rows: [string, string][] = [
    ["Company", request.company],
    ["Your role", request.role],
    ["Peak throughput", throughputLabel(request.throughput)],
    ["Messages kept", retentionLabel(request.retention)],
    ["Start", request.preferredStart ? formatDay(request.preferredStart) : "As soon as possible"],
    ["Requested", formatDay(new Date(request.createdAt))],
  ];
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 rounded-lg border p-4 text-sm">
      {rows.map(([term, value]) => (
        <div key={term} className="contents">
          <dt className="text-muted-foreground">{term}</dt>
          <dd className="min-w-0 break-words">{value}</dd>
        </div>
      ))}
      <dt className="col-span-2 text-muted-foreground">What you will use Kafka for</dt>
      <dd className="col-span-2 whitespace-pre-line break-words">{request.useCase}</dd>
    </dl>
  );
}
