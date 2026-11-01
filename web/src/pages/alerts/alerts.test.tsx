import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import type { Alert } from "@/api/client";
import { alertsAbout } from "@/lib/alerts";
import { AlertBadge, AlertRows } from "@/pages/alerts/ActiveAlerts";

afterEach(cleanup);

const alert = (name: string, extra: Partial<Alert> = {}): Alert => ({
  name,
  severity: "warn",
  summary: `${name} summary`,
  description: "What it means.",
  startsAt: "2026-10-31T10:00:00Z",
  ...extra,
});

const alerts = [
  alert("ConsumerGroupLagHigh", { topic: "orders", consumerGroup: "billing" }),
  alert("ConsumerGroupStuck", { topic: "orders", consumerGroup: "billing" }),
  alert("TopicRequestErrors", { topic: "orders" }),
  alert("ClientThrottled", { clientId: "loader" }),
];

describe("alertsAbout", () => {
  test("tells a topic's alerts from its consumer groups'", () => {
    expect(alertsAbout(alerts, { topic: "orders" }).map((a) => a.name)).toEqual([
      "TopicRequestErrors",
    ]);
    expect(
      alertsAbout(alerts, { topic: "orders", consumerGroup: "billing" }).map((a) => a.name),
    ).toEqual(["ConsumerGroupLagHigh", "ConsumerGroupStuck"]);
    expect(alertsAbout(alerts, { topic: "orders", consumerGroup: "shipping" })).toEqual([]);
    expect(alertsAbout(undefined, { topic: "orders" })).toEqual([]);
  });
});

describe("AlertBadge", () => {
  test("names the first alert, counts the others, and lists them all on hover", () => {
    render(
      <AlertBadge alerts={alertsAbout(alerts, { topic: "orders", consumerGroup: "billing" })} />,
    );
    const badge = screen.getByText(/Behind/);
    expect(badge.textContent).toBe("Behind +1");
    expect(badge.getAttribute("title")).toBe(
      "ConsumerGroupLagHigh summary\nConsumerGroupStuck summary",
    );
  });

  test("shows nothing without alerts", () => {
    const { container } = render(<AlertBadge alerts={[]} />);
    expect(container.textContent).toBe("");
  });
});

describe("AlertRows", () => {
  test("shows each alert's summary and what it means", () => {
    render(<AlertRows alerts={alerts.slice(2)} />);
    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("TopicRequestErrors summary");
    expect(rows[0]?.textContent).toContain("What it means. Since ");
  });
});
