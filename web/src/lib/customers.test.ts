import { describe, expect, test } from "bun:test";
import type { Customer } from "@/api/client";
import { byOrganization } from "@/lib/customers";

function customer(
  id: string,
  displayName: string,
  environment: string,
  extra: Partial<Customer> = {},
): Customer {
  return {
    id,
    displayName,
    environment,
    profile: "dev",
    ready: true,
    status: { phase: "Ready", message: "" },
    conditions: [],
    deletionProtection: false,
    createdAt: "2026-11-01T00:00:00Z",
    ...extra,
  } as Customer;
}

describe("byOrganization", () => {
  test("groups an organization's environments, production first, then by name, those being removed last", () => {
    const groups = byOrganization([
      customer("c-qa0001", "Hooli", "qa", { organizationID: "org_hooli" }),
      customer("c-old001", "Hooli", "archive", { organizationID: "org_hooli", deleting: true }),
      customer("demo", "Demo", "production", { organizationID: "org_demo" }),
      customer("c-prod01", "Hooli", "production", { organizationID: "org_hooli" }),
      customer("c-stg001", "Demo", "staging", { organizationID: "org_demo" }),
    ]);
    expect(groups.map((g) => g.map((c) => c.id))).toEqual([
      ["demo", "c-stg001"],
      ["c-prod01", "c-qa0001", "c-old001"],
    ]);
  });

  test("keeps customers without an organization apart, even of the same name", () => {
    expect(
      byOrganization([
        customer("demo", "Acme", "production", { organizationID: "" }),
        customer("acme", "Acme", "production", { organizationID: "" }),
      ]).map((g) => g.map((c) => c.id)),
    ).toEqual([["acme"], ["demo"]]);
    const groups = byOrganization([
      customer("c-aaaaaa", "Acme", "production"),
      customer("c-bbbbbb", "Acme", "production"),
    ]);
    expect(groups).toHaveLength(2);
  });
});
