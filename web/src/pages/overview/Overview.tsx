import type { Customer } from "@/api/client";
import { StatusBadge } from "@/components/CustomerSummary";
import { customerLine } from "@/lib/customers";
import { Page } from "@/components/Page";
import { useEnvironmentCrumb } from "@/lib/useEnvironment";
import { AlertsBanner } from "@/pages/alerts/ActiveAlerts";
import { DiagnosesCard } from "@/pages/diagnoses/Diagnoses";
import { ConsumerGroupsCard } from "@/pages/overview/ConsumerGroupsCard";
import { HealthCard } from "@/pages/overview/HealthCard";
import { ThroughputCard } from "@/pages/overview/ThroughputCard";
import { TopicsCard } from "@/pages/overview/TopicsCard";
import { TopologyCard } from "@/pages/overview/TopologyCard";

/**
 * A ready cluster: its active alerts and what needs attention, which ask the
 * customer to act, first; then its health, throughput, how data flows
 * through it, topics and consumer groups. How to connect has a page of its
 * own (ConnectPage).
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
      <HealthCard />
      <ThroughputCard />
      <TopologyCard />
      <TopicsCard admin={admin} />
      <ConsumerGroupsCard />
    </Page>
  );
}
