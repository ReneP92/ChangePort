import { useState } from "react";
import { Link } from "react-router";
import type { Customer } from "@/api/client";
import { useConnection, useTopics } from "@/api/queries";
import { DocsLink } from "@/components/DocsLink";
import { Page } from "@/components/Page";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import connectingExamples from "@/lib/connecting-examples.json";
import { onboardingExamples } from "@/lib/onboarding";
import { useEnvironmentCrumb, useEnvironmentPath } from "@/lib/useEnvironment";
import { CustomerArea } from "@/pages/CustomerArea";
import { Examples } from "@/pages/connect/Examples";
import { ConnectionCard } from "@/pages/overview/ConnectionCard";

// Every language of the connection guide, those most teams new to Kafka
// start in first, as on the welcome page.
const starters = onboardingExamples(connectingExamples);
const examples = [
  ...starters,
  ...connectingExamples.filter((example) => !starters.includes(example)),
];

/** What the examples write to while the cluster has no topic of its own. */
const placeholderTopic = "my-topic";

/**
 * How the environment's apps connect to its cluster, those that write and
 * those that read alike: the address and credentials, client.properties for
 * admins, and the connection guide's examples (docs/console.md).
 */
export function ConnectPage() {
  return (
    <CustomerArea>
      {(customer, admin) => <ConnectView customer={customer} admin={admin} />}
    </CustomerArea>
  );
}

function ConnectView({ customer, admin }: { customer: Customer; admin: boolean }) {
  const crumbs = useEnvironmentCrumb();
  return (
    <Page
      crumbs={crumbs}
      title="Connect"
      description="How your apps connect to this cluster. Apps that write to topics and apps that read them use the same address and credentials."
    >
      <ConnectionCard admin={admin} />
      <ExamplesCard customer={customer} admin={admin} />
    </Page>
  );
}

/**
 * The connection guide's examples, in every language it has, for a topic
 * the customer picks: each sends a message to it and reads it back as a
 * consumer group. Topics a Postgres source writes are left out, as a test
 * message there would land among the database's changes.
 */
function ExamplesCard({ customer, admin }: { customer: Customer; admin: boolean }) {
  const envPath = useEnvironmentPath();
  const connection = useConnection();
  const topics = useTopics();
  const own = (topics.data ?? []).filter((topic) => topic.source === undefined);
  const [picked, setPicked] = useState<string>();
  const topic = own.find((t) => t.name === picked)?.name ?? own[0]?.name;
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Examples</h2>
        </CardTitle>
        <CardDescription>
          Set these variables in your app&apos;s folder, next to client.properties
          {admin ? "" : ", which an admin of your organization can give you"}, and pick your
          language. Each example sends a message to the topic and reads it back as a consumer group.{" "}
          <DocsLink page="connecting.html" className="text-foreground underline">
            Read the connection guide
          </DocsLink>
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        {topics.data && own.length === 0 && (
          <p className="text-muted-foreground">
            The examples write to {placeholderTopic} until this cluster has a topic of its own.{" "}
            <Link to={envPath("/")} className="text-foreground underline underline-offset-4">
              Create one on the overview
            </Link>
            .
          </p>
        )}
        {own.length > 0 && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <Label htmlFor="example-topic">Topic</Label>
            <Select value={topic} onValueChange={setPicked}>
              <SelectTrigger id="example-topic" className="w-64 max-w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {own.map((t) => (
                  <SelectItem key={t.name} value={t.name}>
                    {t.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        <Examples
          examples={examples}
          bootstrap={connection.data?.bootstrapAddress ?? customer.bootstrapAddress ?? ""}
          username={connection.data?.username ?? "admin"}
          topic={topic ?? placeholderTopic}
        />
      </CardContent>
    </Card>
  );
}
