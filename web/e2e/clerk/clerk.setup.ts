import { randomBytes } from "node:crypto";
import { isClerkAPIResponseError } from "@clerk/backend/errors";
import { clerkSetup } from "@clerk/testing/playwright";
import { test as setup } from "@playwright/test";
import {
  abandonedAfterMs,
  clerkClient,
  env,
  kubectl,
  operatorEmail,
  runLabel,
  testEmail,
  testEmailPattern,
  testCustomers,
  testOrganizationPattern,
  writeFixtures,
  type CustomerFixture,
  type Fixtures,
  type UserFixture,
} from "./fixtures";

// Creates this run's Clerk users and organizations, and a customer for each
// organization, after removing what runs that died left behind.
setup("create this run's users, organizations and customers", async () => {
  // Creating customer A's cluster takes a minute or two.
  setup.setTimeout(6 * 60_000);
  const clerk = clerkClient();
  await removeAbandoned();

  // Clerk's testing token lets the browser past its bot protection.
  await clerkSetup({ publishableKey: env("CLERK_PUBLISHABLE_KEY") });

  const run = `e2e${Date.now().toString(36).slice(-5)}${randomBytes(2).toString("hex")}`;
  const user = async (role: string) => {
    const created = await clerk.users.createUser({
      emailAddress: [testEmail(run, role)],
      firstName: "E2E",
      lastName: role,
      skipPasswordRequirement: true,
    });
    return { id: created.id, email: testEmail(run, role) };
  };
  const users = {
    formA: await user("form-a"),
    signOutA: await user("sign-out-a"),
    memberA: await user("member-a"),
    adminA: await user("admin-a"),
    memberB: await user("member-b"),
    staff: await sharedOperator(),
  };
  const organizationA = await clerk.organizations.createOrganization({ name: `E2E ${run} A` });
  const organizationB = await clerk.organizations.createOrganization({ name: `E2E ${run} B` });
  const memberships: [string, string, "org:member" | "org:admin"][] = [
    [organizationA.id, users.formA.id, "org:member"],
    [organizationA.id, users.signOutA.id, "org:member"],
    [organizationA.id, users.memberA.id, "org:member"],
    [organizationA.id, users.adminA.id, "org:admin"],
    [organizationB.id, users.memberB.id, "org:member"],
  ];
  for (const [organizationId, userId, role] of memberships) {
    await clerk.organizations.createOrganizationMembership({ organizationId, userId, role });
  }

  const customer = (suffix: string, organizationID: string): CustomerFixture => ({
    id: `${run}-${suffix}`,
    displayName: `E2E ${run} ${suffix.toUpperCase()}`,
    organizationID,
  });
  const customers = { a: customer("a", organizationA.id), b: customer("b", organizationB.id) };
  // The screenshot tour provisions customers of its own, through the
  // operator console.
  const tour = Boolean(process.env.CHANGEPORT_SCREENSHOTS_DIR);
  for (const c of tour ? [] : Object.values(customers)) {
    kubectl(
      ["apply", "--server-side", "--field-manager=changeport-e2e", "-f", "-"],
      JSON.stringify({
        apiVersion: "changeport.io/v1alpha1",
        kind: "Customer",
        metadata: { name: c.id, labels: { [runLabel]: run } },
        spec: { displayName: c.displayName, profile: "dev", clerkOrganizationID: c.organizationID },
      }),
    );
  }
  const fixtures: Fixtures = {
    run,
    customers,
    users,
    organizations: [organizationA.id, organizationB.id],
  };
  writeFixtures(fixtures);

  // The fixtures are written first, so that the teardown removes everything
  // even when this fails. Customer A's cluster serves the overview tests,
  // which need it ready; B only needs to exist.
  if (!tour) {
    kubectl(["wait", `customer/${customers.a.id}`, "--for=condition=Ready", "--timeout=5m"]);
  }
});

/**
 * Returns the operator every run shares, creating it the first time, and
 * keeps it in the staff organization.
 */
async function sharedOperator(): Promise<UserFixture> {
  const clerk = clerkClient();
  const find = async () =>
    (await clerk.users.getUserList({ emailAddress: [operatorEmail] })).data.at(0);
  let operator = await find();
  if (!operator) {
    try {
      operator = await clerk.users.createUser({
        emailAddress: [operatorEmail],
        firstName: "E2E",
        lastName: "operator",
        skipPasswordRequirement: true,
      });
    } catch (error) {
      // Another run created it at the same moment.
      operator = await find();
      if (!operator) {
        throw error;
      }
    }
  }
  try {
    await clerk.organizations.createOrganizationMembership({
      organizationId: env("CHANGEPORT_STAFF_ORG_ID"),
      userId: operator.id,
      role: "org:member",
    });
  } catch (error) {
    const member =
      isClerkAPIResponseError(error) &&
      error.errors.some((e) => e.code === "already_a_member_in_organization");
    if (!member) {
      throw error;
    }
  }
  return { id: operator.id, email: operatorEmail };
}

/**
 * The janitor: removes the test users, organizations and customers of runs
 * that started over an hour ago and never cleaned up, so that a crashed run
 * costs nothing for long. Younger ones may belong to a run in progress.
 */
async function removeAbandoned(): Promise<void> {
  const clerk = clerkClient();
  const cutoff = Date.now() - abandonedAfterMs;
  // Clerk's search finds nothing for a query with + or @ in it, so this
  // searches for every test address and the pattern picks the runs' users.
  const users = await clerk.users.getUserList({ query: "clerk_test", limit: 100 });
  for (const u of users.data) {
    const email = u.primaryEmailAddress?.emailAddress ?? "";
    if (testEmailPattern.test(email) && u.createdAt < cutoff) {
      await clerk.users.deleteUser(u.id);
    }
  }
  const organizations = await clerk.organizations.getOrganizationList({
    query: "E2E e2e",
    limit: 100,
  });
  for (const o of organizations.data) {
    if (testOrganizationPattern.test(o.name) && o.createdAt < cutoff) {
      await clerk.organizations.deleteOrganization(o.id);
    }
  }
  for (const c of testCustomers()) {
    if (Date.parse(c.metadata.creationTimestamp) < cutoff) {
      kubectl(["delete", "customer", c.metadata.name, "--wait=false"]);
    }
  }
}
