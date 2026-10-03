# @ai-novel-studio/api-client

AI 小说创作台浏览器端的零运行时依赖 API 基础客户端。接口路径和约束以
`api/openapi.yaml` 为准。

客户端仅使用同源 Cookie 会话，不接受或保存 API Key、Bearer Token 等密钥。
`Idempotency-Key` 只是写请求去重标识，可由 `createIdempotencyKey()` 创建。

```ts
import { ApiClient, createIdempotencyKey } from "@ai-novel-studio/api-client";

const api = new ApiClient();
const worlds = await api.listWorlds({ sort: "recommended", tags: ["悬疑"] });
const world = await api.getWorld(worldId);
const draftJob = await api.generateWorldDraft(
  {
    topic: "潮湿海港中的信件悬疑",
    type: "都市奇幻",
    contentRating: "16+",
    boundaries: "不出现现实伤害指导",
  },
  { idempotencyKey: createIdempotencyKey() },
);

// 页面刷新后以服务端任务快照为准；本机只需保留 draftJob.id 作为恢复指针。
const draftSnapshot = await api.getJob(draftJob.id);
if (draftSnapshot.kind === "world_draft" && draftSnapshot.status === "completed") {
  // 类型在这里固定为 private / draft / ai，生成任务没有自动发布权限。
  const privateWorld = draftSnapshot.result.world;
}

const journey = await api.createJourney({ worldId: world.id });
const job = await api.createTurnJob(
  simulationId,
  { action: "推开那扇门" },
  { idempotencyKey: createIdempotencyKey() },
);

const eventsUrl = api.getJobEventsUrl(jobId);
```

`generateWorldDraft()`、`createTurnJob()`、`getJob()`、`cancelJob()` 和 `retryJob()`
返回带 `kind` 与 `status` 判别字段的任务联合类型。世界草稿在排队、生成、校验、保存、
失败或取消阶段都没有 `result`；只有 `completed` 分支包含已经提交的私密 AI 草稿。

`getJobEventsUrl()` 返回同源 SSE 地址。SSE 可使用 `parseSseEvent` 解析单个事件，或使用
`createSseParser` 处理分块文本流；事件载荷类型导出为 `AIJobEvent`。生成过程中只处理
`job.status`，收到 `world_draft.completed` 后再调用 `getJob()` 读取完整结果，不从中间事件
恢复未经校验的世界字段。
