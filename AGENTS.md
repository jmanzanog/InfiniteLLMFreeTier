# AI Agent Context & Guidelines

This document provides context, architectural guidelines, and workflows for AI agents (Antigravity, OpenCodeInterpreter, Cursor, etc.) working on the **InfiniteLLMFreeTier** codebase.

## 1. Project Overview
**InfiniteLLMFreeTier** is a high-availability Load Balancer and Gateway for LLM providers (Groq, OpenAI, Mistral, etc.). It aggregates multiple free/paid tiers into a unified OpenAI-compatible API.

### Key Capabilities
- **Provider Balancing**: Round-robin/Least-connection distribution (`pkg/balancer`).
- **Web Automation**: Headless browser capability via Playwright to interface with web-only chat interfaces (`pkg/provider/chatgpt_web`).
- **Metrics**: SQLite-based request tracking (`pkg/metrics`).
- **API**: OpenAPI 3.0 spec-driven development (`pkg/api`).

## 2. Tech Stack & Constraints
- **Language**: Go 1.25+ (Use latest stable features).
- **Core Libs**:
  - `playwright-go`: For browser automation.
  - `oapi-codegen`: For API generation.
  - `mattn/go-sqlite3`: For built-in metrics storage.
- **Testing**:
  - **Dockerized Tests**: All tests run inside Docker to ensure environment consistency.
  - **Coverage**: **100% Code Coverage is ENFORCED** for non-generated code. Verify scripts will fail if <100%.

## 3. Codebase Map

| Path | Description | Rules |
|------|-------------|-------|
| `pkg/api/openapi_generated.go` | Generated API server code. | **DO NOT EDIT MANUALLY.** |
| `pkg/balancer` | Load balancing logic. | Thread-safety is critical. Use mutexes. |
| `pkg/provider` | LLM Provider implementations. | Implement `Provider` interface. Use `//go:build` tags for heavy deps. |
| `pkg/provider/chatgpt_web*` | Playwright implementation. | Strict separation between headless/stub versions. |
| `scripts/` | CI/CD and local verification. | Use `verify.ps1` (Win) or `verify.sh` (Linux) as source of truth. |
| `main.go` | Entry point. | Keep minimal. Wire dependencies here. |

## 4. Workflows & Commands ("Source of Truth")

### Verification (The "Definition of Done")
Before confirming a task is complete, you **MUST** pass the verification script. It runs `fmt`, `lint`, `vulncheck`, and `tests` with coverage enforcement.

**CRITICAL RULE FOR TESTING:**
Never run `go test` directly. ALWAYS use the verification script appropriate for the OS. These scripts handle Docker environments, coverage profiles, and exclusions correctly.

**Windows (PowerShell):**
```powershell
./scripts/verify.ps1
```

**Linux/WSL/Mac (Bash):**
```bash
./scripts/verify.sh
```

### Generation
If you modify `api/openapi.yaml`, run:
```bash
oapi-codegen -config oapi-config.yaml api/openapi.yaml
```

## 5. Coding Standards for Agents

### Go Specifics
1.  **Error Handling**: Wrap errors: `fmt.Errorf("doing x: %w", err)`.
2.  **Concurrency**: Use `sync.RWMutex` for reads/writes on maps (e.g., in `balancer`).
3.  **Interfaces**: Define interfaces where they are used (consumer side), unless it's a core domain contract.
4.  **Comments**: Avoid unnecessary comments; prefer clear, descriptive names for variables and functions.

### Agent Behavior Rules
1.  **Check existing tools**: Before writing a new script, check `scripts/`.
2.  **Safe Refactors**: When refactoring `pkg/provider`, ensure both the *real* implementation and the *test stub* are updated.
3.  **Deps**: If adding a generic dependency, prefer standard lib if possible. If adding a hefty one, discuss first.

## 6. Known Pitfalls
- **Playwright Installation**: Running tests locally requires Playwright drivers. The `verify` scripts handle this via Docker. Prefer Docker execution.
- **SQLite Locking**: `pkg/metrics` uses SQLite. Ensure connection pooling/mutexes are respected to avoid `database is locked`.
- **Generated Code**: `pkg/api/*_generated.go` changes will be overwritten. Modify logic in `pkg/handlers` or `pkg/server` instead.

## 7. Verification Notes
- **Docker requirement**: `scripts/verify.ps1` runs tests inside Docker and requires the daemon to be running.
- **Coverage artifacts**: verification generates `coverage.out` and a filtered `coverage.filtered.out` (excluding generated files) for review.
- **Linux/WSL support**: if `scripts/verify.sh` is missing, create it or adjust this doc to match the available verification script.
