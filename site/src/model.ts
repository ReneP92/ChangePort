/**
 * The hero's model of a customer's streams, as both scripts/streams.ts,
 * which draws it, and main.ts, which plays its story, know it.
 */

export type Source = "pg" | "checkout" | "payments";
export type App = "billing" | "fraud" | "search" | "warehouse";

export const names: Record<Source | App, string> = {
  pg: "Postgres: shop",
  checkout: "checkout-api",
  payments: "payments-api",
  billing: "billing",
  fraud: "fraud-checks",
  search: "search-indexer",
  warehouse: "warehouse-sync",
};

/** Each source's color, which its changes, labels and topics show. */
export const colors: Record<Source, string> = {
  pg: "#2f9e8f",
  checkout: "#3b6fd8",
  payments: "#9457e0",
};

/** The topic each source's changes go to. */
export const topics: Record<Source, string> = {
  pg: "shop.public.orders",
  checkout: "orders",
  payments: "payments",
};

/** The sources each app subscribes to. */
export const subscriptions: Record<App, Source[]> = {
  billing: ["pg", "payments"],
  fraud: ["checkout", "payments"],
  search: ["checkout"],
  warehouse: ["pg", "checkout"],
};

/**
 * What Changeport watches for changes waiting: the Postgres source it
 * reads, and every app. checkout-api and payments-api write to Kafka
 * themselves, so what waits in them is theirs to see.
 */
export const watched: (Source | App)[] = ["pg", "billing", "fraud", "search", "warehouse"];

/** The levels a stack has, the level past which Changeport alerts, and far behind. */
export const levels = 8;
export const threshold = 4;
export const severe = 7;

/**
 * The second of main.ts's story the page starts at, and the level each
 * watched block has then: search-indexer behind, as without the script.
 */
export const opening = 8;
export const start: Partial<Record<Source | App, number>> = {
  pg: 1,
  billing: 1,
  fraud: 2,
  search: 6,
  warehouse: 1,
};
