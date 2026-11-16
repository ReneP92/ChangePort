import { Link } from "react-router";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { errorText, type Usage } from "@/api/client";
import { useTopics, useUsage } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { Page } from "@/components/Page";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { formatBytes } from "@/lib/format";
import { useEnvironmentCrumb, useEnvironmentPath } from "@/lib/useEnvironment";
import { CustomerArea } from "@/pages/CustomerArea";

const chartConfig = {
  bytesIn: { label: "In", color: "var(--chart-1)" },
  bytesOut: { label: "Out", color: "var(--chart-2)" },
} satisfies ChartConfig;

/** A usage day's date, such as "Nov 3", in UTC as the days are. */
function formatDay(date: string, options: Intl.DateTimeFormatOptions = {}): string {
  return new Date(`${date}T00:00:00Z`).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
    ...options,
  });
}

/**
 * The data the environment's cluster processed over the last 30 days, per
 * day and per topic, and what its topics store now (docs/console.md).
 */
export function UsagePage() {
  return <CustomerArea>{() => <UsageView />}</CustomerArea>;
}

function UsageView() {
  const crumbs = useEnvironmentCrumb();
  const usage = useUsage();
  return (
    <Page
      crumbs={crumbs}
      title="Usage"
      description="The data your cluster processed over the last 30 days: what producers wrote and consumers read, each byte once. Replication and the platform's own topics do not count."
    >
      {usage.isPending && <Notice title="Loading" busy />}
      {usage.isError && <Notice title="Usage is unavailable">{errorText(usage.error)}</Notice>}
      {usage.data && (
        <>
          <Totals usage={usage.data} />
          <DailyCard usage={usage.data} />
          <TopicsCard usage={usage.data} />
        </>
      )}
    </Page>
  );
}

/** The 30 days' data in and out, and what the topics store now. */
export function Totals({ usage }: { usage: Usage }) {
  const stats: [string, string, string][] = [
    ["Data in", formatBytes(usage.bytesIn), "Written by producers"],
    ["Data out", formatBytes(usage.bytesOut), "Read by consumers"],
    [
      "Stored now",
      formatBytes(usage.localBytes + usage.remoteBytes),
      `${formatBytes(usage.localBytes)} on the brokers' disks, ${formatBytes(usage.remoteBytes)} in tiered storage`,
    ],
  ];
  return (
    <Card>
      <CardContent>
        <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-3">
          {stats.map(([term, value, note]) => (
            <div key={term} className="flex flex-col gap-1">
              <dt className="text-sm text-muted-foreground">{term}</dt>
              <dd className="text-2xl font-semibold tracking-tight tabular-nums">{value}</dd>
              <dd className="text-sm text-muted-foreground">{note}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

/** Data in and out per day, today so far last. */
function DailyCard({ usage }: { usage: Usage }) {
  const today = usage.days.at(-1)?.date;
  const points = usage.days.map((day) => ({
    date: day.date,
    bytesIn: day.bytesIn,
    bytesOut: day.bytesOut,
  }));
  const empty = usage.days.every((day) => day.bytesIn === 0 && day.bytesOut === 0);
  // A label a week apart, back from today, so that they are evenly spaced.
  const ticks = usage.days
    .map((day) => day.date)
    .filter((_, i) => (usage.days.length - 1 - i) % 7 === 0);
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Per day</h2>
        </CardTitle>
        <CardDescription>Days in UTC; today so far is the last.</CardDescription>
      </CardHeader>
      <CardContent>
        {empty ? (
          <p className="text-sm text-muted-foreground">
            No data in or out in the last 30 days yet.
          </p>
        ) : (
          <ChartContainer config={chartConfig} className="aspect-auto h-64 w-full">
            <BarChart data={points} margin={{ left: 8, right: 8, top: 4 }} barGap={2}>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="date"
                ticks={ticks}
                interval={0}
                tickFormatter={(date: string) => formatDay(date)}
                tickLine={false}
                axisLine={false}
              />
              <YAxis
                tickFormatter={(bytes: number) => formatBytes(bytes)}
                tickLine={false}
                axisLine={false}
                width={72}
              />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    labelFormatter={(_, payload) => {
                      const date = (payload[0]?.payload as { date?: string } | undefined)?.date;
                      if (date === undefined) {
                        return "";
                      }
                      const day = formatDay(date, { weekday: "short" });
                      return date === today ? `${day}, so far` : day;
                    }}
                    formatter={(value, name) => (
                      <span>
                        {chartConfig[name as keyof typeof chartConfig].label}{" "}
                        <span className="font-mono tabular-nums">{formatBytes(Number(value))}</span>
                      </span>
                    )}
                  />
                }
              />
              <ChartLegend content={<ChartLegendContent />} />
              {/* Without animation: each refresh would draw the bars again from nothing. */}
              <Bar
                dataKey="bytesIn"
                fill="var(--color-bytesIn)"
                radius={[4, 4, 0, 0]}
                isAnimationActive={false}
              />
              <Bar
                dataKey="bytesOut"
                fill="var(--color-bytesOut)"
                radius={[4, 4, 0, 0]}
                isAnimationActive={false}
              />
            </BarChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  );
}

/** Each topic's data in and out over the 30 days, and what it stores now. */
function TopicsCard({ usage }: { usage: Usage }) {
  const envPath = useEnvironmentPath();
  // Topics deleted within the 30 days still count, but have no page.
  const existing = new Set(useTopics().data?.map((topic) => topic.name));
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Topics</h2>
        </CardTitle>
        <CardDescription>The most data processed first.</CardDescription>
      </CardHeader>
      <CardContent className="text-sm">
        {usage.topics.length === 0 ? (
          <p className="text-muted-foreground">No topic processed or stores data yet.</p>
        ) : (
          // Wide tables scroll sideways on phones instead of overflowing.
          <div className="relative -mx-1 overflow-x-auto px-1">
            <table className="w-full text-left whitespace-nowrap [&_td]:px-3 [&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th]:px-3 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0">
              <thead className="border-b text-muted-foreground">
                <tr>
                  <th className="py-2 font-medium">Topic</th>
                  <th className="py-2 text-right font-medium">In</th>
                  <th className="py-2 text-right font-medium">Out</th>
                  <th className="py-2 text-right font-medium">On disk</th>
                  <th className="py-2 text-right font-medium">Tiered</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {usage.topics.map((topic) => (
                  <tr key={topic.topic}>
                    <td className="py-2">
                      {existing.has(topic.topic) ? (
                        <Link
                          to={envPath(`/topics/${encodeURIComponent(topic.topic)}`)}
                          className="font-medium underline-offset-4 hover:underline"
                        >
                          {topic.topic}
                        </Link>
                      ) : (
                        <span className="font-medium">{topic.topic}</span>
                      )}
                    </td>
                    {[topic.bytesIn, topic.bytesOut, topic.localBytes, topic.remoteBytes].map(
                      (bytes, i) => (
                        <td key={i} className="py-2 text-right tabular-nums">
                          {formatBytes(bytes)}
                        </td>
                      ),
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
