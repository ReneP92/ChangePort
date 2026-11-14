import { createContext, useContext } from "react";

/** Opens the list of keyboard shortcuts; KeyboardShortcuts provides it. */
export const ShortcutsContext = createContext<() => void>(() => undefined);

/** Opens the list of keyboard shortcuts, as the help menu does. */
export function useOpenShortcuts(): () => void {
  return useContext(ShortcutsContext);
}

/** The key shortcuts are pressed with: ⌘ on Apple's devices, Ctrl elsewhere. */
export function modifier(): string {
  return /Mac|iPhone|iPad/.test(navigator.userAgent) ? "⌘" : "Ctrl";
}

/**
 * Whether a key pressed on target is someone typing, or meant for an open
 * dialog or menu, rather than a shortcut. A menu closing keeps the focus
 * while it fades, which a key pressed straight after Escape would otherwise
 * be lost to.
 */
export function typing(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const popup = target.closest(
    '[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]',
  );
  return (
    target.isContentEditable ||
    ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName) ||
    (popup !== null && popup.getAttribute("data-state") !== "closed")
  );
}
