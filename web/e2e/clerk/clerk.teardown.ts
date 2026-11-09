import { test as teardown } from "@playwright/test";
import {
  clerkClient,
  kubectl,
  operatorEmail,
  readFixtures,
  runLabel,
  testCustomers,
  testTrialRequests,
} from "./fixtures";

// Removes this run's users, organizations and customers, but not the
// operator every run shares. The provisioning controller deletes the
// customers' clusters in the background.
teardown("remove this run's users, organizations and customers", async () => {
  const fixtures = readFixtures();
  const clerk = clerkClient();
  for (const user of Object.values(fixtures.users)) {
    if (user.email !== operatorEmail) {
      await clerk.users.deleteUser(user.id);
    }
  }
  for (const organization of fixtures.organizations) {
    await clerk.organizations.deleteOrganization(organization);
  }
  // A test that failed may have left its customer protected, which would
  // refuse the deletion.
  for (const name of kubectl([
    "get",
    "customers",
    "-l",
    `${runLabel}=${fixtures.run}`,
    "-o",
    "name",
  ])
    .split("\n")
    .filter(Boolean)) {
    kubectl(["patch", name, "--type=merge", "-p", '{"spec":{"deletionProtection":false}}']);
  }
  kubectl(["delete", "customers", "-l", `${runLabel}=${fixtures.run}`, "--wait=false"]);
  // What the tests provisioned through the operator console is named like
  // the run's organizations, though not labelled.
  const prefix = `E2E ${fixtures.run} `;
  const provisioned = await clerk.organizations.getOrganizationList({ query: prefix, limit: 100 });
  for (const o of provisioned.data) {
    if (o.name.startsWith(prefix) && !fixtures.organizations.includes(o.id)) {
      await clerk.organizations.deleteOrganization(o.id);
    }
  }
  for (const c of testCustomers()) {
    if (c.spec.displayName.startsWith(prefix)) {
      kubectl(["delete", "customer", c.metadata.name, "--wait=false", "--ignore-not-found"]);
    }
  }
  // The trial requests the tests made, for companies named the same way.
  for (const r of testTrialRequests()) {
    if (r.spec.company.startsWith(prefix)) {
      kubectl(["delete", "trialrequest", r.metadata.name, "--wait=false", "--ignore-not-found"]);
    }
  }
});
