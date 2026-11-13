import { afterEach, describe, expect, mock, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { TimelineEvent } from "@/api/client";
import { ChangeList } from "@/components/Timeline";
import { markerSpacing, numberEvents, numberedMarkers } from "@/lib/timeline";

afterEach(cleanup);

const event = (id: string, time: string, extra: Partial<TimelineEvent> = {}): TimelineEvent => ({
  id,
  time,
  kind: "topic.config",
  message: `Change ${id}.`,
  ...extra,
});

const start = new Date("2026-10-31T12:00:00Z").getTime();
const end = new Date("2026-10-31T13:00:00Z").getTime();

describe("numberEvents", () => {
  test("numbers the events in the range from the oldest, as the markers run left to right", () => {
    const numbered = numberEvents(
      [
        event("late", "2026-10-31T12:50:00Z"),
        event("early", "2026-10-31T12:10:00Z"),
        event("before", "2026-10-31T11:59:59Z"),
        event("after", "2026-10-31T13:00:01Z"),
        event("b", "2026-10-31T12:30:00Z"),
        event("a", "2026-10-31T12:30:00Z"),
      ],
      start,
      end,
    );
    expect(numbered.map(({ n, event }) => `${String(n)} ${event.id}`)).toEqual([
      "1 early",
      "2 a",
      "3 b",
      "4 late",
    ]);
  });
});

describe("ChangeList", () => {
  const numbered = numberEvents(
    [
      event("1", "2026-10-31T12:10:00Z", {
        kind: "group.rebalanced",
        message: "Consumer group billing rebalanced.",
      }),
      event("2", "2026-10-31T12:40:00Z", { message: "Topic orders: retention.ms changed." }),
    ],
    start,
    end,
  );

  test("lists the changes newest first, with their markers' numbers and times", () => {
    render(
      <ChangeList
        numbered={numbered}
        truncated={false}
        highlighted={null}
        onHighlight={() => undefined}
      />,
    );
    const items = screen.getAllByRole("listitem");
    expect(items[0]?.textContent).toContain("2Topic orders: retention.ms changed.");
    expect(items[1]?.textContent).toContain("1Consumer group billing rebalanced.");
    expect(items[0]?.querySelector("time")?.getAttribute("dateTime")).toBe("2026-10-31T12:40:00Z");
    expect(screen.getByLabelText("Marker 2")).toBeDefined();
  });

  test("highlights a change's marker while pointed at", () => {
    const onHighlight = mock((n: number | null) => n);
    render(
      <ChangeList
        numbered={numbered}
        truncated={false}
        highlighted={null}
        onHighlight={onHighlight}
      />,
    );
    const item = screen.getAllByRole("listitem")[1];
    if (!item) {
      throw new Error("no change listed");
    }
    fireEvent.mouseEnter(item);
    expect(onHighlight).toHaveBeenLastCalledWith(1);
    fireEvent.mouseLeave(item);
    expect(onHighlight).toHaveBeenLastCalledWith(null);
  });

  test("says when there are no changes, and when only the newest are shown", () => {
    const { rerender } = render(
      <ChangeList
        numbered={[]}
        truncated={false}
        highlighted={null}
        onHighlight={() => undefined}
      />,
    );
    expect(screen.getByText("No changes in this range.")).toBeDefined();
    rerender(
      <ChangeList numbered={numbered} truncated highlighted={null} onHighlight={() => undefined} />,
    );
    expect(screen.getByText(/Only the newest changes are shown/)).toBeDefined();
  });
});

describe("numberedMarkers", () => {
  // Changes over an hour, as on the overview: three a minute apart, one
  // half an hour later.
  const numbered = numberEvents(
    [
      event("a", "2026-10-31T12:10:00Z"),
      event("b", "2026-10-31T12:11:00Z"),
      event("c", "2026-10-31T12:12:00Z"),
      event("d", "2026-10-31T12:40:00Z"),
    ],
    start,
    end,
  );
  const ids = (labelled: Set<number>) =>
    numbered.filter(({ n }) => labelled.has(n)).map(({ event }) => event.id);
  const x = (n: number, width: number) =>
    (((numbered[n - 1]?.time ?? start) - start) / (end - start)) * width;

  test("numbers markers a minute apart on a wide chart", () => {
    // 720 pixels for an hour: 12 a minute.
    expect(ids(numberedMarkers(numbered, null, start, end, 720))).toEqual(["a", "c", "d"]);
  });

  test("numbers the newest of markers too close on a phone, and never overlaps two numbers", () => {
    // 230 pixels for an hour, a phone: 4 a minute.
    for (const highlighted of [null, 1, 2, 3, 4]) {
      const labelled = [...numberedMarkers(numbered, highlighted, start, end, 230)];
      for (const a of labelled) {
        for (const b of labelled) {
          if (a !== b) {
            expect(Math.abs(x(a, 230) - x(b, 230))).toBeGreaterThanOrEqual(markerSpacing);
          }
        }
      }
    }
    expect(ids(numberedMarkers(numbered, null, start, end, 230))).toEqual(["c", "d"]);
  });

  test("always numbers the one pointed at in the list", () => {
    expect(ids(numberedMarkers(numbered, 1, start, end, 230))).toEqual(["a", "d"]);
    expect(ids(numberedMarkers(numbered, 2, start, end, 230))).toEqual(["b", "d"]);
  });
});
