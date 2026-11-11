import { afterEach, describe, expect, test } from "bun:test";
import type { ConsumerGroupLag, TailMessage, Topic } from "@/api/client";
import examples from "@/lib/connecting-examples.json";
import {
  exampleEnvironment,
  finishOnboarding,
  firstMessage,
  onboardingDone,
  onboardingExamples,
  shouldWelcome,
} from "@/lib/onboarding";

const topic = (name: string, extra: Partial<Topic> = {}): Topic => ({
  name,
  partitions: 3,
  replicationFactor: 1,
  tiered: true,
  cleanupPolicy: "delete",
  ...extra,
});

const group = (name: string, topicName: string): ConsumerGroupLag => ({
  group: name,
  topic: topicName,
  lagSeconds: 0,
  lagMessages: 0,
  capped: false,
  history: [],
});

const message = (timestamp: string): TailMessage => ({
  partition: 0,
  offset: 0,
  timestamp,
  value: { format: "text", text: "hello from Python", size: 17, truncated: false },
  headers: [],
});

afterEach(() => {
  localStorage.clear();
});

describe("shouldWelcome", () => {
  test("welcomes a cluster that is not ready or has no topics yet", () => {
    expect(shouldWelcome(false, false, undefined)).toBe(true);
    expect(shouldWelcome(false, true, [])).toBe(true);
    expect(shouldWelcome(false, true, [topic("orders")])).toBe(false);
    // A Postgres source's topics count: the customer has data.
    expect(shouldWelcome(false, true, [topic("shop.public.orders", { source: "shop" })])).toBe(
      false,
    );
    // While the topics load, nobody is sent anywhere yet.
    expect(shouldWelcome(false, true, undefined)).toBe(false);
  });

  test("leaves a viewer who is done with it alone", () => {
    expect(shouldWelcome(true, false, undefined)).toBe(false);
    expect(shouldWelcome(true, true, [])).toBe(false);
  });

  test("remembers per customer in this browser", () => {
    expect(onboardingDone("acme")).toBe(false);
    finishOnboarding("acme");
    expect(onboardingDone("acme")).toBe(true);
    expect(onboardingDone("globex")).toBe(false);
  });
});

describe("onboardingExamples", () => {
  test("are the guide's Python, Go and JavaScript examples, in that order", () => {
    expect(onboardingExamples(examples).map((e) => e.language)).toEqual([
      "Python",
      "Go",
      "Node.js",
    ]);
  });
});

describe("firstMessage", () => {
  const since = Date.parse("2026-11-10T10:00:00Z");

  test("waits until a message arrives, then for the group that reads it", () => {
    expect(firstMessage("orders", since, [], [topic("orders")], [])).toEqual({
      arrived: false,
      readBy: undefined,
      messages: [],
    });
    const arrived = [message("2026-11-10T10:01:00Z")];
    expect(firstMessage("orders", since, arrived, [topic("orders")], [])).toEqual({
      arrived: true,
      readBy: undefined,
      messages: arrived,
    });
    expect(
      firstMessage("orders", since, arrived, [topic("orders")], [group("example-python", "orders")])
        .readBy,
    ).toBe("example-python");
  });

  test("counts only messages since the step, give or take a clock's skew", () => {
    const old = message("2026-11-10T09:00:00Z");
    const skewed = message("2026-11-10T09:59:50Z");
    expect(firstMessage("orders", since, [skewed, old], [topic("orders")], []).messages).toEqual([
      skewed,
    ]);
    // A group that read the topic before is no sign of a new message.
    expect(
      firstMessage("orders", since, [old], [topic("orders")], [group("billing", "orders")]),
    ).toEqual({
      arrived: false,
      readBy: undefined,
      messages: [],
    });
  });

  test("sees members' messages by the topic's traffic", () => {
    expect(
      firstMessage("orders", since, [], [topic("orders", { bytesInPerSecond: 12 })], []).arrived,
    ).toBe(true);
    expect(
      firstMessage("orders", since, [], [topic("payments", { bytesInPerSecond: 12 })], []).arrived,
    ).toBe(false);
  });
});

describe("exampleEnvironment", () => {
  test("sets what the examples read, with the password from client.properties", () => {
    expect(exampleEnvironment("bootstrap.acme.kafka.changeport.io:9094", "admin", "orders")).toBe(
      [
        "export KAFKA_BOOTSTRAP=bootstrap.acme.kafka.changeport.io:9094",
        "export KAFKA_USERNAME=admin",
        `export KAFKA_PASSWORD="$(sed -n 's/.*password="\\([^"]*\\)".*/\\1/p' client.properties)"`,
        "# On AWS, the ID of the availability zone your app runs in, such as use1-az1",
        'export KAFKA_CLIENT_RACK=""',
        "export TOPIC=orders",
      ].join("\n"),
    );
  });
});
