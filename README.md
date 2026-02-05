# InfiniteLLM Gateway

InfiniteLLM Gateway is a high-performance, developer-friendly LLM API proxy that aggregates and load-balances multiple free-tier LLM providers (Groq, Cerebras, Mistral, OpenRouter, and **Gemini Native**). It exposes a single OpenAI-compatible endpoint with automatic failover and streaming support.

## Key Features

- **OpenAI Compatible**: Fully compatible with OpenAI SDKs and tools via `/v1/chat/completions`.
- **Native Gemini Support**: Adapts the native Gemini API to the OpenAI format.
- **Intelligent Load Balancing**: Round-robin distribution across providers.
- **Robust Failover**: Automatically retries the next available provider if one fails or returns a `429 Too Many Requests` or `5xx Server Error`.
- **Streaming Support**: Transparent proxying of Server-Sent Events (SSE) for real-time model responses.
- **Contract-First Development**: API types and server interfaces are generated from an OpenAPI 3.0 specification.
- **Developer Ready**: Includes local debugging configurations and a full verification suite.

## Tech Stack

- **Language**: Go 1.25.6
- **Router**: [go-chi/chi](https://github.com/go-chi/chi)
- **Code Generation**: [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) (Strict Server mode)
- **CI/CD**: GitHub Actions with security auditing (Gosec, Govulncheck) and GHCR publishing
- **Testing**: Native Go tests with high coverage targets.

## Architecture

The project follows a clean, modular architecture inspired by Domain-Driven Design (DDD):

- **`/api`**: Contains the OpenAPI specifications. `openai_proxy.yml` is the optimized version for generating Go types.
- **`/pkg/api`**: Auto-generated boilerplate (routing, JSON decoding/encoding). **Do not edit manually**.
- **`/pkg/balancer`**: Core logic for provider selection, round-robin state, and retry policies.
- **`/pkg/handlers`**: HTTP handlers for health, JSON stats, and the web dashboard using Go's `embed` and `html/template`.
- **`/pkg/metrics`**: Asynchronous metrics collection with SQLite persistence for request stats.
- **`/pkg/provider`**: Implementation of various LLM adapters (Groq, Mistral, Gemini, etc.).
- **`main.go`**: Implements the `StrictServerInterface`, orchestrates the bootstrap process, and handles the reverse proxy logic.

## Getting Started

### Prerequisites

- Go 1.25.6 or higher.
- A `.env` file in the root directory.

### Environment Variables

Create a `.env` file with your provider keys:

```env
PORT=8080
GROQ_API_KEY=your_key
CEREBRAS_API_KEY=your_key
OPENROUTER_API_KEY=your_key
MISTRAL_API_KEY=your_key
GEMINI_API_KEY=your_key

# Optional Debug Flags
LOG_LLM_RESPONSE_DETAILS=true  # Log full upstream response body
FIXED_PROVIDER=Gemini          # Force routing to a specific provider

# Metrics (optional)
METRICS_DB_PATH=metrics.db     # SQLite database path for metrics persistence
METRICS_RETENTION_DAYS=30      # How many days to keep metrics (default: 30)
```

### Running the Gateway

```bash
# Install dependencies
go mod tidy

# Run the server
go run main.go
```

The gateway will be available at `http://localhost:8080/v1/chat/completions`.

### Health Endpoint

A `/health` endpoint is available for Kubernetes liveness/readiness probes:

```bash
curl http://localhost:8080/health
# Returns: {"status":"ok"}
```

### Stats and Dashboard

The gateway provides both raw JSON metrics and a visual dashboard:

#### 1. Visual Dashboard
Access a real-time dashboard at `http://localhost:8080/stats/web`.

- **Auto-Refresh**: Use the `refresh` query parameter (e.g., `/stats/web?refresh=5`) to automatically reload the dashboard every N seconds.
- **Rich UI**: Embedded templates provide fast, frame-free visualization of your gateway's health.

#### 2. JSON Metrics
A `/stats` endpoint provides aggregated metrics for programmatic access:

```bash
curl http://localhost:8080/stats
```

Returns statistics including:
- **Total requests**, successes, and failures.
- **Average, min, max response times** (in milliseconds).
- **Per-provider breakdown** with error counts (429, 5xx, 4xx).
- **Success rate** percentage and "Stats Since" timestamp.

### Response Headers

Every response from `/v1/chat/completions` includes:
- `X-Provider`: Name of the LLM provider that handled the request
- `X-Response-Time-Ms`: Response time in milliseconds

## Docker

Build and run the containerized gateway using the optimized, scratch-based image:

```bash
# Build
docker build -t infinitellm .

# Run
docker run -p 8080:8080 --env-file .env infinitellm
```

The image is automatically built and published to **GitHub Container Registry (GHCR)** on every push to `main`.

## Development Workflow

### Verification Suite

Run the full verification script (format, lint, test):

```powershell
# Windows
.\scripts\verify.ps1
```

### Manual Testing

Use `curl` to test the status:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-1.5-flash",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Generating API Code

If you modify `api/openai_proxy.yml`, regenerate the Go code using:

```bash
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
  -config oapi-config.yaml api/openai_proxy.yml
```

## Security and Quality

Every commit is validated against:
- **Linting**: `golangci-lint` (v2.7.2).
- **Vulnerabilities**: `govulncheck` and `gosec`.
- **Tests**: Race condition detection enabled.

## Debugging

A VS Code `launch.json` is provided with:
1. **Debug InfiniteLLM Gateway**: Launches the app with `.env` loaded.
2. **Test Current Function**: Allows debugging a specific test function by selecting its name.

## License

This project is licensed under the MIT License.
