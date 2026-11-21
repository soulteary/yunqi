# Development Guide

This document explains how to set up Yunqi for local development, common commands, and the verification workflow.

> Related docs:
> - Architecture: [architecture.md](architecture.md)
> - Environment variables: [environment.md](environment.md)
> - Release process: [release.md](release.md)

## Environment Setup

### Prerequisites

| Tool | Minimum Version | Install (macOS) | Install (Linux) | Notes |
| --- | --- | --- | --- | --- |
| **Go** | 1.26 | `brew install go` | [go.dev/dl](https://go.dev/dl/) | Backend compilation and runtime |
| **Bun** | 1.x | `brew install oven-sh/bun/bun` | `curl -fsSL https://bun.sh/install \| bash` | Frontend dependency management and build |
| **Git** | - | Comes with Xcode CLT | `apt install git` / `yum install git` | Version control |
| **Make** | - | Comes with Xcode CLT | Usually pre-installed | Build scripts |

macOS users who do not have Xcode Command Line Tools yet, run:

```bash
xcode-select --install
```

### Install All Dependencies at Once

The root Makefile provides `make install` to automatically install frontend dependencies, Go modules, and development tools (Air hot-reload):

```bash
make install
```

### Verify Environment

```bash
go version     # should be >= 1.26
bun --version  # should be >= 1.x
make help      # confirm available commands
```

## Recommended Development Methods

| Method | Use Case | Description |
| --- | --- | --- |
| Root Makefile | Full-stack development | Starts frontend dev server and backend hot-reload simultaneously |
| backend-go Makefile | Backend-only development | Runs Go backend commands only |
| Frontend Bun | Frontend-only development | Runs Vue/Vite frontend commands only |
| Docker | Production-like verification | Not recommended as the primary hot-reload development method |

## Method 1: Root Directory (Recommended)

The root `Makefile` is the source of truth for full-stack development.

```bash
make help
make dev
make run
make frontend-dev
make build
make clean
```

Details:
- `make dev`: Starts the frontend dev server and runs the Go backend with hot-reload in `backend-go/`
- `make run`: Builds the frontend then runs the backend
- `make build`: Builds the frontend and compiles the Go backend
- `make frontend-dev`: Starts only the frontend dev server

## Method 2: backend-go Directory

The `backend-go/Makefile` is the source of truth for backend commands.

```bash
cd "backend-go"
make help
make dev
make run
make build
make test
make test-cover
make fmt
make lint
make deps
```

Details:
- `make dev`: Uses Air for hot-reload
- `make run`: Validates/builds and embeds the frontend through the shared content cache, then runs the backend
- `make build`: Validates/builds and embeds the frontend, then builds the production binary to the root `dist/` directory
- `make test`: Runs all Go tests
- `make test-cover`: Generates a coverage report

## Method 3: frontend Directory

Frontend scripts follow `frontend/package.json`. Bun is the preferred package manager.

```bash
cd "frontend"
bun install
bun run dev
bun run build
bun run preview
bun run type-check
bun run lint
```

Use Bun for frontend installs and upgrades and commit `frontend/bun.lock`; a pnpm compatibility lock is no longer maintained. The documentation site is a separate npm project: install with `cd docs && npm ci` and commit its own `package-lock.json`. Do not generate a root npm lockfile.

## Windows Environment Tips

If `make` is not available, use Go and Bun commands directly:

```powershell
cd backend-go
air
go test ./...
go fmt ./...

cd ../frontend
bun install
bun run dev
```

Recommended installations:
- Go
- Bun
- Make
- Git

## File changes and reloads

- `make dev` uses backend `.air.toml`, watching Go, templates, HTML, YAML, TOML and `.env`. Saves trigger a rebuild after a one-second delay and may restart the development process.
- `stop_on_error=true` means a failed rebuild does not guarantee that the old process remains running. Hot reload is not a zero-downtime deployment mechanism.
- Air excludes backend temporary files, embedded frontend files and build output. The frontend development server handles Vue and style updates separately.
- Channel configuration supports hot reload. Environment changes for a regular service binary require an operator-managed restart; `.env` is watched in Air mode.
- Automated collaborators must not kill or restart existing services. Use an isolated checkout for verification when source edits could trigger watchers.

If reloads stop working, check the Air executable, working directory, watch/exclude settings and build logs. Identify the owner of a conflicting port and let the operator decide how to handle it; do not force-kill processes or recursively change repository permissions.

## Linting and formatting

`frontend/eslint.config.js` uses Flat Config and Vue/TypeScript recommended rules, with formatting conflicts disabled by `eslint-config-prettier`. ESLint does not replace `vue-tsc` type checking.

```bash
(cd frontend && bun run lint)
(cd frontend && bun run lint:fix)    # Intentionally applies source fixes
(cd frontend && bun run format)      # Intentionally formats source files
(cd frontend && bun run type-check)  # Also included in the production build
```

The configuration file defines the actual rules. Avoid unrelated repository-wide automatic fixes or formatting during troubleshooting.

## Common verification commands

Run from the repository root. Subshells keep later commands in the correct directory:

```bash
make build
(cd backend-go && make test && go vet ./...)
(cd frontend && bun run lint && bun run test)
(cd docs && npm ci && npm run build)
```

`make build` already checks frontend types, builds the production frontend and embeds it into the Go service. Select checks appropriate to the changed modules; do not repeat unchanged builds without a new failure or concern. CI runs the Node script regression tests and checks all shared generated consumers (frontend presets, backend embedded JSON, published shards, indexes and checksums). Regenerate with `node scripts/generate-preset-manifest.mjs`; use `--check` for read-only validation.

## Local Access Points

- Web admin UI: `http://localhost:3688`
- Proxy API: `http://localhost:3688/v1`
- Health check: `http://localhost:3688/health`
- Frontend dev server: `http://localhost:5173` by default

### Windows / WSL / Docker Access Tips

On Windows, cmd, PowerShell, WSL, and Docker may be in different network environments. `localhost` / `127.0.0.1` may not reach the Yunqi process. For cross-environment access, use the Windows host LAN IPv4 address, for example:

```text
http://192.168.1.23:3688/v1
```

Get the address:

```powershell
ipconfig
```

Verify connectivity:

```powershell
curl.exe -i http://192.168.1.23:3688/health
```

When `BIND_HOST` is empty, the Yunqi backend listens on `:PORT` by default, which means all network interfaces. You generally do not need to change it to `0.0.0.0`. Set `BIND_HOST=127.0.0.1` for local-only access. If the LAN IP is not accessible, first check whether the Windows firewall allows inbound traffic on the corresponding port.

## Common Development Tasks

### Backend Only

```bash
cd "backend-go"
make dev
```

### Frontend Only

```bash
cd "frontend"
bun install
bun run dev
```

### Full-Stack Development

```bash
make dev
```

### Verify Production Build

```bash
make build
```

### Keep compatibility learning isolated in tests

Protocol handler tests use an in-memory shared compatibility cache. Persistence tests must explicitly use a path under `t.TempDir()`. Do not create or commit `.config/channel_compat.json` beneath source packages; it is runtime state, not a fixture.
