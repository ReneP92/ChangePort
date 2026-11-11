import type { ConsumerGroupLag, TailMessage, Topic } from "@/api/client";
import type { Example } from "@/lib/examples";

/**
 * Whether a viewer is done with the welcome page for a customer, finished
 * or skipped, as this browser remembers it.
 */
export function onboardingDone(customerID: string): boolean {
  try {
    return localStorage.getItem(storageKey(customerID)) === "done";
  } catch {
    // Storage can be off, as in a private window.
    return false;
  }
}

/** Remembers in this browser that the viewer is done with the welcome page. */
export function finishOnboarding(customerID: string) {
  try {
    localStorage.setItem(storageKey(customerID), "done");
  } catch {
    // The welcome page then shows again on the next visit.
  }
}

function storageKey(customerID: string): string {
  return `changeport.onboarding.${customerID}`;
}

/**
 * Whether a viewer lands on the welcome page rather than the overview:
 * while their cluster is not ready or has no topics yet, those of a
 * Postgres source included, unless they are done with it.
 */
export function shouldWelcome(done: boolean, ready: boolean, topics: Topic[] | undefined): boolean {
  if (done) {
    return false;
  }
  return !ready || topics?.length === 0;
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

/** How far the customer's first message is. */
export interface FirstMessage {
  /** A message reached the topic. */
  arrived: boolean;
  /** The consumer group that read the topic, once one has. */
  readBy: string | undefined;
  /** The messages that arrived, newest first, for admins. */
  messages: TailMessage[];
}

/**
 * Whether the customer's first message reached their topic since they got
 * to that step, at since: admins see it arrive in the topic's tail, and
 * members by the topic's traffic. Then the consumer group that reads it, as
 * the examples consume what they produced. A topic that had messages
 * before counts only the new ones, give or take a clock's skew.
 */
export function firstMessage(
  topic: string,
  since: number,
  tailed: TailMessage[],
  topics: Topic[] | undefined,
  groups: ConsumerGroupLag[] | undefined,
): FirstMessage {
  const fresh = tailed.filter((message) => Date.parse(message.timestamp) >= since - clockSkew);
  const traffic = (topics?.find((t) => t.name === topic)?.bytesInPerSecond ?? 0) > 0;
  const arrived = fresh.length > 0 || traffic;
  const readBy = arrived ? groups?.find((group) => group.topic === topic)?.group : undefined;
  return { arrived, readBy, messages: fresh };
}

/** How far the browser's clock and the producer's may be apart. */
const clockSkew = 30_000;

/**
 * The languages the welcome page shows examples in, by the headings of the
 * connection guide, in order: the guide itself also has Java and the
 * command line.
 */
export const onboardingLanguages = ["Python", "Go", "Node.js"];

/** The guide's examples in the welcome page's languages, in their order. */
export function onboardingExamples(examples: Example[]): Example[] {
  return onboardingLanguages.flatMap((language) =>
    examples.filter((example) => example.language === language),
  );
}
