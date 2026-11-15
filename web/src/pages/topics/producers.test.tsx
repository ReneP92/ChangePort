import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, render, screen, within } from "@testing-library/react";
import { Transactions } from "@/pages/overview/HealthCard";
import { ProducerList } from "@/pages/topics/ProducersCard";

afterEach(cleanup);

const now = Date.parse("2026-11-16T12:00:00Z");
const ago = (seconds: number) => new Date(now - seconds * 1000).toISOString();

describe("ProducerList", () => {
  test("names transactional producers, numbers the others, the most recent first", () => {
    render(
      <ProducerList
        now={now}
        producers={{
          heldBack: 120,
          producers: [
            {
              producerId: 7,
              transactionalId: "checkout-1",
              partitions: 2,
              lastWrite: ago(60),
              inTransaction: true,
            },
            { producerId: 9, partitions: 1, lastWrite: ago(600), inTransaction: false },
          ],
        }}
      />,
    );
    const rows = within(screen.getByRole("list", { name: "Producers" })).getAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual([
      "checkout-1TransactionalIn a transaction2 partitions, last wrote 1 minute ago",
      "Producer 9Idempotent1 partition, last wrote 10 minutes ago",
    ]);
    expect(
      screen.getByText(/Open transactions hold back 120 messages from read_committed consumers/),
    ).toBeDefined();
    expect(screen.getByText(/Producers that are not idempotent never show here/)).toBeDefined();
  });

  test("says so when no producer is known", () => {
    render(<ProducerList now={now} producers={{ heldBack: 0, producers: [] }} />);
    expect(
      screen.getByText(/No producer wrote to it idempotently or in a transaction/),
    ).toBeDefined();
    expect(screen.queryByText(/hold back/)).toBeNull();
  });
});

describe("Transactions", () => {
  test("none open", () => {
    render(<Transactions now={now} summary={{ open: 0 }} />);
    expect(screen.getByText("none open")).toBeDefined();
  });

  test("the oldest, amber once it holds consumers back", () => {
    render(<Transactions now={now} summary={{ open: 2, oldestStartedAt: ago(180) }} />);
    const line = screen.getByText("2 open, the oldest for 3 minutes");
    expect(line.className).toContain("text-amber-700");
  });

  test("one, briefly", () => {
    render(<Transactions now={now} summary={{ open: 1, oldestStartedAt: ago(20) }} />);
    const line = screen.getByText("1 open, for 20 seconds");
    expect(line.className).not.toContain("text-amber");
  });
});
