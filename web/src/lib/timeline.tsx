import { ReferenceLine } from "recharts";
import type { TimelineEvent } from "@/api/client";
import { MarkerLabel } from "@/components/Timeline";

/** An event on a chart, numbered from the oldest in its range. */
export interface NumberedEvent {
  event: TimelineEvent;
  n: number;
  time: number;
}

/**
 * The events between start and end, in milliseconds, numbered from 1 in time
 * order, so that the markers count from left to right.
 */
export function numberEvents(events: TimelineEvent[], start: number, end: number): NumberedEvent[] {
  return events
    .map((event) => ({ event, time: new Date(event.time).getTime() }))
    .filter(({ time }) => time >= start && time <= end)
    .sort((a, b) => a.time - b.time || a.event.id.localeCompare(b.event.id))
    .map((numbered, i) => ({ ...numbered, n: i + 1 }));
}

/**
 * A marker per event on a chart whose x axis runs from start to end, in
 * milliseconds: a dashed line, with the event's number on top where the
 * number has room. Of markers too close to number apart, the newest is
 * numbered, and the one pointed at in the list always is. A function rather
 * than a component, as the chart takes its children as they are.
 */
export function eventMarkers(
  numbered: NumberedEvent[],
  highlighted: number | null,
  start: number,
  end: number,
) {
  const room = (end - start) / 24;
  const labelled = new Set<number>();
  let last = Infinity;
  for (const { n, time } of numbered.toReversed()) {
    if (last - time >= room) {
      labelled.add(n);
      last = time;
    }
  }
  return numbered.map(({ n, time, event }) => (
    <ReferenceLine
      key={event.id}
      x={time}
      stroke={n === highlighted ? "var(--foreground)" : "var(--muted-foreground)"}
      strokeOpacity={n === highlighted ? 1 : 0.6}
      strokeDasharray="3 3"
      ifOverflow="hidden"
      {...(labelled.has(n) || n === highlighted
        ? { label: <MarkerLabel n={n} active={n === highlighted} /> }
        : {})}
    />
  ));
}
