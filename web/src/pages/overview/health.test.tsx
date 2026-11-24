import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Health } from "@/api/client";
import { HealthPanel } from "@/pages/overview/HealthCard";

afterEach(cleanup);

const health = (extra: Partial<Health> = {}): Health => ({
  brokers: [{ name: "acme-dual-role-0", ready: true, zone: "zone-a", restarts: 0 }],
  underReplicatedPartitions: 0,
  underMinIsrPartitions: 0,
  offlinePartitions: 0,
  activeControllers: 1,
  transactions: { open: 0 },
  ...extra,
});

describe("HealthPanel", () => {
  test("a healthy cluster says so in a line, with the details a click away", () => {
    render(<HealthPanel health={health()} />);
    expect(screen.getByText("The broker is ready; every partition is in sync")).toBeDefined();
    expect(screen.queryByText("Offline partitions")).toBeNull();
    expect(screen.getByText("none open")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(screen.getByText("Offline partitions")).toBeDefined();
    expect(screen.getByText("acme-dual-role-0")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Hide details" }));
    expect(screen.queryByText("Offline partitions")).toBeNull();
  });

  test.each([
    ["a partition out of sync", { underReplicatedPartitions: 2 }],
    ["an offline partition", { offlinePartitions: 1 }],
    ["no active controller", { activeControllers: 0 }],
    [
      "a broker not ready",
      { brokers: [{ name: "acme-dual-role-0", ready: false, zone: "zone-a", restarts: 3 }] },
    ],
  ] satisfies [string, Partial<Health>][])("shows the details by itself with %s", (_, extra) => {
    render(<HealthPanel health={health(extra)} />);
    expect(screen.getByText("Offline partitions")).toBeDefined();
    expect(screen.getByRole("button", { name: "Hide details" })).toBeDefined();
  });

  test("keeps the details closed once the viewer closed them", () => {
    const { rerender } = render(<HealthPanel health={health({ offlinePartitions: 1 })} />);
    fireEvent.click(screen.getByRole("button", { name: "Hide details" }));
    rerender(<HealthPanel health={health({ offlinePartitions: 2 })} />);
    expect(screen.queryByText("Offline partitions")).toBeNull();
  });

  test("offers no details before the health is known", () => {
    render(<HealthPanel health={undefined} />);
    expect(screen.getByText("Checking the brokers")).toBeDefined();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
