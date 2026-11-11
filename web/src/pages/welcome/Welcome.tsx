import { Check } from "lucide-react";
import { useState } from "react";
import { Navigate, useNavigate } from "react-router";
import type { Customer } from "@/api/client";
import { useCustomer, useMe, useTopics } from "@/api/queries";
import { ClusterProgress } from "@/components/CustomerSummary";
import { Notice } from "@/components/Notice";
import { TrialNotice } from "@/components/TrialNotice";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { finishOnboarding } from "@/lib/onboarding";
import { cn } from "@/lib/utils";
import { ConnectStep } from "@/pages/welcome/ConnectStep";
import { FirstMessageStep } from "@/pages/welcome/FirstMessageStep";
import { StepTitle } from "@/pages/welcome/StepTitle";
import { TopicStep } from "@/pages/welcome/TopicStep";

/**
 * The welcome page: a new customer's way from their cluster to their own
 * first message, one step at a time. Signed-in customers land here until
 * their cluster has a topic (lib/onboarding), and can skip it any time.
 */
export function Welcome() {
  const me = useMe();
  const customer = useCustomer();
  if (me.isPending || (me.data?.access === "customer" && customer.isPending)) {
    return <Notice title="Loading" busy />;
  }
  if (me.isError) {
    return <Notice title="The console is unavailable">{me.error.message}</Notice>;
  }
  if (me.data.access !== "customer" || !customer.data) {
    return <Navigate to="/" replace />;
  }
  return (
    <>
      {/* Every page of a customer on a trial says when it ends. */}
      {customer.data.trial && <TrialNotice trial={customer.data.trial} />}
      <Steps customer={customer.data} admin={me.data.admin ?? false} />
    </>
  );
}

const titles = ["Your cluster", "A topic", "Connect", "First message"];

function Steps({ customer, admin }: { customer: Customer; admin: boolean }) {
  const navigate = useNavigate();
  const topics = useTopics(customer.ready);
  const [step, setStep] = useState(0);
  const [topic, setTopic] = useState<string>();
  const [arrived, setArrived] = useState(false);
  const finish = () => {
    finishOnboarding(customer.id);
    void navigate("/");
  };
  const done = [customer.ready, topic !== undefined, step > 2, arrived];
  const canGoOn = [customer.ready, topic !== undefined, true, true][step] ?? false;
  return (
    <main className="mx-auto flex max-w-3xl flex-col gap-6 p-6">
      <header className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Welcome to Changeport</h1>
          <p className="text-sm text-muted-foreground">
            From {customer.displayName}&apos;s new Kafka cluster to your first message, in about ten
            minutes.
          </p>
        </div>
        <Button variant="ghost" size="sm" className="shrink-0" onClick={finish}>
          Skip to the overview
        </Button>
      </header>
      <ol aria-label="Steps" className="grid grid-cols-4 gap-2">
        {titles.map((title, i) => (
          <li
            key={title}
            aria-current={i === step ? "step" : undefined}
            className="flex flex-col items-center gap-1.5 text-center text-xs sm:flex-row sm:text-left sm:text-sm"
          >
            <span
              className={cn(
                "flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-medium tabular-nums",
                done[i] && "border-primary bg-primary text-primary-foreground",
                !done[i] && i === step && "border-primary text-foreground",
                !done[i] && i !== step && "text-muted-foreground",
              )}
            >
              {done[i] ? <Check className="size-4" aria-label="Done" /> : i + 1}
            </span>
            <span className={cn(i === step ? "font-medium" : "text-muted-foreground")}>
              {title}
            </span>
          </li>
        ))}
      </ol>
      <Card>
        <CardContent className="flex flex-col gap-5 text-sm">
          {step === 0 && <ClusterStep customer={customer} />}
          {step === 1 && (
            <TopicStep
              admin={admin}
              topics={topics.data ?? []}
              chosen={topic}
              onChoose={setTopic}
            />
          )}
          {step === 2 && topic !== undefined && (
            <ConnectStep customer={customer} admin={admin} topic={topic} />
          )}
          {step === 3 && topic !== undefined && (
            <FirstMessageStep admin={admin} topic={topic} onArrived={setArrived} />
          )}
          <footer className="flex flex-wrap items-center justify-between gap-3 border-t pt-5">
            {step > 0 ? (
              <Button
                variant="outline"
                onClick={() => {
                  setStep(step - 1);
                }}
              >
                Back
              </Button>
            ) : (
              <span />
            )}
            {step < titles.length - 1 ? (
              <Button
                disabled={!canGoOn}
                onClick={() => {
                  setStep(step + 1);
                }}
              >
                {nextLabel(step, customer, topic)}
              </Button>
            ) : (
              <Button variant={arrived ? "default" : "outline"} onClick={finish}>
                Go to your cluster
              </Button>
            )}
          </footer>
        </CardContent>
      </Card>
    </main>
  );
}

function nextLabel(step: number, customer: Customer, topic: string | undefined): string {
  if (step === 0 && !customer.ready) {
    return "Next once your cluster is ready";
  }
  if (step === 1 && topic === undefined) {
    return "Next once you have a topic";
  }
  return "Next";
}

/** What the customer has, in plain words, and their cluster getting ready. */
function ClusterStep({ customer }: { customer: Customer }) {
  return (
    <>
      <StepTitle>Your Kafka cluster</StepTitle>
      <p className="text-muted-foreground">
        Changeport runs a Kafka cluster for {customer.displayName}; you bring the apps. Kafka keeps
        events, such as orders placed or pages viewed, in the order they happened:
      </p>
      <ul className="flex flex-col gap-2 pl-5 text-muted-foreground marker:text-muted-foreground [&>li]:list-disc">
        <li>
          A <span className="font-medium text-foreground">topic</span> holds one kind of event, such
          as orders.
        </li>
        <li>
          <span className="font-medium text-foreground">Producers</span> are your apps that write
          events to topics.
        </li>
        <li>
          <span className="font-medium text-foreground">Consumer groups</span> are your apps that
          read them, each at its own pace, and Kafka remembers how far each got.
        </li>
      </ul>
      {customer.ready ? (
        <p role="status" className="flex items-center gap-2">
          <Check className="size-4 text-primary" aria-hidden />
          Your cluster is ready.
        </p>
      ) : (
        <ClusterProgress customer={customer} />
      )}
    </>
  );
}
