import { afterEach, describe, expect, test } from "bun:test";
import type { Environment, Me } from "@/api/client";
import {
  defaultEnvironment,
  environmentFromPath,
  environmentPath,
  meWithEnvironments,
  rememberEnvironment,
  renameRemembered,
} from "@/lib/environments";

const environment = (name: string, more: Partial<Environment> = {}): Environment => ({
  name,
  customerID: `c-${name}`,
  profile: "dev",
  ready: true,
  deleting: false,
  default: false,
  deletionProtection: false,
  trial: false,
  ...more,
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

describe("after a change to the environments", () => {
  const me: Me = {
    userID: "user_2",
    access: "customer",
    customerID: "c-production",
    environments: [environment("production", { default: true }), environment("staging")],
  };

  test("the viewer switches between those not being removed, the default first", () => {
    const after = meWithEnvironments(me, {
      limit: 5,
      environments: [
        environment("qa", { default: true }),
        environment("production"),
        environment("staging", { deleting: true }),
      ],
    });
    expect(after.environments?.map((e) => e.name)).toEqual(["qa", "production"]);
    expect(after.customerID).toBe("c-qa");
    const operator: Me = { userID: "user_6", access: "operator" };
    expect(meWithEnvironments(operator, { limit: 5, environments: [] })).toBe(operator);
  });

  test("a renamed environment stays the one this browser remembers", () => {
    rememberEnvironment("org_acme", "staging");
    renameRemembered("org_acme", "production", "live");
    expect(defaultEnvironment("org_acme", [environment("live"), environment("staging")])).toBe(
      "staging",
    );
    renameRemembered("org_acme", "staging", "qa");
    expect(defaultEnvironment("org_acme", [environment("live"), environment("qa")])).toBe("qa");
  });
});
