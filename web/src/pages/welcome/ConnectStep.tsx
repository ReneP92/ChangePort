import { useState } from "react";
import type { Customer } from "@/api/client";
import { useConnection, useDownloadClientProperties } from "@/api/queries";
import { Button } from "@/components/ui/button";
import connectingExamples from "@/lib/connecting-examples.json";
import { onboardingExamples } from "@/lib/onboarding";
import { Examples } from "@/pages/connect/Examples";
import { StepTitle } from "@/pages/welcome/StepTitle";

// Python, Go and JavaScript, the languages most teams new to Kafka start in.
const examples = onboardingExamples(connectingExamples);

/**
 * How the customer's apps connect: client.properties, the variables the
 * examples read, and the examples of the connection guide in their language.
 */
export function ConnectStep({
  customer,
  admin,
  topic,
}: {
  customer: Customer;
  admin: boolean;
  topic: string;
}) {
  const connection = useConnection();
  const download = useDownloadClientProperties();
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const bootstrap = connection.data?.bootstrapAddress ?? customer.bootstrapAddress ?? "";
  const username = connection.data?.username ?? "admin";
  return (
    <>
      <StepTitle>Connect your app</StepTitle>
      <p className="text-muted-foreground">
        Your apps connect over TLS with a username and password, which{" "}
        <code>client.properties</code> holds.{" "}
        {admin
          ? "Download it into your app's folder, set these variables there, and pick your language."
          : "Ask an admin of your organization for it, save it in your app's folder as client.properties, set these variables there, and pick your language."}
      </p>
      {admin && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Button
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
      <Examples examples={examples} bootstrap={bootstrap} username={username} topic={topic} />
      <p className="text-muted-foreground">
        Each example sends a message to {topic} and reads it back as a consumer group. Run it in the
        next step, and watch your message arrive.
      </p>
    </>
  );
}
