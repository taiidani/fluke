# Fluke

**GitOps-based continuous delivery for VMs and bare metal.**

Fluke is a single-binary GitOps CD tool for long-lived servers. It follows the same reconciliation-loop model popularized by Flux and Argo CD, without any dependency on Kubernetes.

```
Git repo (desired state)
       ↓  poll / webhook
    fluke server
       ↓  gRPC
    fluke agent   ←→   mise, systemd, shell...
   (on each host)
```

## Key Features

- **Single binary** — server and agent modes in one binary, selected by flag
- **HCL manifests** — expressive desired state with per-executor schemas
- **Pluggable executors** — mise, systemd, shell built-in; designed for external plugins later
- **Label-based targeting** — match hosts by labels, not hostnames
- **Configurable drift handling** — remediate silently, alert-and-remediate, or alert-only
- **Intentionally stateless** — in-memory event history by default, Redis opt-in

## Quick Start

See [Getting Started](docs/tutorials/getting-started.md).

## Documentation

Docs follow the [Diátaxis](https://diataxis.fr) structure:

- **Tutorials** — learning-oriented, follow along to get something working
  - [Getting Started](docs/tutorials/getting-started.md)
- **How-to guides** — goal-oriented, how to accomplish a specific thing
  - [Configure TLS](docs/how-to/configure-tls.md)
  - [Use the mise executor](docs/how-to/use-the-mise-executor.md)
  - [Use the shell executor](docs/how-to/use-the-shell-executor.md)
  - [Configure drift policy](docs/how-to/configure-drift-policy.md)
  - [Enable Redis event store](docs/how-to/enable-redis-event-store.md)
- **Reference** — information-oriented, precise technical detail
  - [Manifest reference](docs/reference/manifest.md)
  - [Configuration reference](docs/reference/configuration.md)
  - [Executor reference](docs/reference/executors.md)
  - [CLI reference](docs/reference/cli.md)
- **Explanation** — understanding-oriented, design rationale and concepts
  - [Architecture](docs/explanation/architecture.md)
  - [Executor model](docs/explanation/executor-model.md)
  - [Drift and reconciliation](docs/explanation/drift-and-reconciliation.md)
  - [Statelessness](docs/explanation/statelessness.md)
  - [Security model](docs/explanation/security-model.md)

## Status

Fluke is in early development. The manifest format and gRPC API are unstable until v1.0.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). AI coding agents should read [AGENTS.md](AGENTS.md) first.

## License

Apache 2.0
