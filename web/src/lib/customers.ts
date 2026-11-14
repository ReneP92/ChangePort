import type { Customer } from "@/api/client";

/** Which customer, environment and profile a cluster is, in words. */
export function customerLine(customer: Customer): string {
  return `Customer ${customer.id}, the ${customer.environment} environment on a ${customer.profile} cluster`;
}
