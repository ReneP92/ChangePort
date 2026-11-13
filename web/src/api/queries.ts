import { useAuth } from "@clerk/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, use, useEffect } from "react";
import { useLocation } from "react-router";
import {
  ApiError,
  unwrap,
  type Alert,
  type AlertList,
  type AlertThresholds,
  type ApiClient,
  type EnvironmentChange,
  type Environments,
  type Me,
  type NewCustomer,
  type NewEnvironment,
  type NewSource,
  type NewTopic,
  type NewTrialRequest,
  type ReceiverKind,
  type SetupSQLRequest,
  type SourceChange,
  type SourceCheckRequest,
  type SourceSQLRequest,
  type Team,
  type TeamRole,
  type ThroughputRange,
  type TrialApproval,
  type TrialChange,
} from "@/api/client";
import { environmentFromPath, environmentHeaders, meWithEnvironments } from "@/lib/environments";
import { readEvents } from "@/lib/sse";

/** The API client of the signed-in user, which ApiProvider provides. */
export const ApiContext = createContext<ApiClient | null>(null);

function useApi(): ApiClient {
  const client = use(ApiContext);
  if (!client) {
    throw new Error("useApi needs an ApiProvider");
  }
  return client;
}

/**
 * Queries are per active organization and environment, each a cluster of
 * its own, so every query is keyed by both: switching environment never
 * shows another's data.
 */
function useOrganizationKey(): string {
  const organization = useAuth().orgId ?? "none";
  const environment = environmentFromPath(useLocation().pathname);
  return environment === undefined ? organization : `${organization}/${environment}`;
}

/** The signed-in user and what they may see. */
export function useMe() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["me", organization],
    queryFn: async () => unwrap(await api.GET("/api/me")),
  });
}

/** The customer of the active organization. */
export function useCustomer() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer")),
    refetchInterval: 10_000,
  });
}

/** Every customer, for operators. */
export function useCustomers() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["operator", "customers", organization],
    queryFn: async () => unwrap(await api.GET("/api/operator/customers")).customers,
    refetchInterval: 10_000,
  });
}

/**
 * One customer, for operators, refreshed often while its cluster is being
 * created or deleted. Once it is gone, the query fails with a 404.
 */
export function useOperatorCustomer(id: string) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["operator", "customers", organization, id],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/operator/customers/{customerID}", {
          params: { path: { customerID: id } },
        }),
      ),
    refetchInterval: (query) => {
      const customer = query.state.data;
      if (query.state.error instanceof ApiError && query.state.error.status === 404) {
        return false;
      }
      return customer?.ready && !customer.deleting ? 10_000 : 2_000;
    },
    retry: (failures, error) =>
      !(error instanceof ApiError && error.status === 404) && failures < 3,
  });
}

/** Adds an environment, a cluster of its own, to a customer's organization. */
export function useAddEnvironment(customerID: string) {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (environment: NewEnvironment) =>
      unwrap(
        await api.POST("/api/operator/customers/{customerID}/environments", {
          params: { path: { customerID } },
          body: environment,
        }),
      ),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["operator", "customers"] }),
  });
}

/** Provisions a customer, then refreshes the customers. */
export function useProvisionCustomer() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (customer: NewCustomer) =>
      unwrap(await api.POST("/api/operator/customers", { body: customer })),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["operator", "customers"] }),
  });
}

/** Extends a customer's trial or converts the customer, then refreshes. */
export function useChangeTrial() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, change }: { id: string; change: TrialChange }) =>
      unwrap(
        await api.PATCH("/api/operator/customers/{customerID}/trial", {
          params: { path: { customerID: id } },
          body: change,
        }),
      ),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["operator", "customers"] }),
  });
}

/** Deprovisions a customer, then refreshes the customers. */
export function useDeprovisionCustomer() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      const { error, response } = await api.DELETE("/api/operator/customers/{customerID}", {
        params: { path: { customerID: id } },
      });
      if (!response.ok) {
        throw new ApiError(response.status, error);
      }
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: ["operator", "customers"] }),
  });
}

/**
 * The signed-in user's trial requests, newest first, refreshed while one
 * waits for an operator's decision.
 */
export function useMyTrialRequests() {
  const api = useApi();
  return useQuery({
    queryKey: ["trial-requests", "mine"],
    queryFn: async () => unwrap(await api.GET("/api/trial-requests/mine")).trialRequests,
    refetchInterval: (query) =>
      query.state.data?.some((request) => request.phase === "Pending") ? 5_000 : false,
  });
}

/** Requests a trial, then refreshes the user's requests. */
export function useRequestTrial() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (request: NewTrialRequest) =>
      unwrap(await api.POST("/api/trial-requests", { body: request })),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["trial-requests", "mine"] }),
  });
}

/** Every trial request, newest first, for operators. */
export function useTrialRequests() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["operator", "trial-requests", organization],
    queryFn: async () => unwrap(await api.GET("/api/operator/trial-requests")).trialRequests,
    refetchInterval: 10_000,
  });
}

/**
 * Approves a pending trial request, which provisions the requester's
 * customer, then refreshes the requests and the customers.
 */
export function useApproveTrialRequest() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, approval }: { id: string; approval: TrialApproval }) =>
      unwrap(
        await api.POST("/api/operator/trial-requests/{trialRequestID}/approve", {
          params: { path: { trialRequestID: id } },
          body: approval,
        }),
      ),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["operator"] }),
  });
}

/** Declines a pending trial request with a reason, then refreshes them. */
export function useDeclineTrialRequest() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) =>
      unwrap(
        await api.POST("/api/operator/trial-requests/{trialRequestID}/decline", {
          params: { path: { trialRequestID: id } },
          body: { reason },
        }),
      ),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["operator", "trial-requests"] }),
  });
}

/** How the active organization's customer connects. */
export function useConnection() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "connection", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/connection")),
  });
}

/** The brokers and partitions of the active organization's cluster. */
export function useHealth() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "health", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/health")),
    refetchInterval: 15_000,
  });
}

/** The topics of the active organization's cluster. */
export function useTopics(enabled = true) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "topics", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/topics")).topics,
    refetchInterval: 15_000,
    // A cluster being created has no topics to ask about yet.
    enabled,
  });
}

/**
 * How data flows through the cluster now, for the topology map: producers,
 * topics, consumer groups and the bytes between them. Clients report every
 * few seconds to a minute, so it refreshes every 15 seconds.
 */
export function useTopology() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "topology", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/topology")),
    refetchInterval: 15_000,
  });
}

/** The bytes in and out per second over range, of the cluster or a topic. */
export function useThroughput(range: ThroughputRange, topic?: string) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "throughput", range, topic ?? "", organization],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/customer/throughput", {
          params: { query: { range, ...(topic === undefined ? {} : { topic }) } },
        }),
      ),
    refetchInterval: 60_000,
  });
}

/** What a timeline is about: a topic, a consumer group, or everything. */
export interface EventsAbout {
  topic?: string;
  consumerGroup?: string;
}

/**
 * The changes in the cluster over range, newest first: all of them, or
 * those about a topic or a consumer group (docs/timeline.md).
 */
export function useEvents(range: ThroughputRange, about: EventsAbout = {}) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: [
      "customer",
      "events",
      range,
      about.topic ?? "",
      about.consumerGroup ?? "",
      organization,
    ],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/customer/events", {
          params: { query: { range, ...about } },
        }),
      ),
    refetchInterval: 15_000,
  });
}

/** How far behind a consumer group was on each topic it reads, over range. */
export function useConsumerGroupLag(group: string, range: ThroughputRange) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "consumer-group-lag", group, range, organization],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/customer/consumer-groups/{group}/lag", {
          params: { path: { group }, query: { range } },
        }),
      ),
    refetchInterval: 60_000,
  });
}

/**
 * The consumer groups' lag per topic, most behind first, with the last hour
 * of it. The agent measures it every 30 seconds; a page waiting for a new
 * group asks more often.
 */
export function useConsumerGroups(refetchInterval = 30_000) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "consumer-groups", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/consumer-groups")).groups,
    refetchInterval,
  });
}

/** Downloads the admin user's client.properties as a file. */
export function useDownloadClientProperties() {
  const api = useApi();
  /** Saves the file under the name the console gives it, or as saveAs. */
  return async (saveAs?: string) => {
    const { data, error, response } = await api.GET("/api/customer/client-properties", {
      parseAs: "text",
    });
    if (data === undefined) {
      throw new ApiError(response.status, error);
    }
    const name =
      /filename="([^"]+)"/.exec(response.headers.get("Content-Disposition") ?? "")?.[1] ??
      "client.properties";
    const url = URL.createObjectURL(new Blob([data], { type: "text/plain" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = saveAs ?? name;
    link.click();
    URL.revokeObjectURL(url);
  };
}

/** The guardrails on the active organization's topics. */
export function useTopicPolicy() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "topic-policy", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/topic-policy")),
    staleTime: Infinity,
  });
}

/** The consumer groups that read a topic, while enabled. */
export function useTopicConsumerGroups(topic: string, enabled: boolean) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "topic-consumer-groups", topic, organization],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/customer/topics/{topic}/consumer-groups", {
          params: { path: { topic } },
        }),
      ).groups,
    enabled,
  });
}

/** Creates a topic, then refreshes the topics. */
export function useCreateTopic() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (topic: NewTopic) =>
      unwrap(await api.POST("/api/customer/topics", { body: topic })),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["customer", "topics"] }),
  });
}

/** Deletes a topic, then refreshes the topics. */
export function useDeleteTopic() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (topic: string) => {
      const { error, response } = await api.DELETE("/api/customer/topics/{topic}", {
        params: { path: { topic } },
      });
      if (!response.ok) {
        throw new ApiError(response.status, error);
      }
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: ["customer", "topics"] }),
  });
}

/** What adding a Postgres source offers: providers, presets and the egress addresses. */
export function useSourceOptions() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "source-options", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/source-options")),
    staleTime: Infinity,
  });
}

/** The active organization's Postgres sources. */
export function useSources() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "sources", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/sources")).sources,
    refetchInterval: 10_000,
  });
}

/**
 * One Postgres source, refreshed often while it copies tables or is being
 * deleted. Once it is gone, the query fails with a 404.
 */
export function useSource(name: string) {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "sources", organization, name],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/customer/sources/{source}", { params: { path: { source: name } } }),
      ),
    refetchInterval: (query) => {
      if (query.state.error instanceof ApiError && query.state.error.status === 404) {
        return false;
      }
      const source = query.state.data;
      return source?.phase === "Streaming" || source?.phase === "Stopped" ? 10_000 : 3_000;
    },
    retry: (failures, error) =>
      !(error instanceof ApiError && error.status === 404) && failures < 3,
  });
}

/** The SQL of a new source's steps, which the first step's answer alone holds a password in. */
export function useSetupSQL() {
  const api = useApi();
  return useMutation({
    mutationFn: async (request: SetupSQLRequest) =>
      unwrap(await api.POST("/api/customer/sources/setup-sql", { body: request })),
  });
}

/** Checks a database for a source, keeping nothing. */
export function useCheckSource() {
  const api = useApi();
  return useMutation({
    mutationFn: async (request: SourceCheckRequest) =>
      unwrap(await api.POST("/api/customer/sources/checks", { body: request })),
  });
}

/** Lists a database's tables for the table picker of a new source. */
export function useListSourceTables() {
  const api = useApi();
  return useMutation({
    mutationFn: async (request: SourceCheckRequest) =>
      unwrap(await api.POST("/api/customer/sources/tables", { body: request })).tables,
  });
}

/** Creates a source, then refreshes the sources. */
export function useCreateSource() {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (source: NewSource) =>
      unwrap(await api.POST("/api/customer/sources", { body: source })),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["customer", "sources"] }),
  });
}

/** Changes a source's tables, retention or state, then refreshes the sources and topics. */
export function useChangeSource(name: string) {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (change: SourceChange) =>
      unwrap(
        await api.PATCH("/api/customer/sources/{source}", {
          params: { path: { source: name } },
          body: change,
        }),
      ),
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["customer", "sources"] });
      await queries.invalidateQueries({ queryKey: ["customer", "topics"] });
    },
  });
}

/** Deletes a source, with its name typed, then refreshes the sources. */
export function useDeleteSource(name: string) {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { error, response } = await api.DELETE("/api/customer/sources/{source}", {
        params: { path: { source: name }, query: { confirm: name } },
      });
      if (!response.ok) {
        throw new ApiError(response.status, error);
      }
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: ["customer", "sources"] }),
  });
}

/** The SQL of a change to an existing source's database. */
export function useSourceSQL(name: string) {
  const api = useApi();
  return useMutation({
    mutationFn: async (request: SourceSQLRequest) =>
      unwrap(
        await api.POST("/api/customer/sources/{source}/sql", {
          params: { path: { source: name } },
          body: request,
        }),
      ),
  });
}

/** Has a source use a new password, once it logs in. */
export function useSetSourcePassword(name: string) {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (password: string) => {
      const { error, response } = await api.PUT("/api/customer/sources/{source}/password", {
        params: { path: { source: name } },
        body: { password },
      });
      if (!response.ok) {
        throw new ApiError(response.status, error);
      }
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: ["customer", "sources"] }),
  });
}

/** Confirms that the customer dropped a deleted source's slot themselves. */
export function useConfirmSlotDropped(name: string) {
  const api = useApi();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { error, response } = await api.POST("/api/customer/sources/{source}/slot-dropped", {
        params: { path: { source: name } },
      });
      if (!response.ok) {
        throw new ApiError(response.status, error);
      }
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: ["customer", "sources"] }),
  });
}

/**
 * The tables of an existing source's database, which the source controller
 * lists on request: the console cannot log in to the database itself. While
 * enabled, it asks once, then polls until the answer is there.
 */
export function useSourceCatalog(name: string, enabled: boolean) {
  const api = useApi();
  const organization = useOrganizationKey();
  const queries = useQueryClient();
  const key = ["customer", "source-catalog", organization, name];
  const request = useQuery({
    queryKey: [...key, "request"],
    queryFn: async () =>
      unwrap(
        await api.POST("/api/customer/sources/{source}/catalog", {
          params: { path: { source: name } },
        }),
      ),
    enabled,
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
  const catalog = useQuery({
    queryKey: key,
    queryFn: async () =>
      unwrap(
        await api.GET("/api/customer/sources/{source}/catalog", {
          params: { path: { source: name } },
        }),
      ),
    enabled: enabled && request.isSuccess,
    gcTime: 0,
    refetchInterval: (query) => (query.state.data?.ready ? false : 1_000),
  });
  return {
    data: catalog.data?.ready ? catalog.data : undefined,
    error: request.error ?? catalog.error,
    retry: () => queries.resetQueries({ queryKey: key }),
  };
}

/**
 * The active alerts about the customer's own applications, which
 * useLiveAlerts keeps current.
 */
export function useAlerts() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "alerts", organization],
    queryFn: async (): Promise<Alert[]> => unwrap(await api.GET("/api/customer/alerts")).alerts,
    staleTime: Infinity,
  });
}

/** How long to wait before opening the stream again after it failed. */
function retryDelay(failures: number): number {
  return Math.min(30_000, 1_000 * 2 ** failures);
}

/**
 * Streams the customer's alerts into useAlerts while mounted, as they fire
 * and clear. The stream ends when the session token expires, and opens
 * again at once with a new one; after a failure, a little later each time.
 * Mount it once, where every page of a customer passes.
 */
export function useLiveAlerts() {
  const { getToken } = useAuth();
  const organization = useOrganizationKey();
  const queries = useQueryClient();
  useEffect(() => {
    const controller = new AbortController();
    const { signal } = controller;
    const sleep = (ms: number) =>
      new Promise<void>((resolve) => {
        if (signal.aborted) {
          resolve();
          return;
        }
        const timer = setTimeout(resolve, ms);
        signal.addEventListener("abort", () => {
          clearTimeout(timer);
          resolve();
        });
      });
    void (async () => {
      let failures = 0;
      while (!signal.aborted) {
        try {
          const token = await getToken();
          const response = await fetch(`${window.location.origin}/api/customer/alerts/stream`, {
            headers: {
              Accept: "text/event-stream",
              Authorization: `Bearer ${token ?? ""}`,
              ...environmentHeaders(),
            },
            signal,
          });
          if (!response.ok || !response.body) {
            throw new ApiError(response.status);
          }
          let received = false;
          for await (const event of readEvents(response.body)) {
            if (event.event === "alerts") {
              const list = JSON.parse(event.data) as AlertList;
              queries.setQueryData(["customer", "alerts", organization], list.alerts);
              received = true;
            }
          }
          // A stream that ended without a word did not work either.
          failures = received ? 0 : failures + 1;
        } catch {
          // Aborted too, when the page goes; the loop then ends.
          failures++;
        }
        await sleep(failures > 0 ? retryDelay(failures) : 0);
      }
    })();
    return () => {
      controller.abort();
    };
  }, [getToken, organization, queries]);
}

/** The members of the active organization and its pending invitations. */
export function useTeam() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "team", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/team")),
  });
}

/** A change to the team, which answers with the team as it is after it. */
function useTeamChange<T>(change: (api: ApiClient, input: T) => Promise<Team>) {
  const api = useApi();
  const organization = useOrganizationKey();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: T) => change(api, input),
    onSuccess: (team) => {
      queries.setQueryData(["customer", "team", organization], team);
    },
  });
}

/** Invites someone to the organization, for admins. */
export function useInviteTeammate() {
  return useTeamChange(async (api, invitation: { email: string; role: TeamRole }) =>
    unwrap(await api.POST("/api/customer/team/invitations", { body: invitation })),
  );
}

/** Revokes a pending invitation, for admins. */
export function useRevokeInvitation() {
  return useTeamChange(async (api, invitationID: string) =>
    unwrap(
      await api.DELETE("/api/customer/team/invitations/{invitationID}", {
        params: { path: { invitationID } },
      }),
    ),
  );
}

/** Makes a member an admin or a member, for admins. */
export function useChangeTeammateRole() {
  return useTeamChange(async (api, change: { userID: string; role: TeamRole }) =>
    unwrap(
      await api.PUT("/api/customer/team/members/{userID}", {
        params: { path: { userID: change.userID } },
        body: { role: change.role },
      }),
    ),
  );
}

/** Removes a member from the organization, for admins. */
export function useRemoveTeammate() {
  return useTeamChange(async (api, userID: string) =>
    unwrap(
      await api.DELETE("/api/customer/team/members/{userID}", { params: { path: { userID } } }),
    ),
  );
}

/**
 * The environments of the active organization, each a cluster of its own.
 * They are the whole organization's, so the key leaves out the environment
 * the page is in. While one starts or is being removed, they are read
 * again every few seconds.
 */
export function useEnvironments() {
  const api = useApi();
  const organization = useAuth().orgId ?? "none";
  return useQuery({
    queryKey: ["customer", "environments", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/environments")),
    refetchInterval: (query) =>
      query.state.data?.environments.some((e) => !e.ready || e.deleting) ? 3_000 : false,
  });
}

/**
 * A change to the environments, which answers with them as they are after
 * it. The viewer's environments follow at once, in every environment's
 * page, including one that a rename just named.
 */
function useEnvironmentChange<T>(change: (api: ApiClient, input: T) => Promise<Environments>) {
  const api = useApi();
  const organization = useAuth().orgId ?? "none";
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: T) => change(api, input),
    onSuccess: (list) => {
      // What was cached under a name the organization no longer uses, as
      // after a rename, is another environment's should the name come back.
      const live = (environments: Environments | undefined) =>
        (environments?.environments ?? []).filter((e) => !e.deleting).map((e) => e.name);
      const before = live(queries.getQueryData(["customer", "environments", organization]));
      for (const name of before.filter((name) => !live(list).includes(name))) {
        const key = `${organization}/${name}`;
        queries.removeQueries({ predicate: (query) => query.queryKey.includes(key) });
      }
      queries.setQueryData(["customer", "environments", organization], list);
      const cached = queries.getQueriesData<Me>({ queryKey: ["me"] });
      const me = cached.find(([, data]) => data?.access === "customer")?.[1];
      if (me === undefined) {
        return;
      }
      const updated = meWithEnvironments(me, list);
      for (const [key] of cached) {
        queries.setQueryData(key, updated);
      }
      for (const environment of updated.environments ?? []) {
        queries.setQueryData(["me", `${organization}/${environment.name}`], updated);
      }
    },
  });
}

/** Adds an environment to the organization, for admins. */
export function useCreateEnvironment() {
  return useEnvironmentChange(async (api, environment: NewEnvironment) =>
    unwrap(await api.POST("/api/customer/environments", { body: environment })),
  );
}

/** Renames an environment, makes it the default or protects it, for admins. */
export function useChangeEnvironment() {
  return useEnvironmentChange(
    async (api, { customerID, ...change }: EnvironmentChange & { customerID: string }) =>
      unwrap(
        await api.PATCH("/api/customer/environments/{customerID}", {
          params: { path: { customerID } },
          body: change,
        }),
      ),
  );
}

/** Removes an environment, deleting its cluster, for admins. */
export function useRemoveEnvironment() {
  return useEnvironmentChange(async (api, customerID: string) =>
    unwrap(
      await api.DELETE("/api/customer/environments/{customerID}", {
        params: { path: { customerID } },
      }),
    ),
  );
}

/** The thresholds of the customer's alerts, and its receivers. */
export function useAlertSettings() {
  const api = useApi();
  const organization = useOrganizationKey();
  return useQuery({
    queryKey: ["customer", "alert-settings", organization],
    queryFn: async () => unwrap(await api.GET("/api/customer/alert-settings")),
  });
}

/** Changes the thresholds of the customer's alerts; for admins. */
export function useChangeAlertSettings() {
  const api = useApi();
  const organization = useOrganizationKey();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (thresholds: AlertThresholds) =>
      unwrap(await api.PUT("/api/customer/alert-settings", { body: thresholds })),
    onSuccess: (settings) => {
      queries.setQueryData(["customer", "alert-settings", organization], settings);
    },
  });
}

/** Sends the customer's alerts to a receiver; for admins. */
export function useSetAlertReceiver(receiver: ReceiverKind) {
  const api = useApi();
  const organization = useOrganizationKey();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async (value: string) =>
      unwrap(
        await api.PUT("/api/customer/alert-settings/receivers/{receiver}", {
          params: { path: { receiver } },
          body: { value },
        }),
      ),
    onSuccess: (settings) => {
      queries.setQueryData(["customer", "alert-settings", organization], settings);
    },
  });
}

/** Stops sending the customer's alerts to a receiver; for admins. */
export function useRemoveAlertReceiver(receiver: ReceiverKind) {
  const api = useApi();
  const organization = useOrganizationKey();
  const queries = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE("/api/customer/alert-settings/receivers/{receiver}", {
          params: { path: { receiver } },
        }),
      ),
    onSuccess: (settings) => {
      queries.setQueryData(["customer", "alert-settings", organization], settings);
    },
  });
}
