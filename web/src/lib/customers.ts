import type { Customer } from "@/api/client";

/** Which customer, environment and profile a cluster is, in words. */
export function customerLine(customer: Customer): string {
  return `Customer ${customer.id}, the ${customer.environment} environment on a ${customer.profile} cluster`;
}

/**
 * Customers grouped by their organization, each a customer with its
 * environments: by name, and within one production first, then by name, and
 * those being removed last, as the customer's console orders them. One
 * without an organization is a customer of its own.
 */
export function byOrganization(customers: Customer[]): Customer[][] {
  const groups = new Map<string, Customer[]>();
  for (const customer of customers) {
    // One without an organization, an empty one from an older console too,
    // is a customer of its own.
    const key = customer.organizationID || `customer:${customer.id}`;
    groups.set(key, [...(groups.get(key) ?? []), customer]);
  }
  const rank = (c: Customer) => (c.deleting ? 2 : 0) + (c.environment === "production" ? 0 : 1);
  const environments = [...groups.values()].map((group) =>
    group.toSorted(
      (a, b) =>
        rank(a) - rank(b) || a.environment.localeCompare(b.environment) || a.id.localeCompare(b.id),
    ),
  );
  const name = (group: Customer[]) => group[0]?.displayName ?? "";
  const id = (group: Customer[]) => group[0]?.id ?? "";
  return environments.toSorted(
    (a, b) => name(a).localeCompare(name(b)) || id(a).localeCompare(id(b)),
  );
}
