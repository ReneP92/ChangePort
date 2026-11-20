import { useEffect, useRef, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { useMe } from "@/api/queries";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { modifier, ShortcutsContext, typing } from "@/lib/shortcuts";
import { useEnvironmentPath } from "@/lib/useEnvironment";

/** A page the keyboard goes to, as G then its key, as in Linear. */
interface GoTo {
  key: string;
  label: string;
  path: string;
}

/** How long after G its second key still counts. */
const sequenceTimeout = 1500;

/**
 * The console's keyboard shortcuts: G then a key goes to a page, and Mod+/
 * lists them all. The sidebar's own Mod+B collapses it. Keys typed into a
 * field, or while a dialog or menu is open, are left alone.
 */
export function KeyboardShortcuts({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const pages = useGoTo();
  const navigate = useNavigate();
  // The listener stays the same across renders, which polling causes often,
  // so that a G pressed just before one still counts.
  const latest = useRef({ pages, navigate });
  useEffect(() => {
    latest.current = { pages, navigate };
  });
  useEffect(() => {
    let pressedG = 0;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "/" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        setOpen((open) => !open);
        return;
      }
      if (event.metaKey || event.ctrlKey || event.altKey || typing(event.target)) {
        pressedG = 0;
        return;
      }
      const key = event.key.toLowerCase();
      if (pressedG > 0 && Date.now() - pressedG < sequenceTimeout) {
        pressedG = 0;
        const page = latest.current.pages.find((page) => page.key === key);
        if (page) {
          event.preventDefault();
          void latest.current.navigate(page.path);
        }
        return;
      }
      pressedG = key === "g" ? Date.now() : 0;
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, []);
  return (
    <ShortcutsContext.Provider
      value={() => {
        setOpen(true);
      }}
    >
      {children}
      <Dialog open={open} onOpenChange={setOpen}>
        {/* It has nothing to act on, so it takes the focus itself rather than
            ringing its close button. */}
        <DialogContent
          className="sm:max-w-md"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            (event.currentTarget as HTMLElement | null)?.focus();
          }}
        >
          <DialogHeader>
            <DialogTitle>Keyboard shortcuts</DialogTitle>
            <DialogDescription>Press G, then the page&apos;s key, to go there.</DialogDescription>
          </DialogHeader>
          <dl className="flex flex-col text-sm">
            {pages.map((page) => (
              <Shortcut key={page.key} label={`Go to ${page.label}`} keys={["G", page.key]} then />
            ))}
            <Shortcut label="Collapse or expand the menu" keys={[modifier(), "B"]} />
            <Shortcut label="Show keyboard shortcuts" keys={[modifier(), "/"]} />
          </dl>
        </DialogContent>
      </Dialog>
    </ShortcutsContext.Provider>
  );
}

/** A shortcut's row: what it does, and its keys. */
function Shortcut({ label, keys, then }: { label: string; keys: string[]; then?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b py-2 last:border-b-0">
      <dt>{label}</dt>
      <dd className="flex items-center gap-1 text-xs text-muted-foreground">
        {keys.map((key, index) => (
          <span key={key} className="flex items-center gap-1">
            {index > 0 && then && <span>then</span>}
            <kbd className="min-w-5 rounded border bg-muted px-1.5 py-0.5 text-center font-sans font-medium text-foreground">
              {key.length === 1 ? key.toUpperCase() : key}
            </kbd>
          </span>
        ))}
      </dd>
    </div>
  );
}

/** The pages G goes to: the environment's and the organization's, or the operator console's. */
function useGoTo(): GoTo[] {
  const access = useMe().data?.access;
  const environmentPath = useEnvironmentPath();
  if (access === "customer") {
    return [
      { key: "o", label: "the overview", path: environmentPath("/") },
      { key: "s", label: "sources", path: environmentPath("/sources") },
      { key: "a", label: "alerts", path: environmentPath("/alerts") },
      { key: "e", label: "environments", path: "/environments" },
      { key: "t", label: "the team", path: "/team" },
    ];
  }
  if (access === "operator") {
    return [
      { key: "f", label: "the fleet", path: "/operator/fleet" },
      { key: "c", label: "customers", path: "/operator" },
      { key: "r", label: "trial requests", path: "/operator/trials" },
    ];
  }
  return [];
}
