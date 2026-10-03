# AI 小说创作台 · AI Novel Studio

基于 Next.js 与 Go 的 AI 互动小说生成平台，支持世界设定、分支剧情与角色状态管理。

[English](README.md) · [架构与代码](docs/architecture.md) · [完整应用开发配置](docs/development.md)

AI 小说创作台是基于 Next.js 和 Go 的 AI 互动小说应用，包含世界创作、私人旅程与异步剧情生成。模型先提出剧情和状态变更，服务端检查后再写入数据库。

如果你也在做 AI 叙事游戏，可以重点看这里如何保护已经确定的事实：角色可以换地点，但模型不能通过状态补丁悄悄改掉角色姓名。

## 不接模型，也能复现一次状态校验

安装 Go 1.26 或更高版本后，在仓库根目录执行：

```sh
cd services/server
go run ./cmd/demo
```

演示先提交一个港口场景，再故意让模拟模型修改角色姓名。引擎拒绝修改，并检查旅程版本、回合数和角色姓名仍保持提交后的值。

它使用实际剧情引擎、内存仓库和固定输出的模拟 Provider，不需要 API Key、数据库或 Docker。首次运行 Go 可能下载模块元数据。这段演示验证状态校验行为，不评价真实模型的文笔或长期剧情一致性。

## 可以沿着代码看什么

| 关注点 | 具体实现 |
| --- | --- |
| 产品交互 | 世界发现、创作台、私人旅程、账号流程和任务进度 |
| 剧情连续性 | 冻结的世界快照、已提交回合、角色状态与场景校验 |
| 异步任务 | Go API、Outbox、RabbitMQ、独立 Worker 和数据库提交 |
| 接口与数据 | OpenAPI 客户端、MySQL 结构迁移和合成开发数据 |
| 模型接入 | OpenAI 兼容接口、可配置端点及失败回退 |

[架构文档](docs/architecture.md)解释这些选择，并链接到对应代码和测试。命令行演示采用内存提交路径；完整应用采用 Worker 和数据库路径。

## 本地开发

前端需要 Node.js 22 或更高版本，在仓库根目录运行：

```sh
npm ci
npm run build:api-client
npm run test:web
npm run test:api-client
npm run tokens:check
```

Go 检查：

```sh
cd services/server
go test ./...
```

完整应用还需要 MySQL、Redis、RabbitMQ 和自行配置的外部 MinIO。按[开发配置](docs/development.md)启动，并查看[验证记录](docs/verification.md)。数据库与真实模型集成测试需要显式传入测试环境变量，默认测试通过不等于这些集成已验证。

## 内容与能力边界

公开版保留合成开发世界，不包含线上世界目录、原封面图、真实用户数据或生产配置。计费相关代码不能视为已接通真实支付。项目尚未提供可复现的并发性能或模型质量基准。

欢迎提交能复现的连续性问题、校验器回归测试或开发文档改进。请用合成内容描述操作及前后状态，并移除凭据。参见[贡献说明](CONTRIBUTING.md)与[安全反馈](SECURITY.md)。如果这些实现对你的 AI 项目有帮助，欢迎 star，方便后来的人找到它。

## 许可

[MIT](LICENSE)。依赖继续适用各自许可，见[第三方说明](THIRD_PARTY_NOTICES.md)。
