# Contributing Guide

This document provides standardized guidance for project contributors to ensure consistency and high quality across the codebase.

## How to Contribute

We welcome contributions via Issues and Pull Requests!

1. Fork this repository.
2. Create a feature branch (`git checkout -b feature/amazing-feature`).
3. Commit your changes (`git commit -m 'feat: add some amazing feature'`).
4. Push to the branch (`git push origin feature/amazing-feature`).
5. Open a Pull Request.

## Versioning

This project follows **Semantic Versioning 2.0.0**. The version format is `MAJOR.MINOR.PATCH`:

- **MAJOR**: Incompatible API changes.
- **MINOR**: Backwards-compatible new functionality.
- **PATCH**: Backwards-compatible bug fixes.

## Release process

Maintain versions and release notes only in the root `VERSION` and `CHANGELOG.md`. Automated publishing is currently paused; the [release guide](release.md) owns restoration requirements and operator procedures. Follow the [development checks](development.md#common-verification-commands) before submitting changes.

## Coding Standards

### Design Principles

The project strictly follows these software engineering principles:

1. **KISS (Keep It Simple, Stupid)**: Pursue simplicity in code and design. Prefer the most straightforward solution.
2. **DRY (Don't Repeat Yourself)**: Eliminate duplicate code, extract shared functions, and unify similar implementations.
3. **YAGNI (You Aren't Gonna Need It)**: Only implement features that are currently needed. Remove unused code and dependencies.
4. **Functional-first**: Prefer `map`, `reduce`, `filter`, and immutable data operations where applicable.

### Code Quality

**Go Backend (`backend-go/`)**:
- Format with `go fmt`
- Lint with `golangci-lint`
- Use `[Component-Action]` log format; no emoji in logs
- Implement proper error handling and logging

**Vue Frontend (`frontend/`)**:
- Use strict TypeScript; avoid `any` types
- All functions must have explicit type declarations
- Follow Prettier config (2-space indent, single quotes, no semicolons, 120-char width, LF EOL)

### File Naming

**Go Backend**:
- **Files**: `snake_case` (e.g., `channel_scheduler.go`)
- **Types/Interfaces**: `PascalCase` (e.g., `Provider`)
- **Functions**: `PascalCase` (exported) / `camelCase` (private)
- **Constants**: `PascalCase` or `SCREAMING_SNAKE_CASE`

**Vue Frontend**:
- **Files**: `kebab-case` (e.g., `api-service.ts`)
- **Vue Components**: `PascalCase` (e.g., `ChannelStatusBadge.vue`)
- **Types/Interfaces**: `PascalCase`
- **Functions**: `camelCase` (e.g., `getNextApiKey`)
- **Constants**: `SCREAMING_SNAKE_CASE` (e.g., `DEFAULT_CONFIG`)

## Testing

### Development Testing

Before submitting code, ensure:

**Go Backend**:
- Run tests: `cd backend-go && make test`
- Run linter: `cd backend-go && make lint`
- Format code: `cd backend-go && make fmt`

**Vue Frontend**:
- Run build verification: `cd frontend && bun run build`

**Integration**:
- Smoke test via health check endpoint (`GET http://localhost:3000/health`)
- For UI changes, include a brief test plan and screenshots/GIF in the Pull Request

### Commit & Pull Request Guidelines

- **Conventional Commits**: Use `conventional-commits` format, e.g., `feat:`, `fix:`, `refactor:`, `chore:`.
    - Examples: `feat(frontend): add ESC to close modal`, `fix(backend): redact Authorization header`.
- **PR Content**: Pull Requests must include:
    - Purpose description
    - Related Issue (if any)
    - Detailed test steps
    - Configuration / environment variable changes
    - Screenshots/GIF for UI changes

## Security

- **Never commit secrets**: Do not commit keys or sensitive configuration to version control. Use `.env` files and `backend-go/.config/config.json`.
- **Access keys**: `PROXY_ACCESS_KEY` is required for proxy access. Avoid logging full API keys.

## Agent-Specific Notes

- Keep code diffs minimal and consistent with existing code style.
- Update relevant documentation when behavior changes.
- Avoid unnecessary renames or large-scale refactoring.
