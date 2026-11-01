import { useState } from "react";
import {
  errorText,
  type AlertSettings,
  type AlertThresholds,
  type ReceiverKind,
} from "@/api/client";
import {
  useAlertSettings,
  useAlerts,
  useChangeAlertSettings,
  useRemoveAlertReceiver,
  useSetAlertReceiver,
} from "@/api/queries";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { formatDuration } from "@/lib/format";
import { CustomerArea } from "@/pages/CustomerArea";
import { AlertRows } from "@/pages/alerts/ActiveAlerts";

/**
 * The customer's alerts about their own applications: those that are
 * active, live; their thresholds; and where else they go. Admins change the
 * last two.
 */
export function AlertsPage() {
  return <CustomerArea>{(_, admin) => <Alerts admin={admin} />}</CustomerArea>;
}

function Alerts({ admin }: { admin: boolean }) {
  const settings = useAlertSettings();
  // Above the form, which saved settings renew, so that it can say it saved.
  const change = useChangeAlertSettings();
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Alerts</h1>
        <p className="text-sm text-muted-foreground">
          Changeport watches how your applications use your cluster: consumer groups that fall
          behind or stop, clients that are throttled, and requests that fail.
        </p>
      </header>
      <ActiveCard />
      {settings.isPending && <p className="text-sm text-muted-foreground">Loading settings</p>}
      {settings.isError && <p className="text-sm text-destructive">{errorText(settings.error)}</p>}
      {settings.data && (
        <>
          <ThresholdsCard
            // A new form for new settings, such as after another admin's change.
            key={JSON.stringify(settings.data.thresholds)}
            settings={settings.data}
            admin={admin}
            change={change}
          />
          <ReceiversCard settings={settings.data} admin={admin} />
        </>
      )}
    </main>
  );
}

function ActiveCard() {
  const alerts = useAlerts();
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Active</h2>
        </CardTitle>
        <CardDescription>Alerts show here as they fire, and go once they clear.</CardDescription>
      </CardHeader>
      <CardContent className="text-sm">
        {alerts.isPending && <p className="text-muted-foreground">Loading alerts</p>}
        {alerts.isError && <p className="text-destructive">{errorText(alerts.error)}</p>}
        {alerts.data?.length === 0 && <p className="text-muted-foreground">All is well.</p>}
        {alerts.data && alerts.data.length > 0 && <AlertRows alerts={alerts.data} />}
      </CardContent>
    </Card>
  );
}

/** One threshold of the form: how it reads, and how its value converts. */
interface Threshold {
  key: keyof AlertThresholds;
  title: string;
  description: string;
  unit: string;
  min: number;
  max: number;
  /** The value in the API's unit, and back, from the form's. */
  get: (t: AlertThresholds) => number;
  set: (t: AlertThresholds, value: number) => AlertThresholds;
  format: (t: AlertThresholds) => string;
}

const thresholds: Threshold[] = [
  {
    key: "consumerLag",
    title: "A consumer group falls behind",
    description: "When its oldest unconsumed message on a topic is older than",
    unit: "minutes",
    min: 1,
    max: 1440,
    get: (t) => t.consumerLag.seconds / 60,
    set: (t, value) => ({ ...t, consumerLag: { ...t.consumerLag, seconds: value * 60 } }),
    format: (t) => formatDuration(t.consumerLag.seconds),
  },
  {
    key: "consumerStuck",
    title: "A consumer group is stuck",
    description: "When it has not committed on a topic, while messages wait and arrive, for",
    unit: "minutes",
    min: 5,
    max: 1440,
    get: (t) => t.consumerStuck.seconds / 60,
    set: (t, value) => ({ ...t, consumerStuck: { ...t.consumerStuck, seconds: value * 60 } }),
    format: (t) => formatDuration(t.consumerStuck.seconds),
  },
  {
    key: "throttling",
    title: "A client is throttled",
    description: "When the brokers delay a client's requests, for 5 minutes, by more than",
    unit: "ms",
    min: 10,
    max: 60000,
    get: (t) => t.throttling.milliseconds,
    set: (t, value) => ({ ...t, throttling: { ...t.throttling, milliseconds: value } }),
    format: (t) => `${String(t.throttling.milliseconds)} ms`,
  },
  {
    key: "errors",
    title: "Requests fail",
    description:
      "When, for 5 minutes, the share of a topic's requests or a producer's records that fail is over",
    unit: "%",
    min: 1,
    max: 100,
    get: (t) => t.errors.percent,
    set: (t, value) => ({ ...t, errors: { ...t.errors, percent: value } }),
    format: (t) => `${String(t.errors.percent)}%`,
  },
];

function ThresholdsCard({
  settings,
  admin,
  change,
}: {
  settings: AlertSettings;
  admin: boolean;
  change: ReturnType<typeof useChangeAlertSettings>;
}) {
  const [draft, setDraft] = useState(settings.thresholds);
  // What admins type, as typed, until it is a number.
  const [typed, setTyped] = useState<Partial<Record<keyof AlertThresholds, string>>>({});
  const invalid = thresholds.filter((threshold) => {
    const value = threshold.get(draft);
    return !Number.isInteger(value) || value < threshold.min || value > threshold.max;
  });
  const changed = JSON.stringify(draft) !== JSON.stringify(settings.thresholds);
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>When alerts fire</h2>
        </CardTitle>
        <CardDescription>
          {admin
            ? "Changes take effect within a minute."
            : "Ask an admin of your organization to change these."}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="flex flex-col gap-5 text-sm"
          onSubmit={(event) => {
            event.preventDefault();
            if (invalid.length === 0) {
              change.mutate(draft);
            }
          }}
        >
          <ul className="flex flex-col divide-y">
            {thresholds.map((threshold) => {
              const enabled = draft[threshold.key].enabled;
              const id = `threshold-${threshold.key}`;
              const bad = invalid.includes(threshold);
              return (
                <li
                  key={threshold.key}
                  className="flex flex-col gap-3 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="flex items-start gap-3">
                    <Checkbox
                      id={`${id}-enabled`}
                      className="mt-0.5"
                      checked={enabled}
                      disabled={!admin}
                      onCheckedChange={(checked) => {
                        setDraft({
                          ...draft,
                          [threshold.key]: { ...draft[threshold.key], enabled: checked === true },
                        });
                      }}
                    />
                    <div className="flex flex-col gap-0.5">
                      <Label htmlFor={`${id}-enabled`}>{threshold.title}</Label>
                      <span className="text-muted-foreground">
                        {threshold.description}
                        {!admin && ` ${threshold.format(settings.thresholds)}.`}
                        {!admin && !settings.thresholds[threshold.key].enabled && " Off."}
                      </span>
                    </div>
                  </div>
                  {admin && (
                    <div className="flex shrink-0 flex-col gap-1 pl-7 sm:items-end sm:pl-0">
                      <div className="flex items-center gap-2">
                        <Input
                          id={id}
                          aria-label={`${threshold.title}, in ${threshold.unit}`}
                          aria-invalid={bad}
                          type="number"
                          inputMode="numeric"
                          min={threshold.min}
                          max={threshold.max}
                          className="w-24 text-right tabular-nums"
                          disabled={!enabled}
                          value={typed[threshold.key] ?? String(threshold.get(draft))}
                          onChange={(event) => {
                            setTyped({ ...typed, [threshold.key]: event.target.value });
                            const value = Number(event.target.value);
                            setDraft(threshold.set(draft, event.target.value === "" ? NaN : value));
                          }}
                        />
                        <span className="w-14 text-muted-foreground">{threshold.unit}</span>
                      </div>
                      <span className={bad ? "text-destructive" : "text-xs text-muted-foreground"}>
                        {bad
                          ? `From ${String(threshold.min)} to ${String(threshold.max)}`
                          : `Default ${threshold.format(settings.defaults)}`}
                      </span>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
          {admin && (
            <div className="flex items-center gap-3">
              <Button type="submit" disabled={!changed || invalid.length > 0 || change.isPending}>
                {change.isPending ? "Saving" : "Save"}
              </Button>
              {change.isSuccess && !changed && (
                <span role="status" className="text-muted-foreground">
                  Saved
                </span>
              )}
              {change.error && (
                <span role="alert" className="text-destructive">
                  {errorText(change.error)}
                </span>
              )}
            </div>
          )}
        </form>
      </CardContent>
    </Card>
  );
}

/** One receiver: what it is, and what to enter for it. */
interface Receiver {
  kind: ReceiverKind;
  title: string;
  description: string;
  label: string;
  placeholder: string;
}

const receivers: Receiver[] = [
  {
    kind: "slack",
    title: "Slack",
    description: "Posts to a channel through an incoming webhook, as alerts fire and resolve.",
    label: "Incoming webhook URL",
    placeholder: "https://hooks.slack.com/services/…",
  },
  {
    kind: "pagerDuty",
    title: "PagerDuty",
    description: "Raises and resolves incidents through an Events API v2 integration.",
    label: "Integration routing key",
    placeholder: "32 letters and digits",
  },
  {
    kind: "webhook",
    title: "Webhook",
    description: "Posts Alertmanager's webhook JSON to your URL, as alerts fire and resolve.",
    label: "Webhook URL",
    placeholder: "https://alerts.example.com/changeport",
  },
];

function ReceiversCard({ settings, admin }: { settings: AlertSettings; admin: boolean }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Where alerts go</h2>
        </CardTitle>
        <CardDescription>
          Every alert shows here and on the overview. Changeport keeps what you enter where nobody
          can read it back, so to change one, enter it again.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col divide-y text-sm">
          {receivers.map((receiver) => (
            <ReceiverRow
              key={receiver.kind}
              receiver={receiver}
              hint={settings.receivers[receiver.kind]?.hint}
              set={settings.receivers[receiver.kind] !== undefined}
              admin={admin}
            />
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

function ReceiverRow({
  receiver,
  hint,
  set,
  admin,
}: {
  receiver: Receiver;
  hint: string | undefined;
  set: boolean;
  admin: boolean;
}) {
  const remove = useRemoveAlertReceiver(receiver.kind);
  return (
    <li className="flex flex-col gap-3 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-col gap-0.5">
        <h3 className="font-medium">{receiver.title}</h3>
        <span className="text-muted-foreground">{receiver.description}</span>
        <span className={set ? "[overflow-wrap:anywhere]" : "text-muted-foreground"}>
          {set ? (hint ?? "Set") : "Not set"}
        </span>
        {remove.error && (
          <span role="alert" className="text-destructive">
            {errorText(remove.error)}
          </span>
        )}
      </div>
      {admin && (
        <div className="flex shrink-0 gap-2">
          <ReceiverDialog receiver={receiver} set={set} />
          {set && (
            <Button
              variant="outline"
              disabled={remove.isPending}
              onClick={() => {
                remove.mutate();
              }}
            >
              {remove.isPending ? "Removing" : "Remove"}
            </Button>
          )}
        </div>
      )}
    </li>
  );
}

function ReceiverDialog({ receiver, set }: { receiver: Receiver; set: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline">{set ? "Change" : "Set up"}</Button>
      </DialogTrigger>
      <DialogContent>
        {open && (
          <ReceiverForm
            receiver={receiver}
            onSet={() => {
              setOpen(false);
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ReceiverForm({ receiver, onSet }: { receiver: Receiver; onSet: () => void }) {
  const setReceiver = useSetAlertReceiver(receiver.kind);
  const [value, setValue] = useState("");
  const id = `receiver-${receiver.kind}`;
  return (
    <form
      className="flex flex-col gap-5"
      onSubmit={(event) => {
        event.preventDefault();
        if (value.trim() !== "") {
          setReceiver.mutate(value, { onSuccess: onSet });
        }
      }}
    >
      <DialogHeader>
        <DialogTitle>Send alerts to {receiver.title}</DialogTitle>
        <DialogDescription>{receiver.description}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor={id}>{receiver.label}</Label>
        <Input
          id={id}
          type="password"
          autoComplete="off"
          spellCheck={false}
          placeholder={receiver.placeholder}
          value={value}
          aria-invalid={setReceiver.isError}
          onChange={(event) => {
            setValue(event.target.value);
          }}
        />
      </div>
      {setReceiver.error && (
        <p role="alert" className="text-sm text-destructive">
          {errorText(setReceiver.error)}
        </p>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={value.trim() === "" || setReceiver.isPending}>
          {setReceiver.isPending ? "Saving" : "Save"}
        </Button>
      </DialogFooter>
    </form>
  );
}
