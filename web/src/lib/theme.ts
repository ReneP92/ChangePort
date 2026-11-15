import { useSyncExternalStore } from "react";

/** The console's theme: light, the default, or dark. */
export type Theme = "light" | "dark";

const key = "changeport-theme";

// The choice when the browser keeps none, as in a private window.
let unsaved: Theme | undefined;

function stored(): Theme {
  try {
    return localStorage.getItem(key) === "dark" ? "dark" : (unsaved ?? "light");
  } catch {
    return unsaved ?? "light";
  }
}

/** Applies a theme to the page, as public/theme.js does before it draws. */
function apply(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark");
  document.documentElement.style.colorScheme = theme;
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
    // Another tab's choice.
    window.addEventListener("storage", changed);
  }
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      window.removeEventListener("storage", changed);
    }
  };
}

/** Sets the theme, for this browser. */
export function setTheme(theme: Theme) {
  try {
    if (theme === "light") {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, theme);
    }
  } catch {
    // Storage may be off, as in a private window: the choice lasts the page.
    unsaved = theme;
  }
  changed();
}

/** The theme the viewer chose, light unless dark. */
export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, stored, () => "light");
}
