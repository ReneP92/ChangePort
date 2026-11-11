import type { ConsumerGroupLag, SampleApp, Topic } from "@/api/client";

/**
 * Whether a viewer chose to see the getting started guide: auto shows it
 * until the customer's own app sends data, hidden and shown are what they
 * chose since.
 */
export type GuideChoice = "auto" | "hidden" | "shown";

/** How far a customer is through the guide's steps. */
export interface GuideProgress {
  /** The sample app runs or starts. */
  sample: boolean;
  /** The first of the customer's own topics, once there is one. */
  topic: string | undefined;
  /** What shows the customer's own app connected, once something does. */
  app: string | undefined;
}

/**
 * Works out how far a customer is: their own topics are all but the sample
 * app's, those of their Postgres sources too, and their own app shows by
 * the data it writes to one, by the data one holds, so that a producer gone
 * quiet overnight still counts, or by a consumer group reading one. The
 * sample's topic is left out by its name too, as it lingers a moment once
 * the sample stops, no longer marked, with the sample's traffic.
 */
export function guideProgress(
  sample: SampleApp | undefined,
  topics: Topic[],
  groups: ConsumerGroupLag[],
): GuideProgress {
  const own = topics.filter((topic) => !topic.sample && topic.name !== sample?.topic);
  const writing = own.find((topic) => (topic.bytesInPerSecond ?? 0) > 0);
  const holding = own.find((topic) => (topic.localBytes ?? 0) + (topic.remoteBytes ?? 0) > 0);
  const reading = groups.find(
    (group) => group.group !== sample?.group && own.some((topic) => topic.name === group.topic),
  );
  let app: string | undefined;
  if (writing) {
    app = `Data arrives on ${writing.name}`;
  } else if (reading) {
    app = `Consumer group ${reading.group} reads ${reading.topic}`;
  } else if (holding) {
    app = `${holding.name} holds your app's data`;
  }
  return { sample: sample !== undefined && sample.state !== "stopped", topic: own[0]?.name, app };
}

/**
 * Whether the guide shows: as the viewer chose, or else until the customer
 * has a topic and an app of their own and no sample runs.
 */
export function guideVisible(choice: GuideChoice, progress: GuideProgress): boolean {
  if (choice !== "auto") {
    return choice === "shown";
  }
  return progress.sample || progress.topic === undefined || progress.app === undefined;
}

function storageKey(customerID: string): string {
  return `changeport.get-started.${customerID}`;
}

/** The viewer's choice for a customer, remembered in this browser. */
export function loadGuideChoice(customerID: string): GuideChoice {
  try {
    const choice = localStorage.getItem(storageKey(customerID));
    return choice === "hidden" || choice === "shown" ? choice : "auto";
  } catch {
    // Storage can be off, as in a private window.
    return "auto";
  }
}

export function saveGuideChoice(customerID: string, choice: GuideChoice) {
  try {
    localStorage.setItem(storageKey(customerID), choice);
  } catch {
    // The choice then lasts until the page reloads.
  }
}

/**
 * The environment the examples of the connection guide read, for a
 * cluster: the password comes from client.properties, which admins
 * download, so that it never shows on screen.
 */
export function exampleEnvironment(bootstrap: string, username: string, topic: string): string {
  return [
    `export KAFKA_BOOTSTRAP=${bootstrap}`,
    `export KAFKA_USERNAME=${username}`,
    `export KAFKA_PASSWORD="$(sed -n 's/.*password="\\([^"]*\\)".*/\\1/p' client.properties)"`,
    "# On AWS, the ID of the availability zone your app runs in, such as use1-az1",
    'export KAFKA_CLIENT_RACK=""',
    `export TOPIC=${topic}`,
  ].join("\n");
}
