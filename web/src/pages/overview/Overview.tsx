import type { Customer } from "@/api/client";
import { StatusBadge } from "@/components/CustomerSummary";
import { customerLine } from "@/lib/customers";
import { Page } from "@/components/Page";
import { useEnvironmentCrumb } from "@/lib/useEnvironment";
import { AlertsBanner } from "@/pages/alerts/ActiveAlerts";
import { DiagnosesCard } from "@/pages/diagnoses/Diagnoses";
import { ConnectionCard } from "@/pages/overview/ConnectionCard";
import { ConsumerGroupsCard } from "@/pages/overview/ConsumerGroupsCard";
import { HealthCard } from "@/pages/overview/HealthCard";
import { ThroughputCard } from "@/pages/overview/ThroughputCard";
import { TopicsCard } from "@/pages/overview/TopicsCard";
import { TopologyCard } from "@/pages/overview/TopologyCard";

/**
 * A ready cluster: its active alerts, what needs attention, how to connect, its health,
 * throughput, how data flows through it, topics and consumer groups.
 */
export function Overview({ customer, admin }: { customer: Customer; admin: boolean }) {
  const crumbs = useEnvironmentCrumb();
  return (
    <Page
      crumbs={crumbs}
      title="Overview"
      badge={<StatusBadge customer={customer} />}
      description={customerLine(customer)}
    >
      <AlertsBanner />
      <DiagnosesCard />
      <ConnectionCard admin={admin} />
      <HealthCard />
      <ThroughputCard />
      <TopologyCard />
      <TopicsCard admin={admin} />
      <ConsumerGroupsCard />
    </Page>
  );
}
