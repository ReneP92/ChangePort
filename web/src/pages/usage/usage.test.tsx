import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import type { Usage } from "@/api/client";
import { Totals } from "@/pages/usage/UsagePage";

afterEach(cleanup);

const usage: Usage = {
  bytesIn: 1_500_000,
  bytesOut: 4_200_000_000,
  days: [],
  topics: [],
  localBytes: 300_000_000,
  remoteBytes: 2_000_000_000,
};

describe("Totals", () => {
  test("shows the period's data in and out, and what is stored now, on disk and tiered", () => {
    render(<Totals usage={usage} />);
    const values = screen.getAllByRole("definition").map((dd) => dd.textContent);
    expect(values).toEqual([
      "1.5 MB",
      "Written by producers",
      "4.2 GB",
      "Read by consumers",
      "2.3 GB",
      "300 MB on the brokers' disks, 2.0 GB in tiered storage",
    ]);
  });
});
