# Verification record

Local checks on 2026-10-03 used macOS arm64, Node.js 22.23.2 and Go 1.26.4. They were run against the renamed AI Novel Studio public candidate, not the hosted service.

| Check | Observed result |
| --- | --- |
| `go run ./cmd/demo` | A valid turn committed; an identity mutation was rejected; the committed state remained unchanged |
| `go test ./...` | Passed with external integration settings absent; dependency downloads used the Go proxy configured by the project's Docker build |
| `npm run check:api-client` and `build:api-client` | Passed |
| `npm run test:api-client` | 30 tests passed |
| `npm run test:web` | 23 tests passed |
| `npm run tokens:check` and `lint:web` | Passed |
| `npm run build:web` | Passed with Next.js 16.3.8 |
| Local Markdown links | 40 local Markdown links passed |
| `npm audit --omit=dev` | Zero known runtime dependency findings at review time |

Known development dependency findings remain in the ESLint glob dependency chain and Vitest mocker. The compatible dependency update does not resolve those findings. A proposed Vitest major update could not be installed because its dependency range could not be resolved from the registry; the tested version is retained. Do not expose a development or test server to untrusted users.

The README's rendered HTML was inspected at desktop and 390px widths with no horizontal overflow. Screenshot capture timed out, so this is a rendered layout check, not a full visual review or a check of the public GitHub page.

Not verified in this local run: a clean Docker deployment, middleware integration, real-model quality, live payments, production throughput and hosted user behavior. Tests requiring middleware or a live model are conditional and should not be counted as passed integrations when their environment settings are absent.
