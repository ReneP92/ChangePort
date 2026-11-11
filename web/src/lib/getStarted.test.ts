import { afterEach, describe, expect, test } from "bun:test";
import type { ConsumerGroupLag, SampleApp, Topic } from "@/api/client";
import {
  exampleEnvironment,
  guideProgress,
  guideVisible,
  loadGuideChoice,
  saveGuideChoice,
} from "@/lib/getStarted";

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

const running: SampleApp = {
  state: "running",
  stopsAt: "2026-11-10T10:42:00Z",
  topic: "sample.orders",
  group: "sample.order-emails",
};
const stopped: SampleApp = {
  state: "stopped",
  topic: "sample.orders",
  group: "sample.order-emails",
};

afterEach(() => {
  localStorage.clear();
});

describe("guideProgress", () => {
  test("starts with nothing done on an empty cluster", () => {
    expect(guideProgress(stopped, [], [])).toEqual({
      sample: false,
      topic: undefined,
      app: undefined,
    });
  });

  test("counts every topic but the sample's as the customer's own, a source's too", () => {
    const groups = [group("sample.order-emails", "sample.orders")];
    expect(
      guideProgress(
        running,
        [topic("sample.orders", { sample: true, bytesInPerSecond: 1300 })],
        groups,
      ),
    ).toEqual({
      sample: true,
      topic: undefined,
      app: undefined,
    });
    expect(
      guideProgress(
        running,
        [topic("shop.public.orders", { source: "shop", bytesInPerSecond: 200 })],
        groups,
      ),
    ).toEqual({
      sample: true,
      topic: "shop.public.orders",
      app: "Data arrives on shop.public.orders",
    });
  });

  test("leaves out the sample's topic as it goes, once the sample stops", () => {
    const lingering = [topic("sample.orders", { bytesInPerSecond: 1300 })];
    expect(
      guideProgress(stopped, lingering, [group("sample.order-emails", "sample.orders")]),
    ).toEqual({
      sample: false,
      topic: undefined,
      app: undefined,
    });
  });

  test("sees the customer's app by the data it writes, or by its consumer group", () => {
    expect(guideProgress(stopped, [topic("orders")], [])).toEqual({
      sample: false,
      topic: "orders",
      app: undefined,
    });
    expect(
      guideProgress(
        stopped,
        [topic("orders", { bytesInPerSecond: 0 })],
        [group("example-python", "orders")],
      ).app,
    ).toBe("Consumer group example-python reads orders");
    expect(guideProgress(stopped, [topic("orders", { bytesInPerSecond: 42 })], []).app).toBe(
      "Data arrives on orders",
    );
    // A producer gone quiet still counts, by the data it left.
    expect(
      guideProgress(
        stopped,
        [topic("orders", { bytesInPerSecond: 0, localBytes: 0, remoteBytes: 2048 })],
        [],
      ).app,
    ).toBe("orders holds your app's data");
  });
});

describe("guideVisible", () => {
  const done = { sample: false, topic: "orders", app: "Data arrives on orders" };

  test("shows until the customer has a topic and an app of their own", () => {
    expect(guideVisible("auto", { sample: false, topic: undefined, app: undefined })).toBe(true);
    expect(guideVisible("auto", { ...done, app: undefined })).toBe(true);
    expect(guideVisible("auto", done)).toBe(false);
    // The guide is where the sample stops.
    expect(guideVisible("auto", { ...done, sample: true })).toBe(true);
  });

  test("follows the viewer's choice", () => {
    expect(guideVisible("hidden", { sample: true, topic: undefined, app: undefined })).toBe(false);
    expect(guideVisible("shown", done)).toBe(true);
  });

  test("remembers the choice per customer in this browser", () => {
    expect(loadGuideChoice("acme")).toBe("auto");
    saveGuideChoice("acme", "hidden");
    expect(loadGuideChoice("acme")).toBe("hidden");
    expect(loadGuideChoice("globex")).toBe("auto");
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
