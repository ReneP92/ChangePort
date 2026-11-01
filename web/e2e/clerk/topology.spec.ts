import { expect, test } from "@playwright/test";
import { readFixtures } from "./fixtures";
import {
  connect,
  downloadClientProperties,
  expectNoSidewaysScroll,
  kafka,
  kafkaInBackground,
  openOverview,
} from "./helpers";

// The live topology map on customer A's overview (docs/console.md): a
// producer that reports its own metrics, the topic it writes and the
// consumer group that reads it, with the traffic between them.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

test("the map shows a producer, its topic and the group that reads it", async ({
  page,
}, testInfo) => {
  test.setTimeout(6 * 60_000);
  const a = fixtures.customers.a;
  const topic = `flow-${fixtures.run}`;
  const producer = `flow-producer-${fixtures.run}`;
  const group = `flow-readers-${fixtures.run}`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);
  kafka(a.id, properties, "kafka-topics.sh", [
    ...connect(a.id),
    "--create",
    "--topic",
    topic,
    "--partitions",
    "1",
  ]);
  // A producer and a consumer group, running for two minutes, as clients
  // that report their own metrics (KIP-714).
  const bootstrap = connect(a.id)[1] ?? "";
  const running = [
    kafkaInBackground(a.id, properties, "kafka-producer-perf-test.sh", [
      "--topic",
      topic,
      "--num-records",
      "12000",
      "--record-size",
      "200",
      "--throughput",
      "100",
      "--producer.config",
      "/config/client.properties",
      "--producer-props",
      `bootstrap.servers=${bootstrap}`,
      `client.id=${producer}`,
    ]),
    kafkaInBackground(a.id, properties, "kafka-console-consumer.sh", [
      ...connect(a.id),
      "--topic",
      topic,
      "--group",
      group,
      "--timeout-ms",
      "60000",
    ]).catch(() => ""),
  ];

  const map = page.getByRole("group", { name: "Topology map" });
  // Once the clients' metrics have reached the metrics store, and the map
  // has read them again.
  const producerNode = map.getByTitle(new RegExp(`^${producer}: admin, `));
  await expect(producerNode).toBeVisible({ timeout: 4 * 60_000 });
  const topicNode = map.getByRole("link", { name: new RegExp(`^${topic}`) });
  await expect(topicNode).toBeVisible();
  await expect(topicNode).toContainText(/KB\/s in|B\/s in/);
  const groupNode = map.getByRole("link", { name: new RegExp(`^${group}`) });
  await expect(groupNode).toBeVisible({ timeout: 2 * 60_000 });
  await expect(groupNode).toContainText("1 member");
  // Producer to topic, and topic to group.
  await expect(map.locator("svg path")).not.toHaveCount(0);
  const producerBox = await producerNode.boundingBox();
  const topicBox = await topicNode.boundingBox();
  const groupBox = await groupNode.boundingBox();
  expect(
    producerBox && topicBox && groupBox && producerBox.x < topicBox.x && topicBox.x < groupBox.x,
  ).toBe(true);
  await expectNoSidewaysScroll(page);

  // The group's node leads to its page.
  await groupNode.click();
  await expect(page.getByRole("heading", { level: 1, name: group })).toBeVisible();
  await Promise.all(running);
});
