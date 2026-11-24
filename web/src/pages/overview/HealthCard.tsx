import { ChevronDown, TriangleAlert } from "lucide-react";
import { useState } from "react";
import type { Health, TransactionsSummary } from "@/api/client";
import { useHealth } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { formatDuration } from "@/lib/format";
import { useNow } from "@/lib/useNow";
import { cn } from "@/lib/utils";

/** A count that is healthy at want, with what it means. */
function Stat({
  label,
  value,
  healthy,
}: {
  label: string;
  value: number | undefined;
  healthy: (value: number) => boolean;
}) {
  const known = value !== undefined;
  return (
    <div className="flex flex-col gap-1 rounded-md border p-3">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={cn(
          "text-2xl font-semibold tabular-nums",
          known && !healthy(value) && "text-destructive",
        )}
      >
        {known ? value : "–"}
      </span>
    </div>
  );
}

/**
 * The brokers, and whether every partition is in sync and led. Keeping them
 * so is Changeport's job, so while they are, it says so in a line, with the
 * counts and brokers a click away; once something is wrong it shows them,
 * unless the viewer has closed them.
 */
export function HealthCard() {
  const health = useHealth();
  return (
    <HealthPanel health={health.data} error={health.isError ? health.error.message : undefined} />
  );
}

/** The Health card, given the cluster's health once known. */
export function HealthPanel({
  health,
  error,
}: {
  health: Health | undefined;
  error?: string | undefined;
}) {
  const now = useNow();
  const [chosen, setChosen] = useState<boolean>();
  const open = chosen ?? (health !== undefined && !healthy(health));
  return (
    <Collapsible open={open} onOpenChange={setChosen}>
      <Card>
        <CardHeader>
          <CardTitle>
            <h2>Health</h2>
          </CardTitle>
          <CardDescription>{summary(health)}</CardDescription>
          {health && (
            <CardAction>
              <CollapsibleTrigger asChild>
                <Button variant="ghost" size="sm" className="group">
                  {open ? "Hide details" : "Details"}
                  <ChevronDown
                    aria-hidden
                    className="transition-transform group-data-[state=open]:rotate-180 motion-reduce:transition-none"
                  />
                </Button>
              </CollapsibleTrigger>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="flex flex-col gap-4 text-sm empty:hidden">
          {error && <p className="text-destructive">{error}</p>}
          {health && (
            <CollapsibleContent className="flex flex-col gap-4">
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <Stat
                  label="Under-replicated partitions"
                  value={health.underReplicatedPartitions}
                  healthy={(v) => v === 0}
                />
                <Stat
                  label="Under min. in-sync replicas"
                  value={health.underMinIsrPartitions}
                  healthy={(v) => v === 0}
                />
                <Stat
                  label="Offline partitions"
                  value={health.offlinePartitions}
                  healthy={(v) => v === 0}
                />
                <Stat
                  label="Active controllers"
                  value={health.activeControllers}
                  healthy={(v) => v === 1}
                />
              </div>
              <ul aria-label="Brokers" className="flex flex-col gap-2">
                {health.brokers.map((broker) => (
                  <li key={broker.name} className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <Badge variant={broker.ready ? "default" : "destructive"}>
                      {broker.ready ? "Ready" : "Not ready"}
                    </Badge>
                    <span className="min-w-0 font-medium break-all">{broker.name}</span>
                    {broker.zone && (
                      <span className="whitespace-nowrap text-muted-foreground">{broker.zone}</span>
                    )}
                    {broker.restarts > 0 && (
                      <span className="whitespace-nowrap text-muted-foreground">
                        {broker.restarts} restart{broker.restarts === 1 ? "" : "s"}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            </CollapsibleContent>
          )}
          {health?.transactions && <Transactions summary={health.transactions} now={now} />}
        </CardContent>
      </Card>
    </Collapsible>
  );
}

/** Whether every broker is ready and every partition in sync and led. */
function healthy(health: Health): boolean {
  return (
    health.brokers.every((broker) => broker.ready) &&
    (health.underReplicatedPartitions ?? 0) === 0 &&
    (health.underMinIsrPartitions ?? 0) === 0 &&
    (health.offlinePartitions ?? 0) === 0 &&
    (health.activeControllers === undefined || health.activeControllers === 1)
  );
}

/**
 * The transactions open now, and how long the oldest has been: one open for
 * a minute or more holds back read_committed consumers (docs/diagnosis.md).
 */
export function Transactions({ summary, now }: { summary: TransactionsSummary; now: number }) {
  const oldest =
    summary.oldestStartedAt === undefined
      ? undefined
      : Math.max(0, (now - Date.parse(summary.oldestStartedAt)) / 1000);
  const long = oldest !== undefined && oldest >= 60;
  return (
    <p className="text-muted-foreground">
      <span className="font-medium text-foreground">Transactions:</span>{" "}
      {summary.open === 0 ? (
        "none open"
      ) : (
        <span
          className={cn(
            "inline-flex flex-wrap items-center gap-x-1.5",
            long && "text-amber-700 dark:text-amber-500",
          )}
        >
          {long && <TriangleAlert aria-hidden className="size-4 shrink-0" />}
          {summary.open} open
          {oldest !== undefined &&
            `, ${summary.open === 1 ? "" : "the oldest "}for ${formatDuration(oldest)}`}
          {long && ", long enough to hold back read_committed consumers"}
        </span>
      )}
    </p>
  );
}

function summary(health: Health | undefined): string {
  if (!health) {
    return "Checking the brokers";
  }
  const ready = health.brokers.filter((broker) => broker.ready).length;
  const problems =
    (health.underReplicatedPartitions ?? 0) +
    (health.underMinIsrPartitions ?? 0) +
    (health.offlinePartitions ?? 0);
  const total = health.brokers.length;
  const brokers =
    total === 1
      ? ready === 1
        ? "The broker is ready"
        : "The broker is not ready"
      : `${String(ready)} of ${String(total)} brokers ready`;
  return problems === 0
    ? `${brokers}; every partition is in sync`
    : `${brokers}; some partitions are not in sync`;
}
