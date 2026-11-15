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

function form(topic: Topic, more: { error?: Error; readers?: ConsumerGroupLag[] } = {}) {
  const onSave = mock((change: unknown) => change);
  render(
    inDialog(
      <EditTopicForm
        topic={topic}
        policy={policy}
        readers={more.readers ?? [billing]}
        saving={false}
        error={more.error ?? null}
        onSave={onSave}
      />,
    ),
  );
  return onSave;
}

/** Chooses a retention through the select's native counterpart, as autofill does. */
function chooseRetention(ms: number) {
  const native = document.querySelector<HTMLSelectElement>("select[aria-hidden]");
  if (!native) {
    throw new Error("the retention select has no native counterpart");
  }
  fireEvent.change(native, { target: { value: String(ms) } });
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
    expect(
      screen.getByText(
        "billing is 3 hours behind. Keeping messages for 1 hour, orders deletes messages billing has not read yet.",
      ),
    ).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Save, losing unread messages" }));
    expect(onSave).toHaveBeenCalledWith({ retentionMs: hour });
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
