import { afterEach, describe, expect, test } from "bun:test";
import { act, cleanup, renderHook } from "@testing-library/react";
import { setTheme, useTheme } from "@/lib/theme";

const dark = () => document.documentElement.classList.contains("dark");

afterEach(() => {
  cleanup();
  setTheme("light");
  localStorage.clear();
});

describe("setTheme", () => {
  test("keeps dark in the browser, and light as no choice at all", () => {
    setTheme("dark");
    expect(dark()).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe("dark");
    expect(localStorage.getItem("changeport-theme")).toBe("dark");
    setTheme("light");
    expect(dark()).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe("light");
    expect(localStorage.getItem("changeport-theme")).toBeNull();
  });

  // A browser's storage that refuses some writes, in place of the real one
  // for the length of a test.
  const refusing = (refuses: (method: string) => boolean, run: () => void) => {
    const real = globalThis.localStorage;
    const values = new Map<string, string>();
    const refuse = (method: string) => {
      if (refuses(method)) {
        throw new DOMException(method, "QuotaExceededError");
      }
    };
    const storage = {
      getItem: (key: string) => (refuse("getItem"), values.get(key) ?? null),
      setItem: (key: string, value: string) => (refuse("setItem"), void values.set(key, value)),
      removeItem: (key: string) => (refuse("removeItem"), void values.delete(key)),
    };
    Object.defineProperty(globalThis, "localStorage", { value: storage, configurable: true });
    try {
      run();
    } finally {
      Object.defineProperty(globalThis, "localStorage", { value: real, configurable: true });
    }
  };

  test("lasts the page when the browser can't keep it", () => {
    // Storage full: writing fails, removing works.
    refusing(
      (method) => method === "setItem",
      () => {
        setTheme("dark");
        expect(dark()).toBe(true);
        // Light again, not stuck dark.
        setTheme("light");
        expect(dark()).toBe(false);
      },
    );
  });

  test("lasts the page when storage is off altogether", () => {
    refusing(
      () => true,
      () => {
        setTheme("dark");
        expect(dark()).toBe(true);
        setTheme("light");
        expect(dark()).toBe(false);
      },
    );
  });
});

describe("another tab's choice", () => {
  const otherTab = (value: string | null) => {
    if (value === null) {
      localStorage.removeItem("changeport-theme");
    } else {
      localStorage.setItem("changeport-theme", value);
    }
    window.dispatchEvent(new StorageEvent("storage", { key: "changeport-theme", newValue: value }));
  };

  test("applies to the page even with no switch showing", () => {
    otherTab("dark");
    expect(dark()).toBe(true);
    otherTab(null);
    expect(dark()).toBe(false);
  });

  test("flips the switch", () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current).toBe("light");
    act(() => otherTab("dark"));
    expect(result.current).toBe("dark");
    act(() => setTheme("light"));
    expect(result.current).toBe("light");
    expect(dark()).toBe(false);
  });
});
