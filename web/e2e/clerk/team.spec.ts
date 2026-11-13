import { clerk } from "@clerk/testing/playwright";
import { expect, test, type Page } from "@playwright/test";
import { clerkClient, readFixtures, testEmail } from "./fixtures";
import { expectNoSidewaysScroll, openOverview } from "./helpers";

// The team page (docs/console.md): a person belongs to one organization, so
// an invitation to an address that belongs, or is invited, to another is
// refused, and the organization keeps an admin.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

async function openTeam(page: Page, user: (typeof fixtures)["users"]["adminA"]) {
  await openOverview(page, user);
  await page
    .getByRole("navigation", { name: "Console" })
    .getByRole("link", { name: "Team" })
    .click();
  await expect(page.getByRole("heading", { name: "Team", level: 1 })).toBeVisible();
  return page.getByRole("main");
}

test("an admin invites colleagues, but not someone of another organization", async ({ page }) => {
  const main = await openTeam(page, fixtures.users.adminA);
  const members = main.getByRole("list", { name: "Members" });
  await expect(members.getByRole("listitem").filter({ hasText: "You" })).toContainText(
    "E2E admin-a",
  );
  await expect(members.getByRole("listitem").filter({ hasText: "E2E member-a" })).toBeVisible();
  await expectNoSidewaysScroll(page);

  const email = main.getByLabel("Email address");
  await email.fill(fixtures.users.memberB.email);
  await main.getByRole("button", { name: "Invite" }).click();
  await expect(main.getByRole("alert")).toContainText(
    "belongs, or is invited, to another organization",
  );

  const invitee = testEmail(fixtures.run, "invitee");
  await email.fill(invitee);
  await main.getByRole("button", { name: "Invite" }).click();
  const pending = main
    .getByRole("list", { name: "Pending invitations" })
    .getByRole("listitem")
    .filter({ hasText: invitee });
  await expect(pending).toContainText("Member");
  await expect(email).toHaveValue("");
  // Invited, the address cannot join a second organization either.
  await email.fill(invitee);
  await main.getByRole("button", { name: "Invite" }).click();
  await expect(main.getByRole("alert")).toContainText(
    "is a member of your organization, or invited to it",
  );
  await pending.getByRole("button", { name: "Revoke" }).click();
  await expect(pending).toHaveCount(0);
});

test("an admin changes roles and removes members, and the organization keeps an admin", async ({
  page,
}) => {
  const clerkAPI = clerkClient();
  const email = testEmail(fixtures.run, "teammate");
  const teammate = await clerkAPI.users.createUser({
    emailAddress: [email],
    firstName: "E2E",
    lastName: "teammate",
    skipPasswordRequirement: true,
  });
  try {
    await clerkAPI.organizations.createOrganizationMembership({
      organizationId: fixtures.customers.a.organizationID,
      userId: teammate.id,
      role: "org:member",
    });
    const main = await openTeam(page, fixtures.users.adminA);
    const members = main.getByRole("list", { name: "Members" });

    // Alone as admin, the admin cannot step down.
    await members.getByRole("combobox", { name: "Role of E2E admin-a" }).click();
    await page.getByRole("option", { name: "Member" }).click();
    await expect(main.getByRole("alert")).toContainText("keeps at least one admin");

    const role = members.getByRole("combobox", { name: "Role of E2E teammate" });
    await role.click();
    await page.getByRole("option", { name: "Admin" }).click();
    await expect(role).toHaveText("Admin");
    await role.click();
    await page.getByRole("option", { name: "Member" }).click();
    await expect(role).toHaveText("Member");

    await members.getByRole("button", { name: "Remove E2E teammate" }).click();
    const dialog = page.getByRole("dialog", { name: /^Remove E2E teammate from / });
    await dialog.getByRole("button", { name: "Remove" }).click();
    await expect(dialog).toBeHidden();
    await expect(members.getByRole("listitem").filter({ hasText: "E2E teammate" })).toHaveCount(0);
  } finally {
    await clerkAPI.users.deleteUser(teammate.id);
  }
});

test("members see the team, and ask an admin to change it", async ({ page }) => {
  const main = await openTeam(page, fixtures.users.memberA);
  await expect(
    main.getByText("Ask an admin of your organization to invite a colleague."),
  ).toBeVisible();
  await expect(main.getByLabel("Email address")).toHaveCount(0);
  await expect(main.getByRole("combobox")).toHaveCount(0);
  await expect(main.getByRole("button", { name: /^Remove / })).toHaveCount(0);
});

test("an operator cannot invite someone of another organization as a customer's admin", async ({
  page,
}) => {
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: fixtures.users.staff.email });
  await page.goto("/operator");
  await page.getByRole("button", { name: "Provision customer" }).click();
  const dialog = page.getByRole("dialog", { name: "Provision a customer" });
  await dialog.getByLabel("Name").fill(`E2E ${fixtures.run} R`);
  await dialog.getByRole("radio", { name: /^dev One broker/ }).check();
  await dialog.getByLabel("Invite an admin (optional)").fill(fixtures.users.memberA.email);
  await dialog.getByRole("button", { name: "Provision", exact: true }).click();
  await expect(dialog).toContainText("belongs, or is invited, to an organization already");
  await expect(page).toHaveURL(/\/operator$/);
});
