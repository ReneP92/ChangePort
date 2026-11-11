import { useState } from "react";
import type { Customer } from "@/api/client";
import { useConsumerGroups, useSample, useTopics } from "@/api/queries";
import { StatusBadge } from "@/components/CustomerSummary";
import {
  guideProgress,
  guideVisible,
  loadGuideChoice,
  saveGuideChoice,
  type GuideChoice,
} from "@/lib/getStarted";
import { AlertsBanner } from "@/pages/alerts/ActiveAlerts";
import { ConnectionCard } from "@/pages/overview/ConnectionCard";
import { ConsumerGroupsCard } from "@/pages/overview/ConsumerGroupsCard";
import { GetStartedCard } from "@/pages/overview/GetStartedCard";
import { HealthCard } from "@/pages/overview/HealthCard";
import { ThroughputCard } from "@/pages/overview/ThroughputCard";
import { TopicsCard } from "@/pages/overview/TopicsCard";
import { TopologyCard } from "@/pages/overview/TopologyCard";

/**
 * A ready cluster: its active alerts, the getting started guide while the
 * customer needs it, how to connect, its health, throughput, how data flows
 * through it, topics and consumer groups.
 */
export function Overview({ customer, admin }: { customer: Customer; admin: boolean }) {
  const guide = useGuide(customer.id);
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{customer.displayName}</h1>
          <StatusBadge customer={customer} />
        </div>
        <p className="text-sm text-muted-foreground">
          Customer {customer.id}, {customer.profile} cluster
        </p>
      </header>
      <AlertsBanner />
      {guide.visible && (
        <GetStartedCard
          customer={customer}
          admin={admin}
          onHide={() => {
            guide.choose("hidden");
          }}
        />
      )}
      <ConnectionCard
        admin={admin}
        onShowGuide={
          guide.visible || !guide.known
            ? undefined
            : () => {
                guide.choose("shown");
              }
        }
      />
      <HealthCard />
      <ThroughputCard />
      <TopologyCard />
      <TopicsCard admin={admin} />
      <ConsumerGroupsCard />
    </main>
  );
}

/**
 * Whether the getting started guide shows, as the viewer chose in this
 * browser or else by how far the customer is; known once that is loaded,
 * so that the guide does not flash on a cluster that is past it.
 */
function useGuide(customerID: string) {
  const [choice, setChoice] = useState<GuideChoice>(() => loadGuideChoice(customerID));
  const sample = useSample();
  const topics = useTopics();
  const groups = useConsumerGroups();
  const known = sample.data !== undefined && topics.data !== undefined && groups.data !== undefined;
  const progress = guideProgress(sample.data, topics.data ?? [], groups.data ?? []);
  return {
    known,
    visible: known && guideVisible(choice, progress),
    choose: (next: GuideChoice) => {
      saveGuideChoice(customerID, next);
      setChoice(next);
    },
  };
}
