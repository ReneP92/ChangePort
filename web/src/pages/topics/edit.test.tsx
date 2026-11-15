import { afterEach, describe, expect, mock, test } from "bun:test";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { ApiError, type ConsumerGroupLag, type Topic, type TopicPolicy } from "@/api/client";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { EditTopicForm } from "@/pages/topics/EditTopicDialog";

afterEach(cleanup);

function inDialog(children: ReactNode) {
  return (
    <Dialog open>
      <DialogContent>{children}</DialogContent>
    </Dialog>
  );
}

const hour = 60 * 60 * 1000;
const week = 7 * 24 * hour;

const policy: TopicPolicy = {
  maxNameLength: 200,
  reservedPrefixes: ["_"],
  partitions: { min: 1, max: 12, default: 3 },
  replicationFactor: 1,
  defaultRetentionMs: week,
  retentionPresets: [
    { label: "1 hour", ms: hour },
    { label: "1 day", ms: 24 * hour },
    { label: "7 days", ms: week },
  ],
  cleanupPolicies: [
    { name: "delete", tiered: true },
    { name: "compact", tiered: false },
  ],
};

const orders: Topic = {
  name: "orders",
  partitions: 3,
  replicationFactor: 1,
  tiered: true,
  cleanupPolicy: "delete",
  retentionMs: week,
};

const billing: ConsumerGroupLag = {
  group: "billing",
  topic: "orders",
  lagSeconds: 3 * 3600,
  lagMessages: 120,
  capped: false,
  history: [],
};

function form(
  topic: Topic,
  more: { error?: Error; readers?: ConsumerGroupLag[] | undefined; onEdit?: () => void } = {},
) {
  const onSave = mock((change: unknown) => change);
  render(
    inDialog(
      <EditTopicForm
        topic={topic}
        policy={policy}
        readers={"readers" in more ? more.readers : [billing]}
        saving={false}
        error={more.error ?? null}
        onSave={onSave}
        {...(more.onEdit ? { onEdit: more.onEdit } : {})}
      />,
    ),
  );
  return onSave;
}

const warnings = () => screen.getByRole("list", { name: "What this change does" });

/**
 * Chooses a retention through the select's native counterpart, as autofill
 * does; NaN chooses forever.
 */
function chooseRetention(ms: number) {
  const native = document.querySelector<HTMLSelectElement>("select[aria-hidden]");
  if (!native) {
    throw new Error("the retention select has no native counterpart");
  }
  fireEvent.change(native, { target: { value: Number.isNaN(ms) ? "forever" : String(ms) } });
}

describe("EditTopicForm", () => {
  test("starts from the topic as it is, with nothing to save", () => {
    form(orders);
    expect(screen.getByLabelText("Partitions")).toHaveProperty("value", "3");
    expect(screen.getByText("It has 3, and may have up to 12.")).toBeDefined();
    expect(screen.queryByRole("list", { name: "What this change does" })).toBeNull();
    expect(screen.getByRole("button", { name: "Save changes" })).toHaveProperty("disabled", true);
  });

  test("adds partitions, saying keys move", () => {
    const onSave = form(orders);
    fireEvent.change(screen.getByLabelText("Partitions"), { target: { value: "6" } });
    const warnings = screen.getByRole("list", { name: "What this change does" });
    expect(
      within(warnings).getByText(/a key's new messages may go to another partition/),
    ).toBeDefined();
    expect(within(warnings).getByText("Partitions can be added, never removed.")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(onSave).toHaveBeenCalledWith({ partitions: 6 });
  });

  test("never removes partitions", () => {
    const onSave = form(orders);
    fireEvent.change(screen.getByLabelText("Partitions"), { target: { value: "2" } });
    expect(
      screen.getByText("Partitions can be added, never removed: keep at least 3."),
    ).toBeDefined();
    expect(screen.getByRole("button", { name: "Save changes" })).toHaveProperty("disabled", true);
    fireEvent.submit(screen.getByRole("button", { name: "Save changes" }));
    expect(onSave).not.toHaveBeenCalled();
  });

  test("a retention shorter than a group is behind says it loses messages", () => {
    const onSave = form(orders);
    chooseRetention(hour);
    const loss =
      "billing is 3 hours behind. Keeping messages for 1 hour, orders deletes messages billing has not read yet.";
    expect(within(warnings()).getByText(loss)).toBeDefined();
    // Screen readers hear it as it appears.
    expect(document.querySelector("[aria-live=polite]")?.textContent).toBe(loss);
    fireEvent.click(screen.getByRole("button", { name: "Save, losing unread messages" }));
    expect(onSave).toHaveBeenCalledWith({ retentionMs: hour });
  });

  test("while the groups' lag is unknown, a shorter retention may lose messages", () => {
    form(orders, { readers: undefined });
    chooseRetention(hour);
    expect(
      within(warnings()).getByText(/How far behind the consumer groups are is not known/),
    ).toBeDefined();
    expect(screen.getByRole("button", { name: "Save, losing unread messages" })).toBeDefined();
  });

  test("a topic kept forever can stay so", () => {
    const { retentionMs, ...rest } = orders;
    expect(retentionMs).toBe(week);
    const onSave = form(rest);
    expect(screen.getByLabelText("Retention").textContent).toBe("Forever");
    chooseRetention(hour);
    chooseRetention(Number.NaN);
    expect(screen.queryByRole("list", { name: "What this change does" })).toBeNull();
    fireEvent.submit(screen.getByRole("button", { name: "Save changes" }));
    expect(onSave).not.toHaveBeenCalled();
  });

  test("an edit makes the server's last problem stale", () => {
    const onEdit = mock(() => undefined);
    form(orders, { onEdit });
    fireEvent.change(screen.getByLabelText("Partitions"), { target: { value: "4" } });
    chooseRetention(hour);
    expect(onEdit).toHaveBeenCalledTimes(2);
  });

  test("a compacted topic has no retention to change", () => {
    form({ ...orders, cleanupPolicy: "compact", tiered: false });
    expect(screen.getByText(/it keeps the latest message per key however old/)).toBeDefined();
    expect(screen.queryByLabelText("Retention")).toBeNull();
  });

  test("a topic at the cluster's most partitions cannot get more", () => {
    form({ ...orders, partitions: 12 });
    expect(screen.getByLabelText("Partitions")).toHaveProperty("disabled", true);
    expect(screen.getByText("It has 12, the most a topic may have on this cluster.")).toBeDefined();
  });

  test("shows the server's problem by its field", () => {
    form(orders, {
      error: new ApiError(400, {
        status: 400,
        title: "The change breaks a guardrail",
        detail:
          "the cluster holds 990 of its 1000 partition replicas; 3 more partitions would exceed them",
        field: "partitions",
      }),
    });
    expect(screen.getByText(/3 more partitions would exceed them/)).toBeDefined();
  });
});
