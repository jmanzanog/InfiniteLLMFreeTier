# InfiniteLLM Gateway

InfiniteLLM Gateway is a high-performance, developer-friendly LLM API Proxy designed to aggregate and load-balance multiple "Free Tier" LLM providers (Groq, Cerebras, Mistral, OpenRouter, and **Gemini Native**). It exposes a single, OpenAI-compatible endpoint with automatic failover and streaming support.

## 🚀 Key Features

- **OpenAI Compatible**: Fully compatible with OpenAI SDKs and tools via `/v1/chat/completions`.
- **Native Gemini Support**: Adapts native Gemini API to OpenAI format seamlessly.
- **Intelligent Load Balancing**: Round-robin distribution across multiple providers.
- **Robust Failover**: Automatically retries the next available provider if one fails or returns a `429 Too Many Requests` or `5xx Server Error`.
- **Streaming Support**: Transparent proxying of Server-Sent Events (SSE) for real-time model responses.
- **Contract-First Development**: API types and server interfaces are automatically generated from an OpenAPI 3.0 specification.
- **Developer Ready**: Includes localized debugging configurations and a comprehensive verification suite.

## 🛠 Tech Stack

- **Language**: Go 1.25.5
- **Router**: [go-chi/chi](https://github.com/go-chi/chi)
- **Code Generation**: [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) (Strict Server mode)
- **CI/CD**: GitHub Actions with Security Auditing (Gosec, Govulncheck) & GHCR Publishing
- **Testing**: Native Go testing with high coverage requirements.

## 🏗 Architecture

The project follows a clean, modular architecture inspired by Domain-Driven Design (DDD):

- **`/api`**: Contains the OpenAPI specifications. `openai_proxy.yml` is the optimized version for generating Go types.
- **`/pkg/api`**: Auto-generated Boilderplate (Routing, JSON Decoding/Encoding). **Do not edit manually**.
- **`/pkg/balancer`**: Core logic for provider selection, round-robin state, and retry policies.
- **`/pkg/provider`**: Implementation of various LLM adapters (Groq, Mistral, Gemini, etc.).
- **`main.go`**: Implements the `StrictServerInterface`, orchestrates the bootstrap process, and handles the Reverse Proxy logic.

## 🚦 Getting Started

### Prerequisites

- Go 1.25.5 or higher.
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
```

### Running the Gateway

```bash
# Install dependencies
go mod tidy

# Run the server
go run main.go
```

The gateway will be available at `http://localhost:8080/v1/chat/completions`.

## 🐳 Docker

Build and run the containerized gateway using the optimized, scratch-based image:

```bash
# Build
docker build -t infinitellm .

# Run
docker run -p 8080:8080 --env-file .env infinitellm
```

The image is automatically built and published to **GitHub Container Registry (GHCR)** on every push to `main`.

## 🧪 Development Workflow

### Verification Suite

Run the full verification script (Format, Lint, Test):

```powershell
# Windows
.\scripts\verify.ps1
```

### Manual Testing

You can use `curl` to test the status:

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

## 🛡 Security & Quality

Every commit is validated against:
- **Linting**: `golangci-lint` (v2.7.2).
- **Vulnerabilities**: `govulncheck` and `gosec`.
- **Tests**: Race condition detection enabled.

## 🐞 Debugging

A VS Code `launch.json` is provided with:
1. **Debug InfiniteLLM Gateway**: Launches the app with `.env` loaded.
2. **Test Current Function**: Allows debugging a specific test function by selecting its name.

## 📄 License

This project is licensed under the MIT License.
