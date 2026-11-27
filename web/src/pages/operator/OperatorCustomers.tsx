import { ChevronRight } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router";
import type { Customer, EnvironmentUsage } from "@/api/client";
import { useCustomerUsage, useCustomers, useMe } from "@/api/queries";
import { StatusBadge } from "@/components/CustomerSummary";
import {
  byOrganization,
  collapsedCustomers,
  collapsedKey,
  customerKey,
  foldSummary,
  rememberCollapsed,
} from "@/lib/customers";
import { formatBytes } from "@/lib/format";
import { parts } from "@/lib/parts";
import { formatDay, trialEnding, trialLeft } from "@/lib/trials";
import { Notice } from "@/components/Notice";
import { Page } from "@/components/Page";
import { ProvisionCustomerDialog } from "@/pages/operator/ProvisionCustomerDialog";

/** Shows children to operators, and everyone else what to do instead. */
export function OperatorsOnly({ children }: { children: ReactNode }) {
  const me = useMe();
  if (me.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (me.isError || me.data.access !== "operator") {
    return (
      <Notice title="Operators only">
        Only Changeport&apos;s operators can see every customer.
      </Notice>
    );
  }
  return children;
}

/** Every customer, for operators. */
export function OperatorCustomers() {
  return (
    <OperatorsOnly>
      <CustomerTable />
    </OperatorsOnly>
  );
}

function CustomerTable() {
  const customers = useCustomers();
  const usage = useCustomerUsage();
  const [collapsed, setCollapsed] = useState(collapsedCustomers);
  // Another tab folding a customer folds it here too.
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === collapsedKey || event.key === null) {
        setCollapsed(collapsedCustomers());
      }
    };
    window.addEventListener("storage", changed);
    return () => {
      window.removeEventListener("storage", changed);
    };
  }, []);
  // A customer down to one environment forgets it was folded, so that one
  // given more later shows them; so does one that is gone. The list changing
  // prunes them as it renders, as React has state follow data.
  const groups = customers.data ? byOrganization(customers.data) : undefined;
  const folding = groups
    ?.filter((g) => g.length > 1)
    .map((g) => (g[0] ? customerKey(g[0]) : ""))
    .join("\n");
  const [prunedFor, setPrunedFor] = useState<string>();
  if (folding !== undefined && folding !== prunedFor) {
    setPrunedFor(folding);
    const live = new Set(folding.split("\n"));
    const kept = new Set([...collapsed].filter((key) => live.has(key)));
    if (kept.size < collapsed.size) {
      setCollapsed(kept);
    }
  }
  useEffect(() => {
    rememberCollapsed(collapsed);
  }, [collapsed]);
  if (customers.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (customers.isError) {
    return <Notice title="The console is unavailable">{customers.error.message}</Notice>;
  }
  const usageOf = (customer: Customer) => usage.data?.find((u) => u.customerID === customer.id);
  return (
    <Page title="Customers" actions={<ProvisionCustomerDialog />}>
      {customers.data.length === 0 ? (
        <p className="text-muted-foreground">There are no customers yet.</p>
      ) : (
        // Wide tables scroll sideways on phones instead of overflowing; relative
        // keeps what is positioned absolutely, such as screen-reader-only
        // headers, inside the scroll area too.
        <div className="relative -mx-1 overflow-x-auto px-1">
          <table className="w-full text-left text-sm [&_td]:px-3 [&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th]:px-3 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0">
            <thead className="border-b text-muted-foreground">
              <tr>
                <th className="py-2 font-medium">Customer</th>
                <th className="py-2 font-medium">Profile</th>
                <th className="py-2 font-medium">Trial</th>
                <th className="py-2 text-right font-medium">Last 30 days</th>
                <th className="py-2 font-medium">Progress</th>
                <th className="py-2 font-medium">Status</th>
              </tr>
            </thead>
            {(groups ?? []).map((environments) => {
              const [first] = environments;
              if (first === undefined) {
                return null;
              }
              // A customer with one environment, as most are, takes one row.
              if (environments.length === 1) {
                return (
                  <tbody key={first.id} className="border-b last:border-0">
                    <EnvironmentRow
                      customer={first}
                      usage={usageOf(first)}
                      usagePending={usage.isPending}
                    />
                  </tbody>
                );
              }
              // One with more heads its environments, which follow it indented.
              const key = customerKey(first);
              return (
                <CustomerGroup
                  key={key}
                  environments={[first, ...environments.slice(1)]}
                  usages={environments.map(usageOf)}
                  usagePending={usage.isPending}
                  collapsed={collapsed.has(key)}
                  onToggle={() => {
                    const next = new Set(collapsed);
                    if (!next.delete(key)) {
                      next.add(key);
                    }
                    setCollapsed(next);
                  }}
                />
              );
            })}
          </table>
        </div>
      )}
    </Page>
  );
}

/**
 * A customer with several environments: its name, how many and what they
 * processed together, and under it its environments, which the operator can
 * fold away, the header then saying how many are ready.
 */
function CustomerGroup({
  environments,
  usages,
  usagePending,
  collapsed,
  onToggle,
}: {
  environments: [Customer, ...Customer[]];
  usages: (EnvironmentUsage | undefined)[];
  usagePending: boolean;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const [first] = environments;
  const name = `customer-${first.id}`;
  const summary = foldSummary(environments);
  // Folded, the header is the group's only row, padded as one.
  const padding = collapsed ? "py-3" : "pt-3";
  return (
    <tbody className="border-b last:border-0">
      <tr>
        <th scope="rowgroup" className={`${padding} font-normal whitespace-nowrap`}>
          <button
            type="button"
            aria-expanded={!collapsed}
            aria-controls={environments.map((c) => `environment-${c.id}`).join(" ")}
            onClick={onToggle}
            className="group -my-1 -ml-1 flex items-start gap-1.5 rounded-md py-1 pr-2 pl-1 text-left outline-none hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <ChevronRight
              aria-hidden
              className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
            />
            <span>
              <span id={name} className="font-medium">
                {first.displayName}
              </span>
              <span className="block text-muted-foreground">
                {environments.length} environments
              </span>
            </span>
          </button>
        </th>
        <td />
        <td />
        <td className={`${padding} text-right whitespace-nowrap tabular-nums`}>
          {usagePending ? (
            <span className="text-muted-foreground">Loading</span>
          ) : (
            <ProcessedCell usage={total(usages)} />
          )}
        </td>
        <td />
        <td className={padding}>
          {collapsed && (
            <span className={summary.attention ? "font-medium" : "text-muted-foreground"}>
              {summary.text}
            </span>
          )}
        </td>
      </tr>
      {environments.map((customer, i) => (
        <EnvironmentRow
          key={customer.id}
          customer={customer}
          usage={usages[i]}
          usagePending={usagePending}
          nestedUnder={name}
          hidden={collapsed}
        />
      ))}
    </tbody>
  );
}

/**
 * A customer's environment, each a Customer and a cluster of its own. On its
 * own it is named for its customer; nested under its customer, for itself.
 */
function EnvironmentRow({
  customer,
  usage,
  usagePending,
  nestedUnder,
  hidden = false,
}: {
  customer: Customer;
  usage: EnvironmentUsage | undefined;
  usagePending: boolean;
  /** The ID of the customer's name the environment is listed under, if any. */
  nestedUnder?: string;
  /** Whether its customer's environments are folded away. */
  hidden?: boolean;
}) {
  const nested = nestedUnder !== undefined;
  const protection = customer.deletionProtection && " · protected";
  return (
    // Folded, it collapses rather than hides, so that the columns keep
    // their widths and nothing jumps.
    <tr id={`environment-${customer.id}`} className={hidden ? "collapse" : undefined}>
      {/* Nested, a rule the row's full height joins it to its siblings. */}
      <td
        className={
          nested
            ? "py-3 whitespace-nowrap shadow-[inset_1px_0_0_var(--border)]"
            : "py-3 whitespace-nowrap"
        }
      >
        {/* Its name lines up with its customer's, past the chevron. */}
        <div className={nested ? "pl-5.5" : undefined}>
          <Link
            to={`/operator/customers/${customer.id}`}
            className="font-medium underline-offset-4 hover:underline"
            // Out of the table, as in a list of links, it says whose it is.
            aria-describedby={nestedUnder}
          >
            {nested ? customer.environment : customer.displayName}
          </Link>
          <div className="text-muted-foreground">
            {nested ? customer.id : `${customer.id} · ${customer.environment}`}
            {protection}
          </div>
        </div>
      </td>
      <td className="py-3 whitespace-nowrap">{customer.profile}</td>
      <td className="py-3 whitespace-nowrap">
        <TrialCell customer={customer} />
      </td>
      <td className="py-3 text-right whitespace-nowrap tabular-nums">
        {usagePending ? (
          <span className="text-muted-foreground">Loading</span>
        ) : (
          <ProcessedCell usage={usage} />
        )}
      </td>
      <td className="py-3">
        <ConditionDots customer={customer} />
      </td>
      <td className="py-3">
        {/* Long messages wrap below the badge, never to a sliver. */}
        <div className="flex min-w-60 flex-col items-start gap-1">
          <StatusBadge customer={customer} />
          {!customer.ready && (
            <span className="text-muted-foreground">{customer.status.message}</span>
          )}
        </div>
      </td>
    </tr>
  );
}

/** What environments processed together, if every one's is known. */
function total(
  usages: (EnvironmentUsage | undefined)[],
): Pick<EnvironmentUsage, "bytesIn" | "bytesOut"> {
  let bytesIn = 0;
  let bytesOut = 0;
  for (const usage of usages) {
    if (usage?.bytesIn === undefined || usage.bytesOut === undefined) {
      return {};
    }
    bytesIn += usage.bytesIn;
    bytesOut += usage.bytesOut;
  }
  return { bytesIn, bytesOut };
}

/** The bytes a customer's cluster processed over the last 30 days, in and out. */
function ProcessedCell({
  usage,
}: {
  usage: Pick<EnvironmentUsage, "bytesIn" | "bytesOut"> | undefined;
}) {
  const { bytesIn, bytesOut } = usage ?? {};
  if (bytesIn === undefined || bytesOut === undefined) {
    return <span className="text-muted-foreground">Not known</span>;
  }
  return (
    <span>
      {formatBytes(bytesIn + bytesOut)}
      <span className="block text-muted-foreground">
        {formatBytes(bytesIn)} in, {formatBytes(bytesOut)} out
      </span>
    </span>
  );
}

/** When a customer's trial ends, in its last three days as a warning. */
function TrialCell({ customer }: { customer: Customer }) {
  if (!customer.trial) {
    return <span className="text-muted-foreground">No trial</span>;
  }
  const { endsAt } = customer.trial;
  return (
    <span className={trialEnding(endsAt) ? "font-medium text-destructive" : undefined}>
      {trialLeft(endsAt)} left
      <span className="block text-muted-foreground">until {formatDay(new Date(endsAt))}</span>
    </span>
  );
}

/** A dot per part of the customer's cluster, filled once it is ready. */
export function ConditionDots({ customer }: { customer: Customer }) {
  const ready = parts.filter(
    (part) => customer.conditions.find((condition) => condition.type === part)?.ready,
  );
  return (
    <span
      role="img"
      aria-label={`${String(ready.length)} of ${String(parts.length)} parts ready`}
      className="flex gap-1"
    >
      {parts.map((part) => (
        <span
          key={part}
          className={
            ready.includes(part)
              ? "size-2 rounded-full bg-primary"
              : "size-2 rounded-full border border-muted-foreground/40"
          }
        />
      ))}
    </span>
  );
}
