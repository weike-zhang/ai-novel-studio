# Development setup

## Smallest runnable example

Go 1.26+ is enough for the in-memory story demonstration:

```sh
cd services/server
go run ./cmd/demo
go test ./internal/storyengine -v
```

Neither command loads `.env` or contacts an AI provider. Use this path to inspect validation before configuring the full application.

## Full application prerequisites

- Node.js 22+ and Go 1.26+.
- MySQL 8.4, Redis and RabbitMQ. The development Compose file supplies these services.
- Your own external MinIO server and bucket credentials. The API rejects loopback MinIO endpoints. The example configuration contains no hosted infrastructure credentials.
- A model endpoint is optional for fake worker turns, but AI authoring assistance and cover generation require configured compatible endpoints. Those operations are not covered by the fake worker setting.

The full container stack and middleware integrations must be tested on your environment. The initial local review did not have Docker available.

## Start development services

From the repository root:

```sh
cp .env.example .env
npm ci
docker compose -f deploy/compose/compose.yaml up -d
```

Fill in `MINIO_ENDPOINT`, `MINIO_PUBLIC_URL`, `MINIO_ACCESS_KEY` and `MINIO_SECRET_KEY` in `.env`. Keep development services on a trusted local machine. The Compose credentials are public synthetic development values.

The helper below loads `.env` with Node's built-in parser, without executing its contents as shell code:

```sh
node --env-file=.env scripts/run-server.mjs migrate up
docker compose -f deploy/compose/compose.yaml exec -T mysql \
  mysql -unovelstudio -pnovelstudio novelstudio < services/server/db/seeds/development.sql
```

Run each process in its own terminal, from the repository root:

```sh
node --env-file=.env scripts/run-server.mjs api
node --env-file=.env scripts/run-server.mjs worker
node --env-file=.env --run dev:web
```

Open `http://localhost:3000`. The sample uses a synthetic development identity for local requests. Publicly exposed services must not use that identity shortcut, the fake provider, synthetic moderation or echoed verification codes.

The public edition omits hosted catalog migrations and retains schema migrations plus synthetic development seeds. Use a new development database; this edition is not an upgrade package for the hosted service's production database.

## Model configuration

Set `MODEL_PROVIDER=openai_compatible`; the accepted provider names are defined in [`cmd/worker/main.go`](../services/server/cmd/worker/main.go). Supply `TEST_MODEL_BASE_URL`, `TEST_MODEL_API_KEY` and `TEST_MODEL_NAME`. These legacy variable names also configure real endpoints; do not place a production key in shared examples.

Cover generation uses `COVER_BASE_URL`, `COVER_API_KEY` and `COVER_MODEL`, with fallback behavior implemented in the API. Provider API compatibility, supported model names, image formats and usage charges depend on your provider.

## Production review

The [Docker files](../deploy/docker/docker-compose.yml) are a deployment starting point. Set independent authentication secrets, SMTP and Turnstile credentials, real moderation, correct cookie settings and an image host for your own media domain before exposing registration. Build the web image after configuration or frontend changes; editing source alone does not update a running container.

Use secure cookies behind HTTPS. The local HTTP example sets `SESSION_COOKIE_SECURE=false` only for development.

## Verification

```sh
npm run check:api-client
npm run test:api-client
npm run build:api-client
npm run test:web
npm run tokens:check
npm run lint:web
npm run build:web
```

Run `go test ./...` from `services/server`. Tests with `MYSQL_TEST_DSN`, `REDIS_TEST_ADDR`, `RABBITMQ_TEST_URL` or live-model settings use external systems. Point them at disposable test resources. The [CI workflow](../.github/workflows/ci.yml) defines middleware-backed checks; its presence alone is not a passing CI result.
