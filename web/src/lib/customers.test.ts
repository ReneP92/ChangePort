import { afterEach, describe, expect, test } from "bun:test";
import type { Customer } from "@/api/client";
import {
  byOrganization,
  collapsedCustomers,
  customerKey,
  foldSummary,
  rememberCollapsed,
} from "@/lib/customers";

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

describe("collapsed customers", () => {
  afterEach(() => {
    localStorage.clear();
  });

  test("are remembered in this browser, none at first", () => {
    expect(collapsedCustomers()).toEqual(new Set());
    rememberCollapsed(new Set(["org_hooli", "customer:acme"]));
    expect(collapsedCustomers()).toEqual(new Set(["org_hooli", "customer:acme"]));
  });

  test("are none when storage holds something else", () => {
    localStorage.setItem("changeport.operator.collapsed-customers", "{not json");
    expect(collapsedCustomers()).toEqual(new Set());
    localStorage.setItem("changeport.operator.collapsed-customers", '{"org_hooli":true}');
    expect(collapsedCustomers()).toEqual(new Set());
  });

  test("are keyed by organization, or by the environment without one", () => {
    expect(
      customerKey(customer("c-prod01", "Hooli", "production", { organizationID: "org_hooli" })),
    ).toBe("org_hooli");
    expect(customerKey(customer("acme", "Acme", "production", { organizationID: "" }))).toBe(
      "customer:acme",
    );
  });
});

describe("foldSummary", () => {
  const env = (id: string, extra: Partial<Customer>) =>
    customer(id, "Hooli", id, { organizationID: "org_hooli", ...extra });

  test("counts the ready among those that stay, and wants attention for one that is not", () => {
    expect(foldSummary([env("a", { ready: true }), env("b", { ready: true })])).toEqual({
      text: "2 of 2 ready",
      attention: false,
    });
    expect(foldSummary([env("a", { ready: true }), env("b", { ready: false })])).toEqual({
      text: "1 of 2 ready",
      attention: true,
    });
  });

  test("says which are being deleted, which want no attention", () => {
    expect(
      foldSummary([env("a", { ready: true }), env("b", { ready: false, deleting: true })]),
    ).toEqual({
      text: "1 of 1 ready, 1 being deleted",
      attention: false,
    });
    expect(
      foldSummary([
        env("a", { ready: false, deleting: true }),
        env("b", { ready: false, deleting: true }),
      ]),
    ).toEqual({ text: "2 being deleted", attention: false });
  });
});
