import { afterEach, describe, expect, test } from "bun:test";
import type { Environment } from "@/api/client";
import {
  defaultEnvironment,
  environmentFromPath,
  environmentPath,
  rememberEnvironment,
} from "@/lib/environments";

const environment = (name: string): Environment => ({
  name,
  customerID: `c-${name}`,
  profile: "dev",
  ready: true,
});

afterEach(() => {
  localStorage.clear();
});

describe("environment paths", () => {
  test("read the environment out of a page's path", () => {
    expect(environmentFromPath("/e/staging/topics/orders")).toBe("staging");
    expect(environmentFromPath("/e/production")).toBe("production");
    expect(environmentFromPath("/team")).toBeUndefined();
    expect(environmentFromPath("/topics/orders")).toBeUndefined();
  });

  test("put pages in an environment", () => {
    expect(environmentPath("staging", "/")).toBe("/e/staging");
    expect(environmentPath("staging", "/topics/orders")).toBe("/e/staging/topics/orders");
  });
});

describe("defaultEnvironment", () => {
  const environments = [environment("production"), environment("dev"), environment("staging")];

  test("is the one last used in this browser, while the organization has it", () => {
    expect(defaultEnvironment("org_acme", environments)).toBe("production");
    rememberEnvironment("org_acme", "staging");
    expect(defaultEnvironment("org_acme", environments)).toBe("staging");
    expect(defaultEnvironment("org_globex", environments)).toBe("production");
    expect(defaultEnvironment("org_acme", [environment("production")])).toBe("production");
  });

  test("is none without environments", () => {
    expect(defaultEnvironment("org_acme", [])).toBeUndefined();
  });
});
