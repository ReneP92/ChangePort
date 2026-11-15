import { useSyncExternalStore } from "react";

/** The viewer's choice: light, dark, or whatever their system uses. */
export type Theme = "light" | "dark" | "system";

const key = "changeport-theme";
const media = () => window.matchMedia("(prefers-color-scheme: dark)");

// The choice when the browser keeps none, as in a private window.
let unsaved: Theme | undefined;

function stored(): Theme {
  try {
    const theme = localStorage.getItem(key);
    return theme === "light" || theme === "dark" ? theme : (unsaved ?? "system");
  } catch {
    return unsaved ?? "system";
  }
}

/** Applies a theme to the page, as public/theme.js does before it draws. */
function apply(theme: Theme) {
  const dark = theme === "dark" || (theme === "system" && media().matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
}

const listeners = new Set<() => void>();

function changed() {
  apply(stored());
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: () => void) {
  if (listeners.size === 0) {
    // The system's own change, and another tab's choice.
    media().addEventListener("change", changed);
    window.addEventListener("storage", changed);
  }
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      media().removeEventListener("change", changed);
      window.removeEventListener("storage", changed);
    }
  };
}

/** Sets the viewer's theme, for this browser. */
export function setTheme(theme: Theme) {
  try {
    if (theme === "system") {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, theme);
    }
  } catch {
    // Storage may be off, as in a private window: the choice lasts the page.
    unsaved = theme;
  }
  apply(theme);
  for (const listener of listeners) {
    listener();
  }
}

/** The viewer's theme, which follows their system's when they chose none. */
export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, stored, () => "system");
}
