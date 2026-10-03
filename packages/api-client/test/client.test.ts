import { describe, expect, expectTypeOf, it, vi } from "vitest";
import { ApiClient, ApiError, createIdempotencyKey } from "../src/index.js";
import type { AIJobEvent, CompletedWorldDraftJob } from "../src/index.js";

describe("ApiClient", () => {
  it("creates an idempotency key when randomUUID is unavailable", () => {
    const originalCrypto = globalThis.crypto;
    vi.stubGlobal("crypto", {
      getRandomValues: (bytes: Uint8Array) => {
        bytes.fill(0);
        return bytes;
      },
    } as Crypto);
    try {
      expect(createIdempotencyKey()).toBe("00000000-0000-4000-8000-000000000000");
    } finally {
      vi.stubGlobal("crypto", originalCrypto);
    }
  });

  it("binds the platform fetch to its global receiver", async () => {
    const receiver = globalThis;
    const originalFetch = globalThis.fetch;
    const platformFetch = vi.fn(function (this: unknown) {
      expect(this).toBe(receiver);
      return Promise.resolve(new Response(JSON.stringify({ worlds: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }));
    });
    globalThis.fetch = platformFetch as typeof globalThis.fetch;
    try {
      await new ApiClient().listWorlds();
      expect(platformFetch).toHaveBeenCalledOnce();
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("returns a typed world page and omits absent query parameters", async () => {
    const payload = {
      worlds: [
        {
          id: "world-1",
          ownerId: "user-1",
          authorNickname: "织雾者",
          title: "雾港",
          summary: "潮声里的港城",
          tags: ["悬疑"],
          simulationCount: 12,
          version: 3,
          updatedAt: "2026-08-14T00:00:00Z",
        },
      ],
      nextCursor: "next/page",
    };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ fetch: fetcher });

    const page = await client.listWorlds({ keyword: "雾 港", tags: ["悬疑", "海港"], limit: 12 });

    expect(page.worlds[0]?.title).toBe("雾港");
    expect(fetcher).toHaveBeenCalledWith(
      "/v1/worlds?keyword=%E9%9B%BE+%E6%B8%AF&tags=%E6%82%AC%E7%96%91%2C%E6%B5%B7%E6%B8%AF&limit=12",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("serializes every supported world query parameter", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ worlds: [] }), {
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ fetch: fetcher });

    await client.listWorlds({
      keyword: "迷雾",
      type: "mystery",
      tags: "悬疑,海港",
      sort: "popular",
      cursor: "cursor-1",
      limit: 50,
    });

    const url = new URL(String(fetcher.mock.calls[0]?.[0]), "https://example.test");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      keyword: "迷雾",
      type: "mystery",
      tags: "悬疑,海港",
      sort: "popular",
      cursor: "cursor-1",
      limit: "50",
    });
  });

  it("gets an encoded world detail path", async () => {
    const payload = {
      id: "world-1",
      ownerId: "user-1",
      authorNickname: "织雾者",
      title: "雾港",
      summary: "潮声里的港城",
      tags: [],
      simulationCount: 0,
      version: 1,
      updatedAt: "2026-08-14T00:00:00Z",
      visibility: "public",
      status: "published",
      setting: { premise: "潮汐改变记忆" },
    } as const;
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.getWorld("world/a")).resolves.toMatchObject({ title: "雾港" });
    expect(fetcher).toHaveBeenCalledWith("/v1/worlds/world%2Fa", expect.any(Object));
  });

  it("creates a journey through the simulations endpoint", async () => {
    const payload = {
      id: "simulation-1",
      userId: "user-1",
      worldId: "world-1",
      worldVersion: 2,
      worldSnapshot: { title: "雾港" },
      status: "active",
      version: 1,
      createdAt: "2026-08-14T00:00:00Z",
    };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        status: 201,
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.createJourney({ worldId: "world-1" })).resolves.toEqual(payload);
    expect(fetcher).toHaveBeenCalledWith(
      "/v1/simulations",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ worldId: "world-1" }) }),
    );
  });

  it("loads committed simulation state and history", async () => {
    const payload = {
      id: "simulation-1", worldId: "world-1", worldSnapshot: {}, state: {}, summary: "回响",
      version: 3, lastTurnSeq: 2, lastSceneNo: 2, characters: {}, threads: {},
      recentTurns: [{ id: "turn-2", simulationId: "simulation-1", seq: 2, sceneNo: 2, sceneTitle: "雨巷", sceneClosure: "收束。", interaction: "act", userAction: "继续", proposal: { narrative: "雾散开。", choices: ["前行"] }, status: "committed", createdAt: "2026-08-14T00:00:00Z" }],
    };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(payload), { headers: { "content-type": "application/json" } }));
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.getSimulation("simulation/a")).resolves.toEqual(payload);
    expect(fetcher).toHaveBeenCalledWith("/v1/simulations/simulation%2Fa", expect.any(Object));
  });

  it("creates, updates and publishes an authored world with versions", async () => {
    const payload = { id: "world-1", ownerId: "user-1", title: "雾港", summary: "简介", setting: {}, visibility: "private", status: "draft", version: 1, draftSource: "manual", createdAt: "2026-08-14T00:00:00Z", updatedAt: "2026-08-14T00:00:00Z" };
    const fetcher = vi.fn<typeof fetch>().mockImplementation(async () => new Response(JSON.stringify(payload), { headers: { "content-type": "application/json" } }));
    const client = new ApiClient({ fetch: fetcher });
    await client.createWorld({ title: "雾港", summary: "简介", setting: {}, source: "manual" });
    await client.updateWorld("world/a", { expectedVersion: 1, title: "雾港", summary: "新简介", setting: {} });
    await client.confirmAIWorldFields("world/a", { expectedVersion: 2, fields: ["title"] });
    await client.publishWorld("world/a", { expectedVersion: 2 });
    await client.unpublishWorld("world/a", { expectedVersion: 3 });

    expect(fetcher.mock.calls.map((call) => [call[0], call[1]?.method])).toEqual([
      ["/v1/worlds", "POST"],
      ["/v1/worlds/world%2Fa", "PATCH"],
      ["/v1/worlds/world%2Fa/confirm-ai-fields", "POST"],
      ["/v1/worlds/world%2Fa/publish", "POST"],
      ["/v1/worlds/world%2Fa/unpublish", "POST"],
    ]);
  });

  it("regenerates a world cover through the moderator endpoint", async () => {
    const payload = { queued: true };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(payload), { headers: { "content-type": "application/json" } }));
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.regenerateWorldCover("world/a")).resolves.toEqual(payload);
    expect(fetcher).toHaveBeenCalledWith("/v1/worlds/world%2Fa/cover/generate", expect.objectContaining({ method: "POST" }));
  });

  it("regenerates covers for all coverless worlds and manages featured flags", async () => {
    const queued = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ queued: 6 }), { headers: { "content-type": "application/json" } }));
    const featured = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ featured: true, rank: 4 }), { headers: { "content-type": "application/json" } }));
    const removed = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ featured: false }), { headers: { "content-type": "application/json" } }));

    const client = new ApiClient({ fetch: queued });
    await expect(client.regenerateAllCovers()).resolves.toEqual({ queued: 6 });
    expect(queued).toHaveBeenCalledWith("/v1/worlds/covers/regenerate-missing", expect.objectContaining({ method: "POST" }));

    const featuredClient = new ApiClient({ fetch: featured });
    await expect(featuredClient.setWorldFeatured("world/a")).resolves.toEqual({ featured: true, rank: 4 });
    expect(featured).toHaveBeenCalledWith("/v1/worlds/world%2Fa/featured", expect.objectContaining({ method: "POST" }));

    const removedClient = new ApiClient({ fetch: removed });
    await expect(removedClient.removeWorldFeatured("world/a")).resolves.toEqual({ featured: false });
    expect(removed).toHaveBeenCalledWith("/v1/worlds/world%2Fa/featured/remove", expect.objectContaining({ method: "POST" }));
  });

  it.each([
    [401, "http_401"],
    [404, "world_not_found"],
  ])("maps world HTTP %i responses to ApiError", async (status, expectedCode) => {
    const body = status === 404 ? { code: "world_not_found", message: "世界不存在" } : undefined;
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(body === undefined ? null : JSON.stringify(body), {
        status,
        statusText: status === 401 ? "Unauthorized" : "Not Found",
        headers: body === undefined ? undefined : { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ fetch: fetcher });

    const error = await (status === 401
      ? client.createJourney({ worldId: "world-1" })
      : client.getWorld("world-1")
    ).catch((reason: unknown) => reason);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status, code: expectedCode });
  });

  it("preserves the complete structured world error body", async () => {
    const body = {
      code: "invalid_world_query",
      message: "查询参数无效",
      request_id: "req-world-1",
      fields: { limit: "must be at most 50" },
    };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status: 400,
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.listWorlds({ limit: 51 })).rejects.toMatchObject({
      status: 400,
      code: "invalid_world_query",
      requestId: "req-world-1",
      body,
    });
  });

  it("maps structured HTTP failures to ApiError", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({ code: "job_conflict", message: "任务状态冲突", request_id: "req-42" }),
        { status: 409, headers: { "content-type": "application/json" } },
      ),
    );
    const client = new ApiClient({ fetch: fetcher });

    const error = await client.retryJob("job-1").catch((reason: unknown) => reason);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      status: 409,
      code: "job_conflict",
      message: "任务状态冲突",
      requestId: "req-42",
    });
  });

  it("maps network failures to the same ApiError type", async () => {
    const fetcher = vi.fn<typeof fetch>().mockRejectedValue(new TypeError("offline"));
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.getJob("job-1")).rejects.toMatchObject({
      status: 0,
      code: "network_error",
    });
  });

  it("sends the required idempotency header and JSON action", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ id: "job-1" }), {
        status: 202,
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ baseUrl: "/v1/", fetch: fetcher });
    const key = "turn-1234567890123456";

    await client.createTurnJob("sim/a", { action: "推开门" }, { idempotencyKey: key });

    expect(fetcher).toHaveBeenCalledWith(
      "/v1/simulations/sim%2Fa/turn-jobs",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        body: JSON.stringify({ action: "推开门" }),
        headers: expect.objectContaining({ "Idempotency-Key": key }),
      }),
    );
  });

  it("creates an asynchronous world draft with its idempotency key", async () => {
    const payload = {
      id: "018f4f6d-5b80-7e65-ae29-8f9f0abf4192",
      kind: "world_draft",
      status: "queued",
      attemptNo: 0,
      canCancel: true,
      canRetry: false,
      createdAt: "2026-08-15T00:00:00Z",
      updatedAt: "2026-08-15T00:00:00Z",
    };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        status: 202,
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient({ fetch: fetcher });
    const input = {
      topic: "潮湿海港中的信件悬疑",
      type: "都市奇幻",
      contentRating: "16+" as const,
      boundaries: "不出现现实伤害指导",
    };
    const key = "world-draft-1234567890";

    const job = await client.generateWorldDraft(input, { idempotencyKey: key });

    expect(job).toEqual(payload);
    expectTypeOf(job).toMatchTypeOf<{ kind: "world_draft" }>();
    expect(fetcher).toHaveBeenCalledWith(
      "/v1/worlds/generate-draft",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        body: JSON.stringify(input),
        headers: expect.objectContaining({ "Idempotency-Key": key }),
      }),
    );
  });

  it("types a completed world draft as private draft AI output only", () => {
    const job: CompletedWorldDraftJob = {
      id: "job-1",
      kind: "world_draft",
      status: "completed",
      attemptNo: 1,
      canCancel: false,
      canRetry: false,
      result: {
        world: {
          id: "world-1",
          ownerId: "user-1",
          title: "雾港来信",
          summary: "潮声中的失踪信件",
          setting: {},
          visibility: "private",
          status: "draft",
          version: 1,
          draftSource: "ai",
          createdAt: "2026-08-15T00:00:00Z",
          updatedAt: "2026-08-15T00:00:00Z",
        },
      },
      createdAt: "2026-08-15T00:00:00Z",
      updatedAt: "2026-08-15T00:00:01Z",
    };

    expectTypeOf(job.result.world.visibility).toEqualTypeOf<"private">();
    expectTypeOf(job.result.world.status).toEqualTypeOf<"draft">();
    expectTypeOf(job.result.world.draftSource).toEqualTypeOf<"ai">();

    if (false) {
      // @ts-expect-error Generated world draft results can never be public.
      const invalidVisibility: typeof job.result.world.visibility = "public";
      // @ts-expect-error Generated world draft results can never be published.
      const invalidStatus: typeof job.result.world.status = "published";
      // @ts-expect-error Generated world draft results can never claim a manual source.
      const invalidSource: typeof job.result.world.draftSource = "manual";
      void [invalidVisibility, invalidStatus, invalidSource];
    }
  });

  it("returns the discriminated task union from get, cancel and retry", async () => {
    const queued = { id: "job-1", kind: "turn", simulationId: "sim-1", status: "queued", attemptNo: 0, canCancel: true, canRetry: false, createdAt: "2026-08-15T00:00:00Z", updatedAt: "2026-08-15T00:00:00Z" };
    const cancelled = { ...queued, status: "cancelled", canCancel: false };
    const retrying = { ...queued, status: "queued", attemptNo: 1 };
    const fetcher = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(JSON.stringify(queued)))
      .mockResolvedValueOnce(new Response(JSON.stringify(cancelled), { status: 202 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(retrying), { status: 202 }));
    const client = new ApiClient({ fetch: fetcher });

    const snapshots = [
      await client.getJob("job/a"),
      await client.cancelJob("job/a"),
      await client.retryJob("job/a"),
    ];

    expect(snapshots.map(({ kind, status, attemptNo }) => ({ kind, status, attemptNo }))).toEqual([
      { kind: "turn", status: "queued", attemptNo: 0 },
      { kind: "turn", status: "cancelled", attemptNo: 0 },
      { kind: "turn", status: "queued", attemptNo: 1 },
    ]);
    expect(fetcher.mock.calls.map((call) => [call[0], call[1]?.method])).toEqual([
      ["/v1/jobs/job%2Fa", undefined],
      ["/v1/jobs/job%2Fa/cancel", "POST"],
      ["/v1/jobs/job%2Fa/retry", "POST"],
    ]);
  });

  it("submits a world report and returns the accepted report", async () => {
    const payload = { id: "report-1", worldId: "world-1", reporterId: "user-1", reason: "色情低俗", note: "章节内含违规描述", status: "pending", createdAt: "2026-08-15T00:00:00Z", updatedAt: "2026-08-15T00:00:00Z" };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(payload), { status: 201 }));
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.reportWorld("world-1", { reason: "色情低俗", note: "章节内含违规描述" })).resolves.toEqual(payload);
    expect(fetcher).toHaveBeenCalledWith("/v1/worlds/world-1/reports", expect.objectContaining({ method: "POST" }));
  });

  it("streams job events from the SSE endpoint", async () => {
    const body = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('id: job-1:queued:0\nevent: job.status\ndata: {"jobId":"job-1","kind":"turn","status":"queued","attemptNo":0,"canCancel":true}\n\n'));
        controller.enqueue(new TextEncoder().encode('id: job-1:completed:0\nevent: turn.committed\ndata: {"id":"turn-9","simulationId":"sim-1","seq":9,"userAction":"继续前行","proposal":{"narrative":"风停。","choices":["停下","折返"]}}\n\n'));
        controller.close();
      },
    });
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } }),
    );
    const client = new ApiClient({ fetch: fetcher });

    const events: AIJobEvent[] = [];
    await client.streamJobEvents("job-1", (event) => { events.push(event); });

    expect(fetcher).toHaveBeenCalledWith("/v1/jobs/job-1/events", expect.objectContaining({ method: "GET" }));
    expect(events.map((event) => [event.event, (event.data as { status?: string }).status ?? "completed"])).toEqual([
      ["job.status", "queued"],
      ["turn.committed", "completed"],
    ]);
  });

  it("rejects when the SSE endpoint is unavailable", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 404 }));
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.streamJobEvents("job-1", () => {})).rejects.toMatchObject({ status: 404 });
  });

  it("exports the discriminated SSE task event union", () => {
    const event: AIJobEvent = {
      event: "world_draft.completed",
      data: { jobId: "job-1", worldId: "world-1" },
      rawData: '{"jobId":"job-1","worldId":"world-1"}',
    };
    expect(event.event).toBe("world_draft.completed");
  });

  it("rejects an idempotency key outside the OpenAPI length bounds", () => {
    const client = new ApiClient({ fetch: vi.fn<typeof fetch>() });
    expect(() =>
      client.createTurnJob("sim-1", { action: "继续" }, { idempotencyKey: "short" }),
    ).toThrow(TypeError);
  });

  it("builds an encoded SSE endpoint URL", () => {
    const client = new ApiClient({ baseUrl: "https://example.test/v1/", fetch: vi.fn<typeof fetch>() });
    expect(client.getJobEventsUrl("job/a")).toBe("https://example.test/v1/jobs/job%2Fa/events");
  });

  it("returns the task snapshot after cancellation", async () => {
    const payload = { id: "job-1", kind: "turn", simulationId: "sim-1", status: "cancelled", attemptNo: 1, canCancel: false, canRetry: false, createdAt: "2026-08-15T00:00:00Z", updatedAt: "2026-08-15T00:00:01Z" };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(payload), { status: 202 }));
    const client = new ApiClient({ fetch: fetcher });

    await expect(client.cancelJob("job-1")).resolves.toEqual(payload);
  });
});
