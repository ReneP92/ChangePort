import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { ApiError, type Customer } from "@/api/client";
import { useCustomer, useLiveAlerts, useMe } from "@/api/queries";
import { CustomerSummary, StatusBadge } from "@/components/CustomerSummary";
import { customerLine } from "@/lib/customers";
import { Page } from "@/components/Page";
import { useEnvironmentCrumb } from "@/lib/useEnvironment";
import { TrialNotice } from "@/components/TrialNotice";
import { Notice } from "@/components/Notice";

/**
 * Shows a page of the active organization's ready cluster, with whether the
 * user is its admin; until the cluster is ready, how far it is. Everyone
 * else goes home, which says what they can do. Its alerts stay live
 * meanwhile.
 */
export function CustomerArea({
  children,
}: {
  children: (customer: Customer, admin: boolean) => ReactNode;
}) {
  const me = useMe();
  if (me.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (me.isError) {
    return <Notice title="The console is unavailable">{me.error.message}</Notice>;
  }
  if (me.data.access !== "customer") {
    return <Navigate to="/" replace />;
  }
  return <ReadyCustomer admin={me.data.admin ?? false}>{children}</ReadyCustomer>;
}

function ReadyCustomer({
  admin,
  children,
}: {
  admin: boolean;
  children: (customer: Customer, admin: boolean) => ReactNode;
}) {
  const customer = useCustomer();
  const crumbs = useEnvironmentCrumb();
  useLiveAlerts();
  if (customer.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (customer.isError) {
    const missing = customer.error instanceof ApiError && customer.error.status === 404;
    return (
      <Notice title={missing ? "No Kafka cluster yet" : "The console is unavailable"}>
        {missing ? "Your organization has no Changeport cluster yet." : customer.error.message}
      </Notice>
    );
  }
  // Every page of a customer on a trial says when it ends.
  const trial = customer.data.trial && <TrialNotice trial={customer.data.trial} />;
  if (!customer.data.ready) {
    return (
      <>
        {trial}
        <Page
          crumbs={crumbs}
          title="Overview"
          badge={<StatusBadge customer={customer.data} />}
          description={customerLine(customer.data)}
        >
          <CustomerSummary customer={customer.data} named={false} />
        </Page>
      </>
    );
  }
  return (
    <>
      {trial}
      {children(customer.data, admin)}
    </>
  );
}
