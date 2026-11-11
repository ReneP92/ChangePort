import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import {
  createApiClient,
  type ConsumerGroupLag,
  type Customer,
  type SampleApp,
  type Topic,
} from "@/api/client";

// The queries are keyed by the active organization, which Clerk knows.
void mock.module("@clerk/react", () => ({ useAuth: () => ({ orgId: "org_acme" }) }));
const { ApiContext } = await import("@/api/queries");
const { GetStartedCard } = await import("@/pages/overview/GetStartedCard");

const customer: Customer = {
  id: "acme",
  displayName: "Acme Corp",
  profile: "dev",
  ready: true,
  bootstrapAddress: "bootstrap.acme.kafka.changeport.test:9094",
} as Customer;

const sample = (extra: Partial<SampleApp> = {}): SampleApp => ({
  state: "stopped",
  topic: "sample.orders",
  group: "sample.order-emails",
  ...extra,
});

const topic = (name: string, extra: Partial<Topic> = {}): Topic => ({
  name,
  partitions: 3,
  replicationFactor: 1,
  tiered: true,
  cleanupPolicy: "delete",
  ...extra,
});

let requests: { method: string; path: string }[] = [];
const realFetch = globalThis.fetch;

beforeEach(() => {
  requests = [];
  globalThis.fetch = mock((input: Request) => {
    const url = new URL(input.url);
    requests.push({ method: input.method, path: url.pathname });
    if (input.method === "PUT" && url.pathname === "/api/customer/sample") {
      return Promise.resolve(
        Response.json(sample({ state: "starting", stopsAt: "2026-11-10T10:42:00Z" })),
      );
    }
    return Promise.resolve(new Response(null, { status: 204 }));
  }) as unknown as typeof fetch;
});

afterEach(() => {
  cleanup();
  globalThis.fetch = realFetch;
});

/** Renders the card with the queries' answers already in place. */
function renderCard({
  admin = true,
  sampleApp = sample(),
  topics = [] as Topic[],
  groups = [] as ConsumerGroupLag[],
  onHide = () => undefined,
} = {}) {
  const queries = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  });
  queries.setQueryData(["customer", "sample", "org_acme"], sampleApp);
  queries.setQueryData(["customer", "topics", "org_acme"], topics);
  queries.setQueryData(["customer", "consumer-groups", "org_acme"], groups);
  queries.setQueryData(["customer", "connection", "org_acme"], {
    bootstrapAddress: "bootstrap.acme.kafka.changeport.test:9094",
    securityProtocol: "SASL_SSL",
    saslMechanism: "SCRAM-SHA-512",
    username: "admin",
  });
  queries.setQueryData(["customer", "topic-policy", "org_acme"], undefined);
  return render(
    <QueryClientProvider client={queries}>
      <ApiContext value={createApiClient(() => Promise.resolve("token"))}>
        <MemoryRouter>
          <GetStartedCard customer={customer} admin={admin} onHide={onHide} />
        </MemoryRouter>
      </ApiContext>
    </QueryClientProvider>,
  );
}

function step(title: string) {
  const heading = screen.getByRole("heading", { name: title });
  const item = heading.closest("li");
  if (!item) {
    throw new Error(`no step ${title}`);
  }
  return item;
}

describe("GetStartedCard", () => {
  test("leads an admin through the steps of an empty cluster", async () => {
    renderCard();
    expect(within(step("Watch Kafka at work")).queryByLabelText("Done")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Start the sample" }));
    await waitFor(() => {
      expect(within(step("Watch Kafka at work")).getByRole("status").textContent).toContain(
        "Starting the sample",
      );
    });
    expect(requests).toContainEqual({ method: "PUT", path: "/api/customer/sample" });
    expect(within(step("Watch Kafka at work")).getByLabelText("Done")).toBeDefined();
    expect(screen.getByRole("button", { name: "Stop the sample" })).toBeDefined();

    const connect = step("Connect your app");
    expect(connect.textContent).toContain(
      "export KAFKA_BOOTSTRAP=bootstrap.acme.kafka.changeport.test:9094",
    );
    expect(connect.textContent).toContain("export TOPIC=orders");
    expect(connect.textContent).toContain("Waiting for your app's data");
    expect(
      within(connect)
        .getAllByRole("tab")
        .map((tab) => tab.textContent),
    ).toEqual(["Java", "Python", "Go", "Node.js", "Command line"]);
  });

  test("tells members to ask an admin", () => {
    renderCard({ admin: false });
    expect(screen.queryByRole("button", { name: "Start the sample" })).toBeNull();
    expect(step("Watch Kafka at work").textContent).toContain(
      "Ask an admin of your organization to start it.",
    );
    expect(step("Create a topic for your data").textContent).toContain(
      "Ask an admin of your organization to create one.",
    );
    expect(screen.queryByRole("button", { name: "Download client.properties" })).toBeNull();
  });

  test("shows the running sample and where to watch it", () => {
    renderCard({ sampleApp: sample({ state: "running", stopsAt: "2026-11-10T10:42:00Z" }) });
    const watch = step("Watch Kafka at work");
    expect(within(watch).getByRole("link", { name: "its messages" }).getAttribute("href")).toBe(
      "/topics/sample.orders",
    );
    expect(
      within(watch).getByRole("link", { name: "the email service" }).getAttribute("href"),
    ).toBe("/consumer-groups/sample.order-emails");
    expect(within(watch).getByRole("link", { name: "data flow map" }).getAttribute("href")).toBe(
      "#data-flow",
    );
    expect(within(watch).getByRole("button", { name: "Keep it running" })).toBeDefined();
  });

  test("ticks off the customer's own topic and app", () => {
    renderCard({
      topics: [
        topic("sample.orders", { sample: true, bytesInPerSecond: 900 }),
        topic("orders", { bytesInPerSecond: 120 }),
      ],
    });
    const topicStep = step("Create a topic for your data");
    expect(within(topicStep).getByLabelText("Done")).toBeDefined();
    expect(topicStep.textContent).toContain("Your topic orders is ready");
    const connect = step("Connect your app");
    expect(within(connect).getByLabelText("Done")).toBeDefined();
    expect(connect.textContent).toContain("Data arrives on orders.");
  });

  test("hides when asked", () => {
    const onHide = mock(() => undefined);
    renderCard({ onHide });
    fireEvent.click(screen.getByRole("button", { name: "Hide" }));
    expect(onHide).toHaveBeenCalledTimes(1);
  });
});
