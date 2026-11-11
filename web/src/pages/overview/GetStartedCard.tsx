import { Check, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { errorText, type Customer, type SampleApp } from "@/api/client";
import {
  useConnection,
  useConsumerGroups,
  useDownloadClientProperties,
  useSample,
  useStartSample,
  useStopSample,
  useTopics,
} from "@/api/queries";
import { CodeBlock } from "@/components/CodeBlock";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import connectingExamples from "@/lib/connecting-examples.json";
import type { Example } from "@/lib/examples";
import { formatTime } from "@/lib/format";
import { exampleEnvironment, guideProgress, type GuideProgress } from "@/lib/getStarted";
import { cn } from "@/lib/utils";
import { CreateTopicDialog } from "@/pages/overview/CreateTopicDialog";

const examples: Example[] = connectingExamples;

/**
 * The guided first ten minutes: a sample app that shows Kafka at work, a
 * topic of the customer's own, and their own app connected to it.
 */
export function GetStartedCard({
  customer,
  admin,
  onHide,
}: {
  customer: Customer;
  admin: boolean;
  onHide: () => void;
}) {
  const sample = useSample();
  const topics = useTopics();
  const groups = useConsumerGroups();
  const progress = guideProgress(sample.data, topics.data ?? [], groups.data ?? []);
  return (
    <Card role="region" aria-labelledby="get-started">
      <CardHeader>
        <CardTitle>
          <h2 id="get-started">Get started</h2>
        </CardTitle>
        <CardDescription>
          From an empty cluster to your own app streaming data, in about ten minutes.
        </CardDescription>
        <CardAction>
          <Button variant="ghost" size="sm" onClick={onHide}>
            <X />
            Hide
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        <ol className="flex flex-col">
          <Step number={1} done={progress.sample} title="Watch Kafka at work">
            <SampleStep sample={sample.data} admin={admin} />
          </Step>
          <Step number={2} done={progress.topic !== undefined} title="Create a topic for your data">
            <TopicStep progress={progress} admin={admin} existing={topics.data ?? []} />
          </Step>
          <Step number={3} done={progress.app !== undefined} title="Connect your app" last>
            <ConnectStep customer={customer} progress={progress} admin={admin} />
          </Step>
        </ol>
      </CardContent>
    </Card>
  );
}

/** A numbered step, which turns into a check once done. */
function Step({
  number,
  title,
  done,
  last = false,
  children,
}: {
  number: number;
  title: string;
  done: boolean;
  last?: boolean;
  children: ReactNode;
}) {
  return (
    <li className="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-x-3">
      <div className="flex flex-col items-center">
        <span
          className={cn(
            "flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-medium tabular-nums",
            done ? "border-primary bg-primary text-primary-foreground" : "text-muted-foreground",
          )}
        >
          {done ? <Check className="size-4" aria-label="Done" /> : number}
        </span>
        {!last && <span className="my-1 w-px flex-1 bg-border" />}
      </div>
      <div className={cn("flex min-w-0 flex-col gap-3 text-sm", !last && "pb-6")}>
        <h3 className="flex min-h-7 items-center font-medium">{title}</h3>
        {children}
      </div>
    </li>
  );
}

function SampleStep({ sample, admin }: { sample: SampleApp | undefined; admin: boolean }) {
  const start = useStartSample();
  const stop = useStopSample();
  const error = start.error ?? stop.error;
  const busy = start.isPending || stop.isPending;
  if (!sample) {
    return null;
  }
  return (
    <>
      <p className="text-muted-foreground">
        A sample shop runs in your cluster for an hour. Its checkout writes an order event to the
        topic <code className="text-foreground">{sample.topic}</code> a few times a second, and its
        email service reads each one as the consumer group{" "}
        <code className="text-foreground">{sample.group}</code>, as two of your apps would.
      </p>
      {sample.state === "stopped" &&
        (admin ? (
          <div>
            <Button
              size="sm"
              disabled={busy}
              onClick={() => {
                start.mutate();
              }}
            >
              Start the sample
            </Button>
          </div>
        ) : (
          <p className="text-muted-foreground">Ask an admin of your organization to start it.</p>
        ))}
      {sample.state === "starting" && (
        <p role="status" className="text-muted-foreground motion-safe:animate-pulse">
          Starting the sample, which takes a minute or two
        </p>
      )}
      {sample.state === "running" && (
        <>
          <p role="status">
            The sample runs until {sample.stopsAt ? formatTime(sample.stopsAt) : "it is stopped"}.
            Watch its orders flow on the{" "}
            <a href="#data-flow" className="underline underline-offset-4">
              data flow map
            </a>
            , read{" "}
            <Link
              to={`/topics/${encodeURIComponent(sample.topic)}`}
              className="underline underline-offset-4"
            >
              its messages
            </Link>
            , and see how far behind{" "}
            <Link
              to={`/consumer-groups/${encodeURIComponent(sample.group)}`}
              className="underline underline-offset-4"
            >
              the email service
            </Link>{" "}
            is.
          </p>
        </>
      )}
      {sample.state !== "stopped" && admin && (
        <div className="flex flex-wrap gap-2">
          {sample.state === "running" && (
            <Button
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={() => {
                start.mutate();
              }}
            >
              Keep it running
            </Button>
          )}
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => {
              stop.mutate();
            }}
          >
            Stop the sample
          </Button>
        </div>
      )}
      {error && (
        <p role="alert" className="text-destructive">
          {errorText(error)}
        </p>
      )}
    </>
  );
}

function TopicStep({
  progress,
  admin,
  existing,
}: {
  progress: GuideProgress;
  admin: boolean;
  existing: { name: string }[];
}) {
  if (progress.topic !== undefined) {
    return (
      <p className="text-muted-foreground">
        Your topic{" "}
        <Link
          to={`/topics/${encodeURIComponent(progress.topic)}`}
          className="font-medium text-foreground underline-offset-4 hover:underline"
        >
          {progress.topic}
        </Link>{" "}
        is ready for your app&apos;s data.
      </p>
    );
  }
  return (
    <>
      <p className="text-muted-foreground">
        A topic holds one kind of event, such as orders or page views, in the order they happened.
        Name it after what it holds.
      </p>
      {admin ? (
        <div>
          <CreateTopicDialog existing={existing.map((topic) => topic.name)} />
        </div>
      ) : (
        <p className="text-muted-foreground">Ask an admin of your organization to create one.</p>
      )}
    </>
  );
}

function ConnectStep({
  customer,
  progress,
  admin,
}: {
  customer: Customer;
  progress: GuideProgress;
  admin: boolean;
}) {
  const connection = useConnection();
  const download = useDownloadClientProperties();
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const bootstrap = connection.data?.bootstrapAddress ?? customer.bootstrapAddress ?? "";
  const username = connection.data?.username ?? "admin";
  return (
    <>
      <p className="text-muted-foreground">
        Your apps connect over TLS with a username and password, which{" "}
        <code className="text-foreground">client.properties</code> holds.{" "}
        {admin
          ? "Download it into your app's folder, set these variables there, and run an example in your language."
          : "Ask an admin of your organization for it, save it in your app's folder as client.properties, set these variables there, and run an example in your language."}
      </p>
      {admin && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setDownloadError(null);
              // The examples read it by this name.
              download("client.properties").catch((error: unknown) => {
                setDownloadError(error instanceof Error ? error.message : String(error));
              });
            }}
          >
            Download client.properties
          </Button>
          <span className="text-muted-foreground">It holds the password; keep it secret.</span>
        </div>
      )}
      {downloadError && (
        <p role="alert" className="text-destructive">
          {downloadError}
        </p>
      )}
      <CodeBlock
        label="Shell"
        code={exampleEnvironment(bootstrap, username, progress.topic ?? "orders")}
      />
      <Tabs defaultValue={examples[0]?.language} className="gap-3">
        {/* The tabs scroll sideways on phones rather than the page. */}
        <div className="-mx-1 overflow-x-auto px-1 pb-1">
          <TabsList>
            {examples.map((example) => (
              <TabsTrigger key={example.language} value={example.language} className="px-2.5">
                {example.language}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        {examples.map((example) => (
          <TabsContent
            key={example.language}
            value={example.language}
            className="flex flex-col gap-3"
          >
            {example.install && <CodeBlock label="Install the client" code={example.install} />}
            <CodeBlock label={example.language} code={example.code} />
          </TabsContent>
        ))}
      </Tabs>
      <p
        role="status"
        className={cn("flex items-center gap-2", !progress.app && "text-muted-foreground")}
      >
        {progress.app ? (
          <>
            <Check className="size-4 text-primary" aria-hidden />
            {progress.app}.
          </>
        ) : (
          <>
            <span
              className="size-2 rounded-full bg-muted-foreground motion-safe:animate-pulse"
              aria-hidden
            />
            {progress.topic
              ? `Waiting for your app's data on ${progress.topic}`
              : "Waiting for your app's data"}
          </>
        )}
      </p>
    </>
  );
}
