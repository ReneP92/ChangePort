import { useSyncExternalStore } from "react";

/** The console's theme: light, the default, or dark. */
export type Theme = "light" | "dark";

const key = "changeport-theme";

// The choice when the browser can't keep it, as in a private window or with
// storage full. It lasts the page.
let unsaved: Theme | undefined;

function stored(): Theme {
  try {
    return localStorage.getItem(key) === "dark" ? "dark" : (unsaved ?? "light");
  } catch {
    return unsaved ?? "light";
  }
}

/** Applies a theme to the page, as index.html's script does before it draws. */
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

// The page follows another tab's choice whether or not anything shows the
// switch, and the stored choice from the start, should index.html's script
// not have run.
window.addEventListener("storage", (event) => {
  if (event.key === key || event.key === null) {
    changed();
  }
});
apply(stored());

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Sets the theme, for this browser. */
export function setTheme(theme: Theme) {
  unsaved = theme;
  try {
    if (theme === "light") {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, theme);
    }
    unsaved = undefined;
  } catch {
    // Kept in unsaved instead.
  }
  changed();
}

/** The theme the viewer chose, light unless dark. */
export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, stored, () => "light");
}
