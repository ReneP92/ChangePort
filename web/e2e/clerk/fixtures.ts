import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { createClerkClient, type ClerkClient } from "@clerk/backend";
import type { Page } from "@playwright/test";

/**
 * One run's Clerk users and organizations, and the customers of those
 * organizations, which clerk.setup.ts creates and clerk.teardown.ts removes.
 */
export interface Fixtures {
  run: string;
  customers: { a: CustomerFixture; b: CustomerFixture };
  /**
   * Each test that signs in through the browser has users of its own, so
   * that no test's sign-out or session touches another's.
   */
  users: {
    /** Members of customer A's organization, for the form sign-in and the sign-out. */
    formA: UserFixture;
    signOutA: UserFixture;
    /** A member of customer A's organization. */
    memberA: UserFixture;
    /** An admin of customer A's organization. */
    adminA: UserFixture;
    /** A member of customer B's organization. */
    memberB: UserFixture;
    /**
     * The operator every run shares, a member of the Changeport staff
     * organization (operatorEmail); the teardown keeps it.
     */
    staff: UserFixture;
  };
  organizations: string[];
}

export interface CustomerFixture {
  id: string;
  displayName: string;
  organizationID: string;
}

export interface UserFixture {
  id: string;
  email: string;
}

/** Where the setup leaves the run's fixtures for the tests. */
const fixturesFile = path.join(
  import.meta.dirname,
  "..",
  "..",
  "playwright",
  ".clerk",
  "fixtures.json",
);

export function writeFixtures(fixtures: Fixtures): void {
  mkdirSync(path.dirname(fixturesFile), { recursive: true });
  writeFileSync(fixturesFile, JSON.stringify(fixtures, null, 2));
}

export function readFixtures(): Fixtures {
  return JSON.parse(readFileSync(fixturesFile, "utf8")) as Fixtures;
}

/** Fixtures older than this belong to runs that died; the janitor removes them. */
export const abandonedAfterMs = 60 * 60 * 1000;

/** Test users are Clerk test addresses, which take the fixed code 424242. */
export const testEmail = (run: string, role: string) => `${run}-${role}+clerk_test@example.com`;
/**
 * The operator every run shares. Clerk allows an organization 5 members, so
 * an operator of its own per run would let only a few runs start at once,
 * and the operators of runs that died would lock the rest out. Neither the
 * teardown nor the janitor removes it; testEmailPattern does not match it.
 */
export const operatorEmail = "e2e-operator+clerk_test@example.com";
export const testEmailPattern = /^e2e[a-z0-9]+-[a-z0-9-]+\+clerk_test@example\.com$/;
export const testOrganizationPattern = /^E2E e2e[a-z0-9]+ [A-Z]$/;
/** Labels the customers the tests create, by run. */
export const runLabel = "changeport.io/e2e-run";

/** Reads a setting from the environment, which just ui-e2e fills. */
export function env(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is not set; run the signed-in tests with just ui-e2e`);
  }
  return value;
}

export function clerkClient(): ClerkClient {
  return createClerkClient({ secretKey: env("CLERK_SECRET_KEY") });
}

/** Runs the pinned kubectl against the kind cluster, as hack/ui-e2e.sh sets it. */
export function kubectl(args: string[], input?: string): string {
  return execFileSync(env("KUBECTL"), ["--context", env("KUBE_CONTEXT"), ...args], {
    input,
    encoding: "utf8",
    stdio: ["pipe", "pipe", "inherit"],
  });
}

/**
 * Every customer of any test run: those the setup creates, which carry the
 * run label, and those the tests provision through the operator console,
 * which are named like the runs' organizations.
 */
export function testCustomers(): {
  metadata: { name: string; creationTimestamp: string };
  spec: { displayName: string };
}[] {
  const customers = JSON.parse(kubectl(["get", "customers", "-o", "json"])) as {
    items: {
      metadata: { name: string; creationTimestamp: string; labels?: Record<string, string> };
      spec: { displayName: string };
    }[];
  };
  return customers.items.filter(
    (c) =>
      c.metadata.labels?.[runLabel] !== undefined ||
      testOrganizationPattern.test(c.spec.displayName),
  );
}

/** Every trial request a test user made, the users' addresses being test addresses. */
export function testTrialRequests(): {
  metadata: { name: string; creationTimestamp: string };
  spec: { email: string; company: string };
}[] {
  const requests = JSON.parse(kubectl(["get", "trialrequests", "-o", "json"])) as {
    items: {
      metadata: { name: string; creationTimestamp: string };
      spec: { email: string; company: string };
    }[];
  };
  return requests.items.filter((r) => testEmailPattern.test(r.spec.email));
}

/**
 * Marks the welcome page done for the run's customers in the page's
 * browser, as for a returning customer, so that their users land on the
 * overview: other tests may or may not have given customer A a topic yet,
 * which decides where a newcomer lands (web/src/lib/onboarding.ts).
 */
export async function skipWelcome(page: Page): Promise<void> {
  const { customers } = readFixtures();
  await page.addInitScript(
    (ids) => {
      for (const id of ids) {
        localStorage.setItem(`changeport.onboarding.${id}`, "done");
      }
    },
    [customers.a.id, customers.b.id],
  );
}
