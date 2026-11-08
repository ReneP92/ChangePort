import type { NewCustomer } from "@/api/client";

export type Profile = NewCustomer["profile"];

/** The profiles, as operators choose among them. */
export const profiles: { name: Profile; description: string }[] = [
  { name: "dev", description: "One broker, for trying Kafka out" },
  { name: "dev-ha", description: "Three brokers in three zones, small" },
  { name: "production", description: "Three brokers in three zones, sized for production" },
];
