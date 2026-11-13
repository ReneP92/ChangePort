import { TriangleAlert } from "lucide-react";
import { Link } from "react-router";
import type { Alert } from "@/api/client";
import { useAlerts } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { formatSince, shortName } from "@/lib/alerts";
import { cn } from "@/lib/utils";
import { useEnvironmentPath } from "@/lib/useEnvironment";

/** A badge for alerts about one thing, such as a topic; nothing without. */
export function AlertBadge({ alerts }: { alerts: Alert[] }) {
  const first = alerts[0];
  if (!first) {
    return null;
  }
  return (
    <Badge variant="destructive" title={alerts.map((alert) => alert.summary).join("\n")}>
      <TriangleAlert aria-hidden />
      {shortName(first.name)}
      {alerts.length > 1 && ` +${String(alerts.length - 1)}`}
    </Badge>
  );
}

/** The active alerts, one per row, with what to look at. */
export function AlertRows({ alerts, className }: { alerts: Alert[]; className?: string }) {
  return (
    <ul className={cn("flex flex-col divide-y", className)}>
      {alerts.map((alert) => (
        <li
          key={`${alert.name}\u0000${alert.topic ?? ""}\u0000${alert.consumerGroup ?? ""}\u0000${alert.clientId ?? ""}`}
          className="flex flex-col gap-0.5 py-2 first:pt-0 last:pb-0"
        >
          <span className="font-medium [overflow-wrap:anywhere]">{alert.summary}</span>
          <span className="text-muted-foreground">
            {alert.description} Since {formatSince(alert.startsAt)}.
          </span>
        </li>
      ))}
    </ul>
  );
}

/**
 * The cluster's active alerts at the top of the overview, live; nothing
 * while all is well.
 */
export function AlertsBanner() {
  const envPath = useEnvironmentPath();
  const alerts = useAlerts();
  if (!alerts.data || alerts.data.length === 0) {
    return null;
  }
  const count = alerts.data.length;
  return (
    <section
      aria-labelledby="active-alerts"
      className="flex flex-col gap-3 rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm"
    >
      <div className="flex items-center justify-between gap-4">
        <h2 id="active-alerts" className="flex items-center gap-2 font-medium text-destructive">
          <TriangleAlert aria-hidden className="size-4 shrink-0" />
          {count === 1 ? "1 alert" : `${String(count)} alerts`}
        </h2>
        <Link to={envPath("/alerts")} className="shrink-0 underline-offset-4 hover:underline">
          Alert settings
        </Link>
      </div>
      <AlertRows alerts={alerts.data} />
    </section>
  );
}
