import type { TimelineEvent } from "@/api/client";

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

/** How far apart, in pixels, two numbered markers' centres stay: their circles are 16 across. */
export const markerSpacing = 20;

/**
 * Which events' markers carry their number on a chart whose plot is width
 * pixels across, from start to end in milliseconds: the one pointed at in
 * the list always, then, newest first, every one whose number has room
 * beside those already numbered. The others are dashed lines alone, still
 * counted in the list under the chart.
 */
export function numberedMarkers(
  numbered: NumberedEvent[],
  highlighted: number | null,
  start: number,
  end: number,
  width: number,
): Set<number> {
  const x = (time: number) => (end > start ? ((time - start) / (end - start)) * width : 0);
  const labelled = new Set<number>();
  const taken: number[] = [];
  const pointed = numbered.find(({ n }) => n === highlighted);
  if (pointed) {
    labelled.add(pointed.n);
    taken.push(x(pointed.time));
  }
  for (const { n, time } of numbered.toReversed()) {
    const at = x(time);
    if (!labelled.has(n) && taken.every((other) => Math.abs(other - at) >= markerSpacing)) {
      labelled.add(n);
      taken.push(at);
    }
  }
  return labelled;
}
