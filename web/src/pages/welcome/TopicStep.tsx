import { Check } from "lucide-react";
import type { Topic } from "@/api/client";
import { useTopicPolicy } from "@/api/queries";
import { Button } from "@/components/ui/button";
import { formatCopies } from "@/lib/format";
import { CreateTopicForm } from "@/pages/overview/CreateTopicDialog";
import { StepTitle } from "@/pages/welcome/StepTitle";

/** How many of the customer's topics the step offers to use. */
const offered = 6;

/**
 * A topic for the customer's first message: one they create within the
 * guardrails, or one they have.
 */
export function TopicStep({
  admin,
  topics,
  chosen,
  onChoose,
}: {
  admin: boolean;
  topics: Topic[];
  chosen: string | undefined;
  onChoose: (topic: string | undefined) => void;
}) {
  const policy = useTopicPolicy();
  if (chosen !== undefined) {
    return (
      <>
        <StepTitle>Create a topic</StepTitle>
        <p role="status" className="flex flex-wrap items-center gap-2">
          <Check className="size-4 text-primary" aria-hidden />
          <span>
            Your apps send their messages to <code>{chosen}</code>.
          </span>
          <Button
            variant="link"
            size="sm"
            className="h-auto p-0"
            onClick={() => {
              onChoose(undefined);
            }}
          >
            Choose another
          </Button>
        </p>
      </>
    );
  }
  return (
    <>
      <StepTitle>Create a topic</StepTitle>
      <p className="text-muted-foreground">
        A topic holds one kind of event, in the order they happened. Name it after what it holds,
        such as orders or page-views.
      </p>
      {admin && policy.data && (
        <CreateTopicForm
          policy={policy.data}
          existing={topics.map((topic) => topic.name)}
          onCreated={onChoose}
          header={<p className="text-muted-foreground">{formatCopies(policy.data)}</p>}
          footer={(creating) => (
            <div>
              <Button type="submit" disabled={creating}>
                {creating ? "Creating" : "Create topic"}
              </Button>
            </div>
          )}
        />
      )}
      {admin && policy.isError && <p className="text-destructive">{policy.error.message}</p>}
      {!admin && topics.length === 0 && (
        <p className="text-muted-foreground">Ask an admin of your organization to create one.</p>
      )}
      {topics.length > 0 && (
        <div className="flex flex-col gap-2">
          <p className="text-muted-foreground">
            {admin ? "Or use one you have:" : "Use one your organization has:"}
          </p>
          <div className="flex flex-wrap gap-2">
            {topics.slice(0, offered).map((topic) => (
              <Button
                key={topic.name}
                variant="outline"
                size="sm"
                onClick={() => {
                  onChoose(topic.name);
                }}
              >
                {topic.name}
              </Button>
            ))}
          </div>
        </div>
      )}
    </>
  );
}
