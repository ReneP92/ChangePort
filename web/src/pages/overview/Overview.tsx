import type { Customer } from "@/api/client";
import { StatusBadge } from "@/components/CustomerSummary";
import { AlertsBanner } from "@/pages/alerts/ActiveAlerts";
import { ConnectionCard } from "@/pages/overview/ConnectionCard";
import { ConsumerGroupsCard } from "@/pages/overview/ConsumerGroupsCard";
import { HealthCard } from "@/pages/overview/HealthCard";
import { ThroughputCard } from "@/pages/overview/ThroughputCard";
import { TopicsCard } from "@/pages/overview/TopicsCard";
import { TopologyCard } from "@/pages/overview/TopologyCard";

/**
 * A ready cluster: its active alerts, how to connect, its health,
 * throughput, how data flows through it, topics and consumer groups.
 */
export function Overview({ customer, admin }: { customer: Customer; admin: boolean }) {
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{customer.displayName}</h1>
          <StatusBadge customer={customer} />
        </div>
        <p className="text-sm text-muted-foreground">
          Customer {customer.id}, the {customer.environment} environment on a {customer.profile}{" "}
          cluster
        </p>
      </header>
      <AlertsBanner />
      <ConnectionCard admin={admin} />
      <HealthCard />
      <ThroughputCard />
      <TopologyCard />
      <TopicsCard admin={admin} />
      <ConsumerGroupsCard />
    </main>
  );
}
