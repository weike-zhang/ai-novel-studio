# From a player action to a committed story

AI Novel Studio separates text generation from authority over stored state. A model proposes prose, choices, events and patches. The server decides whether those patches can become part of a journey.

## Frozen worlds and committed context

Starting a journey captures a world snapshot. Later edits to the public world do not rewrite that journey's premise. Context construction accepts the snapshot, committed simulation, committed summary and a bounded tail of committed turns.

[`BuildContinuityContext`](../services/server/internal/storyengine/continuity.go) rejects uncommitted turns, mismatched snapshots and duplicate sequence numbers. [Regression tests](../services/server/internal/storyengine/continuity_test.go) exercise those boundaries. The default recent-turn limit is eight; a bounded context is not a guarantee that a model remembers every plot detail.

## A proposal is not permission to mutate everything

[`TurnProposal`](../services/server/internal/storyengine/types.go) makes character patches, events, scenes and threads inspectable. [Validation](../services/server/internal/storyengine/validator.go) checks identity protection, version or previous-value preconditions, event requirements and scene constraints.

The [authorization layer](../services/server/internal/storyengine/authorization.go) also limits changes between authorized and later proposals. Its [tests](../services/server/internal/storyengine/authorization_test.go) cover added or rewritten character and thread patches.

Run the deterministic demonstration:

```sh
cd services/server
go run ./cmd/demo
```

The observable result includes:

```text
COMMITTED turn=1 status=committed
REJECTED code=identity_mutation field=characterPatches.0.current.name
PRESERVED turn=1 version=2 name=Keeper
```

The command also prints a fixed Chinese narrative. It invokes the real validator and asserts the stored state after rejection. It is a small behavioral example, not a benchmark of model accuracy.

## Durable work and transactional state

[`turnjobs.Store`](../services/server/internal/turnjobs/store.go) reserves credits and creates jobs inside a transaction. It checks idempotency keys and locks simulation and wallet rows. Successful completion writes the committed turn and settles the reserved credits.

[`messaging`](../services/server/internal/messaging/outbox.go) contains the outbox implementation; [`TurnJobHandler`](../services/server/internal/messaging/turn_job_handler.go) handles delivery and worker ownership. These mechanisms support retry handling, but should not be described as universal exactly-once delivery.

The [store integration tests](../services/server/internal/turnjobs/store_integration_test.go) require MySQL. Queue integration tests also need RabbitMQ. Their behavior is separate from the in-memory demo and must be verified against disposable middleware.

## Browser contracts

The [API client](../packages/api-client/README.md) uses same-origin cookie sessions, typed job states and SSE parsing. The UI can recover a job's authoritative state rather than treating an intermediate streamed event as a committed result.

The [journey job-state tests](../apps/web/app/journey/job-state.test.ts) and [client tests](../packages/api-client/test/client.test.ts) provide compact entry points for reviewing the frontend/backend boundary.

## Tradeoffs worth reviewing

- The modular monolith keeps state transitions near their SQL transactions. Splitting services would introduce additional consistency boundaries.
- Frozen snapshots make existing journeys stable, at the cost of not receiving later world edits automatically.
- Strict validation can reject a plausible continuation. A rejected proposal should produce a recoverable user experience rather than silently corrupting state.
- Model prose can still contradict the story semantically even when a typed patch is valid. The current checks do not prove long-run narrative consistency.
- The default fake provider is useful for repeatable development, but says nothing about the quality or cost of a real model.

## 中文代码导读

面试或代码评审可以沿一条具体问题阅读：如果模型把守钟人的姓名改掉，会发生什么？先运行演示，再看校验器如何拒绝身份变更，最后对照数据库路径如何检查版本、提交回合并结算额度。

这个案例能解释状态边界和工程取舍。并发容量、实际模型质量、线上用户数和真实支付都需要各自的证据，不能从代码规模或单元测试数量推断。
