import {
  CircleMinus,
  CirclePlus,
  Columns3,
  Gauge,
  KeyRound,
  RotateCcw,
  SlidersHorizontal,
  Trash2,
  Users,
  type LucideIcon,
} from "lucide-react";
import type { TimelineEvent } from "@/api/client";
import { cn } from "@/lib/utils";
import { ReferenceLine, usePlotArea } from "recharts";
import { numberedMarkers, type NumberedEvent } from "@/lib/timeline";

const icons: Record<TimelineEvent["kind"], LucideIcon> = {
  "topic.created": CirclePlus,
  "topic.deleted": Trash2,
  "topic.config": SlidersHorizontal,
  "topic.partitions": Columns3,
  "acl.added": KeyRound,
  "acl.removed": KeyRound,
  "group.rebalanced": Users,
  "broker.restarted": RotateCcw,
  "client.throttled": Gauge,
  "client.unthrottled": CircleMinus,
};

/** When an event happened, for people. */
function formatTime(time: number): string {
  return new Date(time).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/**
 * The changes on a chart, newest first, each with its number on the chart,
 * when it happened and what it was in plain words. Pointing at one
 * highlights its marker.
 */
export function ChangeList({
  numbered,
  truncated,
  highlighted,
  onHighlight,
  isPending,
  error,
}: {
  numbered: NumberedEvent[];
  truncated: boolean;
  highlighted: number | null;
  onHighlight: (n: number | null) => void;
  isPending?: boolean;
  error?: Error | null;
}) {
  return (
    <section aria-label="Changes" className="flex flex-col gap-2 text-sm">
      <h3 className="font-medium">Changes</h3>
      {isPending && <p className="text-muted-foreground">Loading changes</p>}
      {error && <p className="text-destructive">{error.message}</p>}
      {!isPending && !error && numbered.length === 0 && (
        <p className="text-muted-foreground">No changes in this range.</p>
      )}
      {numbered.length > 0 && (
        <ol className="flex max-h-72 flex-col divide-y overflow-y-auto">
          {numbered.toReversed().map(({ n, time, event }) => {
            const Icon = icons[event.kind];
            return (
              <li
                key={event.id}
                className={cn(
                  "flex items-start gap-3 py-2 first:pt-0 last:pb-0",
                  n === highlighted && "text-foreground",
                )}
                onMouseEnter={() => {
                  onHighlight(n);
                }}
                onMouseLeave={() => {
                  onHighlight(null);
                }}
              >
                <span
                  aria-label={`Marker ${String(n)}`}
                  className={cn(
                    "mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full border text-[0.65rem] font-medium tabular-nums",
                    n === highlighted && "border-foreground bg-foreground text-background",
                  )}
                >
                  {n}
                </span>
                <Icon aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="[overflow-wrap:anywhere]">{event.message}</span>
                  <time dateTime={event.time} className="text-xs text-muted-foreground">
                    {formatTime(time)}
                  </time>
                </span>
              </li>
            );
          })}
        </ol>
      )}
      {truncated && (
        <p className="text-xs text-muted-foreground">
          Only the newest changes are shown; choose a shorter range for the others.
        </p>
      )}
    </section>
  );
}

/** The number on top of a marker, which the chart places with viewBox. */
export function MarkerLabel({
  n,
  active,
  viewBox,
}: {
  n: number;
  active: boolean;
  viewBox?: { x?: number; y?: number };
}) {
  const x = viewBox?.x ?? 0;
  const y = (viewBox?.y ?? 0) + 9;
  return (
    <g aria-hidden>
      <circle
        cx={x}
        cy={y}
        r={8}
        fill={active ? "var(--foreground)" : "var(--background)"}
        stroke="var(--muted-foreground)"
      />
      <text
        x={x}
        y={y}
        textAnchor="middle"
        dominantBaseline="central"
        fontSize={10}
        fontWeight={500}
        fill={active ? "var(--background)" : "var(--foreground)"}
      >
        {n}
      </text>
    </g>
  );
}

/**
 * A marker per event on a chart whose x axis runs from start to end, in
 * milliseconds: a dashed line, with the event's number on top where the
 * number has room at the chart's width (numberedMarkers). It reads the plot's
 * width from the chart it is in.
 */
export function EventMarkers({
  numbered,
  highlighted,
  start,
  end,
}: {
  numbered: NumberedEvent[];
  highlighted: number | null;
  start: number;
  end: number;
}) {
  const width = usePlotArea()?.width ?? 0;
  const labelled = numberedMarkers(numbered, highlighted, start, end, width);
  return numbered.map(({ n, time, event }) => (
    <ReferenceLine
      key={event.id}
      x={time}
      stroke={n === highlighted ? "var(--foreground)" : "var(--muted-foreground)"}
      strokeOpacity={n === highlighted ? 1 : 0.6}
      strokeDasharray="3 3"
      ifOverflow="hidden"
      {...(labelled.has(n) ? { label: <MarkerLabel n={n} active={n === highlighted} /> } : {})}
    />
  ));
}
