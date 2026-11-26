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
    const key = customerKey(customer);
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

/**
 * Which customer an environment belongs to: its organization, or, without
 * one (an empty one from an older console too), the environment itself.
 */
export function customerKey(customer: Customer): string {
  return customer.organizationID || `customer:${customer.id}`;
}

/**
 * What a customer's folded environments come to: how many of those that
 * stay are ready, and how many are being deleted, which are never ready and
 * need nothing. Attention is wanted when one that stays is not ready.
 */
export function foldSummary(environments: Customer[]): { text: string; attention: boolean } {
  const staying = environments.filter((c) => !c.deleting);
  const ready = staying.filter((c) => c.ready).length;
  const deleting = environments.length - staying.length;
  const parts = [
    ...(staying.length > 0 ? [`${String(ready)} of ${String(staying.length)} ready`] : []),
    ...(deleting > 0 ? [`${String(deleting)} being deleted`] : []),
  ];
  return { text: parts.join(", "), attention: ready < staying.length };
}

/** The customers whose environments the operator folded away in this browser. */
export function collapsedCustomers(): Set<string> {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(collapsedKey) ?? "[]");
    return new Set(Array.isArray(stored) ? stored.filter((k) => typeof k === "string") : []);
  } catch {
    // Storage can be off, as in a private window, or hold something else.
    return new Set();
  }
}

/** Remembers in this browser which customers' environments are folded away. */
export function rememberCollapsed(keys: Set<string>) {
  try {
    localStorage.setItem(collapsedKey, JSON.stringify([...keys]));
  } catch {
    // They then show unfolded on the next visit.
  }
}

/** The key under which the folded customers are kept, for the storage event. */
export const collapsedKey = "changeport.operator.collapsed-customers";
