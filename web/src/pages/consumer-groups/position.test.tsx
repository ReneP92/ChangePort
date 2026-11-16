import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import type { PositionPreview } from "@/api/client";
import { Effects } from "@/pages/consumer-groups/ChangePositionDialog";

afterEach(cleanup);

const preview: PositionPreview = {
  group: "billing",
  members: [],
  topics: [
    {
      topic: "orders",
      partitions: 3,
      readAgain: 600,
      skipped: 0,
      toRead: 1000,
      tieredMessages: 0,
    },
    { topic: "payments", partitions: 1, readAgain: 0, skipped: 40, toRead: 0, tieredMessages: 0 },
  ],
};

describe("Effects", () => {
  test("says what a move does on each topic, and warns of what it means", () => {
    render(<Effects group="billing" replay={false} preview={preview} pending={false} />);
    const rows = screen.getAllByRole("listitem").map((row) => row.textContent);
    expect(rows).toEqual(["ordersreads 600 messages again", "paymentsskips 40 messages"]);
    expect(document.body.textContent).toContain("Your service handles these messages again");
    expect(document.body.textContent).toContain("billing never reads the 40 skipped messages.");
  });

  test("a replay reads what is left, and skips nothing", () => {
    render(<Effects group="billing" replay preview={preview} pending={false} />);
    const rows = screen.getAllByRole("listitem").map((row) => row.textContent);
    expect(rows).toEqual(["ordersreads 1,000 messages", "paymentsonly new messages"]);
    expect(document.body.textContent).toContain("The service you start with the new group handles");
    expect(document.body.textContent).not.toContain("skipped");
  });

  test("says it is working while the preview loads", () => {
    render(<Effects group="billing" replay={false} preview={undefined} pending />);
    expect(screen.getByRole("status").textContent).toBe("Working out what this does");
  });
});
