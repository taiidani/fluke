# Contributing

## Development Setup

### Prerequisites

- Go 1.22+
- `protoc` and `protoc-gen-go` for regenerating gRPC definitions
- `mise` (recommended for managing Go and tool versions)

### Clone and Build

```bash
git clone https://github.com/taiidani/fluke.git
cd fluke
go build ./...
go test ./...
```

### Running Locally

For development, TLS can be disabled. Start server and agent in separate terminals:

```bash
# Terminal 1
fluke server --config dev/server.hcl

# Terminal 2
fluke agent --config dev/agent.hcl
```

Sample development configs are in `dev/`.

## Project Structure

```
cmd/
  fluke/              # Binary entrypoint; mode dispatch (server vs agent)
internal/
  server/             # Server: Git polling, agent registry, dispatch
  agent/              # Agent: executor orchestration, gRPC client
  executor/           # Built-in executors (mise, systemd, shell)
  manifest/           # HCL parsing and desired state model
  proto/              # gRPC service definitions and generated code
  reconcile/          # Diff logic; drives Check → Apply cycle
  events/             # Event store interface and implementations (memory, Redis)
pkg/
  config/             # HCL config parsing for server and agent
  labels/             # Label selector evaluation
web/                  # Embedded web UI assets
docs/                 # Documentation (Diátaxis structure)
dev/                  # Development configs and fixtures
```

## Executors

Each executor lives in `internal/executor/` and implements the `Executor` interface defined in `internal/executor/executor.go`. Before adding or modifying an executor, read [docs/explanation/executor-model.md](docs/explanation/executor-model.md).

## Protobuf / gRPC

When modifying `.proto` files in `internal/proto/`, regenerate Go bindings and commit them alongside your changes:

```bash
protoc --go_out=. --go-grpc_out=. internal/proto/*.proto
```

## Pull Requests

1. Branch from `main`
2. Write tests for new behaviour
3. Run `go test ./...` and `go vet ./...`
4. Update relevant docs in `docs/` if the change affects user-facing behaviour
5. Open a PR with a clear description of what changed and why

## License

By contributing, you agree your contributions will be licensed under Apache 2.0.
