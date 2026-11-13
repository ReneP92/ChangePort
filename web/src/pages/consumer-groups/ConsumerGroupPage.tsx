import { useState } from "react";
import { Link, useParams } from "react-router";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import type { ThroughputRange } from "@/api/client";
import { useAlerts, useConsumerGroupLag, useConsumerGroups, useEvents } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { ChangeList } from "@/components/Timeline";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { durationTicks, formatDuration, formatLag } from "@/lib/format";
import { rangeMilliseconds, ranges } from "@/lib/ranges";
import { eventMarkers, numberEvents } from "@/lib/timeline";
import { cn } from "@/lib/utils";
import { AlertBadge, AlertRows } from "@/pages/alerts/ActiveAlerts";
import { CustomerArea } from "@/pages/CustomerArea";
import { useEnvironmentPath } from "@/lib/useEnvironment";

/**
 * One consumer group: how far behind it is on each topic it reads, over
 * time, with what changed meanwhile, such as its rebalances.
 */
export function ConsumerGroupPage() {
  const { group = "" } = useParams();
  return <CustomerArea>{() => <ConsumerGroup group={group} />}</CustomerArea>;
}

function ConsumerGroup({ group }: { group: string }) {
  const envPath = useEnvironmentPath();
  const groups = useConsumerGroups();
  const alerts = useAlerts();
  if (groups.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (groups.isError) {
    return <Notice title="The console is unavailable">{groups.error.message}</Notice>;
  }
  // A group that has only just joined has committed no offsets yet, but
  // its changes, such as its rebalances, already show.
  const rows = groups.data.filter((row) => row.group === group);
  const groupAlerts = alerts.data?.filter((alert) => alert.consumerGroup === group) ?? [];
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <nav aria-label="Breadcrumb" className="text-sm text-muted-foreground">
          <Link to={envPath("/")} className="underline-offset-4 hover:underline">
            Cluster
          </Link>{" "}
          / Consumer groups
        </nav>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight [overflow-wrap:anywhere]">
            {group}
          </h1>
          <AlertBadge alerts={groupAlerts} />
        </div>
      </header>
      {groupAlerts.length > 0 && (
        <section
          aria-label="Alerts"
          className="rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm"
        >
          <AlertRows alerts={groupAlerts} />
        </section>
      )}
      <Card>
        <CardHeader>
          <CardTitle>
            <h2>Topics</h2>
          </CardTitle>
          <CardDescription>How far behind the group is on each topic it reads.</CardDescription>
        </CardHeader>
        <CardContent className="text-sm">
          {rows.length === 0 && (
            <p className="text-muted-foreground">
              The group has committed no offsets yet, so how far behind it is is not known.
            </p>
          )}
          <ul className="flex flex-col divide-y empty:hidden">
            {rows.map((row) => (
              <li
                key={row.topic}
                className="flex items-center justify-between gap-4 py-2 first:pt-0 last:pb-0"
              >
                <Link
                  to={envPath(`/topics/${encodeURIComponent(row.topic)}`)}
                  className="font-medium underline-offset-4 [overflow-wrap:anywhere] hover:underline"
                >
                  {row.topic}
                </Link>
                <span className="flex shrink-0 items-center gap-3">
                  <span className="text-muted-foreground tabular-nums">
                    {row.lagMessages.toLocaleString()} messages
                  </span>
                  <span
                    className={cn(row.lagMessages === 0 ? "text-muted-foreground" : "font-medium")}
                  >
                    {formatLag(row.lagSeconds, row.lagMessages, row.capped)}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
      <LagCard group={group} />
    </main>
  );
}

const colors = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
] as const;

/** A line's color: the chart's colors in turn. */
function color(i: number): string {
  return colors[i % colors.length] ?? colors[0];
}

/** The group's lag per topic over a range, with its changes marked on it. */
function LagCard({ group }: { group: string }) {
  const [range, setRange] = useState<ThroughputRange>("1h");
  const [highlighted, setHighlighted] = useState<number | null>(null);
  const lag = useConsumerGroupLag(group, range);
  const changes = useEvents(range, { consumerGroup: group });
  const topics = lag.data?.topics ?? [];
  // One row per time, with a column per topic.
  const byTime = new Map<number, Record<string, number>>();
  topics.forEach((topic, i) => {
    for (const point of topic.lagSeconds) {
      const time = new Date(point.time).getTime();
      byTime.set(time, { ...byTime.get(time), time, [`t${String(i)}`]: point.value });
    }
  });
  const points = [...byTime.values()].sort((a, b) => (a.time ?? 0) - (b.time ?? 0));
  const config: ChartConfig = Object.fromEntries(
    topics.map((topic, i) => [`t${String(i)}`, { label: topic.topic, color: color(i) }]),
  );
  const end = Math.max(points.at(-1)?.time ?? 0, changes.dataUpdatedAt);
  // The whole range, so that changes before the first point show too.
  const start = end - rangeMilliseconds[range];
  const numbered = numberEvents(changes.data?.events ?? [], start, end);
  const ticks = durationTicks(
    Math.max(...topics.flatMap((topic) => topic.lagSeconds.map((point) => point.value)), 0),
  );
  const long = range === "24h" || range === "7d";
  const formatTime = (time: number) =>
    new Date(time).toLocaleString(
      undefined,
      long ? { weekday: "short", hour: "2-digit" } : { timeStyle: "short" },
    );
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div className="flex flex-col gap-1.5">
          <CardTitle>
            <h2>Lag</h2>
          </CardTitle>
          <CardDescription>
            The age of the oldest message the group has not consumed, and what changed meanwhile
          </CardDescription>
        </div>
        <div role="group" aria-label="Range" className="flex gap-1">
          {ranges.map((r) => (
            <Button
              key={r}
              size="sm"
              variant={r === range ? "default" : "ghost"}
              aria-pressed={r === range}
              onClick={() => {
                setRange(r);
              }}
            >
              {r}
            </Button>
          ))}
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        {lag.isError && <p className="text-sm text-destructive">{lag.error.message}</p>}
        {lag.data && points.length === 0 && (
          <p className="text-sm text-muted-foreground">No lag measured in this range yet.</p>
        )}
        {points.length > 0 && (
          <ChartContainer config={config} className="aspect-auto h-56 w-full">
            <LineChart data={points} margin={{ left: 8, right: 8, top: 4 }}>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="time"
                type="number"
                scale="time"
                domain={[start, end]}
                tickFormatter={formatTime}
                tickLine={false}
                axisLine={false}
                minTickGap={40}
              />
              <YAxis
                ticks={ticks}
                domain={[0, ticks.at(-1) ?? 0]}
                tickFormatter={(seconds: number) => formatDuration(seconds)}
                tickLine={false}
                axisLine={false}
                width={96}
              />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    labelFormatter={(_, payload) => {
                      const time = (payload[0]?.payload as { time?: number } | undefined)?.time;
                      return time === undefined ? "" : new Date(time).toLocaleString();
                    }}
                    formatter={(value, name) => (
                      <span>
                        {config[String(name)]?.label}{" "}
                        <span className="font-mono tabular-nums">
                          {formatDuration(Number(value))} behind
                        </span>
                      </span>
                    )}
                  />
                }
              />
              <ChartLegend content={<ChartLegendContent />} />
              {eventMarkers(numbered, highlighted, start, end)}
              {topics.map((topic, i) => (
                <Line
                  key={topic.topic}
                  dataKey={`t${String(i)}`}
                  type="monotone"
                  stroke={`var(--color-t${String(i)})`}
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          </ChartContainer>
        )}
        <ChangeList
          numbered={numbered}
          truncated={changes.data?.truncated ?? false}
          highlighted={highlighted}
          onHighlight={setHighlighted}
          isPending={changes.isPending}
          error={changes.error}
        />
      </CardContent>
    </Card>
  );
}
