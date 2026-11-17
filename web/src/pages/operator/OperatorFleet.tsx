import {
  ArrowUpRight,
  CircleCheck,
  CircleDashed,
  CircleHelp,
  CircleX,
  Trash2,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router";
import type { FleetCluster, OperatorAlert } from "@/api/client";
import { useFleet } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Page } from "@/components/Page";
import { formatSince } from "@/lib/alerts";
import {
  brokersLine,
  countStates,
  fleetStateLabels,
  fleetStates,
  formatShare,
  partitionProblems,
  type FleetState,
} from "@/lib/fleet";
import { formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { OperatorsOnly } from "@/pages/operator/OperatorCustomers";

const looks: Record<FleetState, { icon: LucideIcon; className: string }> = {
  down: { icon: CircleX, className: "text-destructive" },
  degraded: { icon: TriangleAlert, className: "text-amber-700 dark:text-amber-500" },
  unknown: { icon: CircleHelp, className: "text-muted-foreground" },
  starting: { icon: CircleDashed, className: "text-muted-foreground" },
  deleting: { icon: Trash2, className: "text-muted-foreground" },
  healthy: { icon: CircleCheck, className: "text-emerald-700 dark:text-emerald-500" },
};

/** A cluster's state, as an icon and words, never color alone. */
export function FleetStateBadge({
  state,
  className,
}: {
  state: FleetState;
  className?: string | undefined;
}) {
  const look = looks[state];
  const Icon = look.icon;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-sm font-medium",
        look.className,
        className,
      )}
    >
      <Icon aria-hidden className="size-3.5 shrink-0" />
      {fleetStateLabels[state]}
    </span>
  );
}

/** Every customer cluster's health on one page, the worst first, for operators. */
export function OperatorFleet() {
  return (
    <OperatorsOnly>
      <FleetView />
    </OperatorsOnly>
  );
}

function FleetView() {
  const fleet = useFleet();
  if (fleet.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (fleet.isError) {
    return <Notice title="The console is unavailable">{fleet.error.message}</Notice>;
  }
  const { clusters, platformAlerts, kafkaVersion, checkedAt } = fleet.data;
  return (
    <Page
      title="Fleet"
      description={`Every customer's cluster, the worst first. Checked at ${formatTime(checkedAt)}, again every 30 seconds.`}
    >
      <StateSummary clusters={clusters} />
      <section aria-labelledby="platform" className="flex flex-col gap-2">
        <h2 id="platform" className="text-sm font-medium">
          The platform
        </h2>
        {platformAlerts.length === 0 ? (
          <p className="text-sm text-muted-foreground">No alerts about the platform itself.</p>
        ) : (
          <ul className="flex flex-col divide-y rounded-lg border px-3 text-sm">
            {platformAlerts.map((alert, index) => (
              <li key={index} className="py-2.5">
                <AlertLine alert={alert} />
                <span className="block text-muted-foreground">
                  Since {formatSince(alert.startsAt)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-labelledby="clusters" className="flex flex-col gap-2">
        <h2 id="clusters" className="text-sm font-medium">
          Clusters
        </h2>
        {clusters.length === 0 ? (
          <p className="text-sm text-muted-foreground">There are no customers yet.</p>
        ) : (
          <ClusterTable clusters={clusters} kafkaVersion={kafkaVersion} />
        )}
      </section>
    </Page>
  );
}

/**
 * How many clusters are in each state: always down, degraded and healthy,
 * and the others when there are any.
 */
function StateSummary({ clusters }: { clusters: FleetCluster[] }) {
  const counts = countStates(clusters);
  const shown = fleetStates.filter(
    (state) => counts[state] > 0 || state === "down" || state === "degraded" || state === "healthy",
  );
  return (
    <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
      {shown.map((state) => (
        <div key={state} className="flex flex-col gap-1 rounded-lg border px-3 py-2.5">
          <dt>
            <FleetStateBadge
              state={state}
              className={counts[state] === 0 ? "text-muted-foreground" : undefined}
            />
          </dt>
          <dd className="text-2xl font-semibold tabular-nums">{counts[state]}</dd>
        </div>
      ))}
    </dl>
  );
}

/** An operator alert: its summary, and its runbook in a new tab. */
function AlertLine({ alert }: { alert: OperatorAlert }) {
  return (
    <span className="[overflow-wrap:anywhere]">
      <span
        className={cn(
          "font-medium",
          alert.severity === "page" ? "text-destructive" : "text-amber-700 dark:text-amber-500",
        )}
      >
        {alert.severity === "page" ? "Page" : "Warning"}:
      </span>{" "}
      {alert.summary}
      {alert.runbook && (
        <>
          {" "}
          <a
            href={alert.runbook}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-0.5 whitespace-nowrap text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
          >
            Runbook
            <span className="sr-only"> for {alert.name} (opens in a new tab)</span>
            <ArrowUpRight aria-hidden className="size-3.5" />
          </a>
        </>
      )}
    </span>
  );
}

function ClusterTable({
  clusters,
  kafkaVersion,
}: {
  clusters: FleetCluster[];
  kafkaVersion: string;
}) {
  return (
    // Wide tables scroll sideways on phones instead of overflowing.
    <div className="relative -mx-1 overflow-x-auto px-1">
      <table className="w-full text-left text-sm [&_td]:px-3 [&_td]:align-top [&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th]:px-3 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0">
        <thead className="border-b whitespace-nowrap text-muted-foreground">
          <tr>
            <th className="py-2 font-medium">Cluster</th>
            <th className="py-2 font-medium">State</th>
            <th className="py-2 font-medium">Brokers</th>
            <th className="py-2 font-medium">Partitions</th>
            <th className="py-2 text-right font-medium">Fullest volume</th>
            <th className="py-2 font-medium">Kafka</th>
            <th className="py-2 font-medium">Alerts</th>
          </tr>
        </thead>
        <tbody>
          {clusters.map((cluster) => (
            <ClusterRow key={cluster.customer.id} cluster={cluster} kafkaVersion={kafkaVersion} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ClusterRow({ cluster, kafkaVersion }: { cluster: FleetCluster; kafkaVersion: string }) {
  const { customer } = cluster;
  const running = cluster.state !== "starting" && cluster.state !== "deleting";
  return (
    <tr aria-label={customer.displayName} className="border-b last:border-0">
      <td className="py-3 whitespace-nowrap">
        <Link
          to={`/operator/customers/${customer.id}`}
          className="font-medium underline-offset-4 hover:underline"
        >
          {customer.displayName}
        </Link>
        <div className="text-muted-foreground">
          {customer.id} · {customer.environment} · {customer.profile}
        </div>
      </td>
      <td className="py-3">
        {/* Reasons wrap below the state, never to a sliver. */}
        <div className="flex min-w-56 flex-col items-start gap-1">
          <FleetStateBadge state={cluster.state} />
          {cluster.reasons.length > 0 && (
            <ul className="text-muted-foreground">
              {cluster.reasons.map((reason, index) => (
                <li key={index}>{reason}</li>
              ))}
            </ul>
          )}
        </div>
      </td>
      <td className="py-3 whitespace-nowrap tabular-nums">
        {running ? <Measured value={brokersLine(cluster)} /> : <Absent />}
      </td>
      <td className="py-3 whitespace-nowrap tabular-nums">
        {running ? <Partitions cluster={cluster} /> : <Absent />}
      </td>
      <td className="py-3 text-right whitespace-nowrap tabular-nums">
        {cluster.volumeUsed === undefined ? (
          <span className="text-muted-foreground">Not reported</span>
        ) : (
          <span
            className={cn(
              cluster.volumeUsed >= 0.8 && "font-medium text-amber-700 dark:text-amber-500",
            )}
          >
            {formatShare(cluster.volumeUsed)}
          </span>
        )}
      </td>
      <td className="py-3 whitespace-nowrap">
        <Version cluster={cluster} kafkaVersion={kafkaVersion} />
      </td>
      <td className="py-3">
        <div className="min-w-64">
          <Alerts cluster={cluster} />
        </div>
      </td>
    </tr>
  );
}

/**
 * One customer's cluster in the fleet view, for its page in the operator
 * console: its state and why, and what it is made of; nothing while it
 * starts or is removed, which the page says already.
 */
export function ClusterHealthCard({ customerID }: { customerID: string }) {
  const fleet = useFleet();
  const cluster = fleet.data?.clusters.find((c) => c.customer.id === customerID);
  if (cluster?.state === "starting" || cluster?.state === "deleting") {
    return null;
  }
  return (
    <Card aria-busy={fleet.isPending}>
      <CardHeader>
        <CardTitle>
          <h2>Health</h2>
        </CardTitle>
        <CardDescription>
          {fleet.isPending
            ? "Loading"
            : !cluster
              ? "The console could not look at the fleet just now."
              : `As the fleet view sees it, at ${formatTime(fleet.data?.checkedAt ?? "")}.`}
        </CardDescription>
      </CardHeader>
      {cluster && fleet.data && (
        <CardContent className="flex flex-col gap-4 text-sm">
          <div className="flex flex-col items-start gap-1">
            <FleetStateBadge state={cluster.state} />
            {cluster.reasons.length > 0 && (
              <ul className="text-muted-foreground">
                {cluster.reasons.map((reason, index) => (
                  <li key={index}>{reason}</li>
                ))}
              </ul>
            )}
          </div>
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2">
            <dt className="text-muted-foreground">Brokers</dt>
            <dd className="tabular-nums">
              <Measured value={brokersLine(cluster)} />
            </dd>
            <dt className="text-muted-foreground">Partitions</dt>
            <dd className="tabular-nums">
              <Partitions cluster={cluster} />
            </dd>
            <dt className="text-muted-foreground">Fullest volume</dt>
            <dd className="tabular-nums">
              {cluster.volumeUsed === undefined ? (
                <span className="text-muted-foreground">Not reported</span>
              ) : (
                formatShare(cluster.volumeUsed)
              )}
            </dd>
            <dt className="text-muted-foreground">Kafka</dt>
            <dd>
              <Version cluster={cluster} kafkaVersion={fleet.data.kafkaVersion} />
            </dd>
            <dt className="text-muted-foreground">Alerts</dt>
            <dd>
              <Alerts cluster={cluster} />
            </dd>
          </dl>
        </CardContent>
      )}
    </Card>
  );
}

/** The Kafka version a cluster runs, and whether the platform runs another. */
function Version({ cluster, kafkaVersion }: { cluster: FleetCluster; kafkaVersion: string }) {
  if (cluster.kafkaVersion === undefined) {
    return <Absent />;
  }
  return (
    <>
      {cluster.kafkaVersion}
      {cluster.kafkaVersion !== kafkaVersion && (
        <span className="block text-amber-700 dark:text-amber-500">
          The platform runs {kafkaVersion}
        </span>
      )}
    </>
  );
}

/** The operators' alerts firing for a cluster, and how many of the customer's own. */
function Alerts({ cluster }: { cluster: FleetCluster }) {
  return (
    <div className="flex flex-col gap-1">
      {cluster.alerts.length === 0 ? (
        <span className="text-muted-foreground">None for operators</span>
      ) : (
        <ul className="flex flex-col gap-1">
          {cluster.alerts.map((alert, index) => (
            <li key={index}>
              <AlertLine alert={alert} />
            </li>
          ))}
        </ul>
      )}
      {cluster.customerAlerts > 0 && (
        <span className="text-muted-foreground">
          {cluster.customerAlerts === 1
            ? "1 of the customer's own"
            : `${String(cluster.customerAlerts)} of the customer's own`}
        </span>
      )}
    </div>
  );
}

/** A cluster's partitions: all in sync, or what is not. */
function Partitions({ cluster }: { cluster: FleetCluster }) {
  const problems = partitionProblems(cluster);
  if (problems === undefined) {
    return <Absent />;
  }
  if (problems.length === 0) {
    return <span className="text-muted-foreground">All in sync</span>;
  }
  return (
    <ul>
      {problems.map((problem) => (
        <li
          key={problem.text}
          className={cn(
            "font-medium",
            problem.serious ? "text-destructive" : "text-amber-700 dark:text-amber-500",
          )}
        >
          {problem.text}
        </li>
      ))}
    </ul>
  );
}

/** A measure, or that the metrics store has none. */
function Measured({ value }: { value: string | undefined }) {
  return value === undefined ? <Absent /> : <span>{value}</span>;
}

function Absent() {
  return (
    <span className="text-muted-foreground">
      <span aria-hidden>–</span>
      <span className="sr-only">Not known</span>
    </span>
  );
}
