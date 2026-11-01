import { useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { ThroughputRange } from "@/api/client";
import { useEvents, useThroughput } from "@/api/queries";
import { ChangeList } from "@/components/Timeline";
import { rangeMilliseconds, ranges } from "@/lib/ranges";
import { eventMarkers, numberEvents } from "@/lib/timeline";
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
import { formatRate } from "@/lib/format";

const chartConfig = {
  bytesIn: { label: "In", color: "var(--chart-1)" },
  bytesOut: { label: "Out", color: "var(--chart-2)" },
} satisfies ChartConfig;

/**
 * Bytes in and out per second over the whole cluster, or one topic, with the
 * changes over the same range marked on it (docs/timeline.md).
 */
export function ThroughputCard({ topic }: { topic?: string }) {
  const [range, setRange] = useState<ThroughputRange>("1h");
  const [highlighted, setHighlighted] = useState<number | null>(null);
  const throughput = useThroughput(range, topic);
  const changes = useEvents(range, topic === undefined ? {} : { topic });
  const points =
    throughput.data?.bytesIn.map((point, i) => ({
      time: new Date(point.time).getTime(),
      bytesIn: point.value,
      bytesOut: throughput.data.bytesOut[i]?.value ?? 0,
    })) ?? [];
  // The chart runs up to when the changes were last read, so that what just
  // changed shows.
  const end = Math.max(points.at(-1)?.time ?? 0, changes.dataUpdatedAt);
  // The whole range, so that changes before the first point show too.
  const start = end - rangeMilliseconds[range];
  const numbered = numberEvents(changes.data?.events ?? [], start, end);
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
            <h2>Throughput</h2>
          </CardTitle>
          <CardDescription>
            {topic === undefined ? "Bytes per second, over all topics" : "Bytes per second"}, and
            what changed meanwhile
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
        {throughput.isError && (
          <p className="text-sm text-destructive">{throughput.error.message}</p>
        )}
        {throughput.data && points.length === 0 && (
          <p className="text-sm text-muted-foreground">No traffic in this range yet.</p>
        )}
        {points.length > 0 && (
          <ChartContainer config={chartConfig} className="aspect-auto h-56 w-full">
            <AreaChart data={points} margin={{ left: 8, right: 8 }}>
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
              <YAxis tickFormatter={formatRate} tickLine={false} axisLine={false} width={80} />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    labelFormatter={(_, payload) => {
                      const time = (payload[0]?.payload as { time?: number } | undefined)?.time;
                      return time === undefined ? "" : new Date(time).toLocaleString();
                    }}
                    formatter={(value, name) => (
                      <span>
                        {chartConfig[name as keyof typeof chartConfig].label}{" "}
                        <span className="font-mono tabular-nums">{formatRate(Number(value))}</span>
                      </span>
                    )}
                  />
                }
              />
              <ChartLegend content={<ChartLegendContent />} />
              {eventMarkers(numbered, highlighted, start, end)}
              {/* Without animation: each refresh would draw the chart again from nothing. */}
              <Area
                dataKey="bytesIn"
                type="monotone"
                stroke="var(--color-bytesIn)"
                strokeWidth={2}
                fill="var(--color-bytesIn)"
                fillOpacity={0.2}
                isAnimationActive={false}
              />
              <Area
                dataKey="bytesOut"
                type="monotone"
                stroke="var(--color-bytesOut)"
                strokeWidth={2}
                fill="var(--color-bytesOut)"
                fillOpacity={0.2}
                isAnimationActive={false}
              />
            </AreaChart>
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
