# Executor Model

## The Problem With Generic Shell Execution

A GitOps tool that executes arbitrary shell commands is powerful, but it puts a large burden on the user: every task needs to be written defensively, idempotently, with its own check logic. There's no shared vocabulary, no schema validation, and the tool can't reason about what a task is trying to do.

The alternative — building deep support for specific tools (mise, systemd, apt) — produces a better experience but limits flexibility. Neither extreme is right on its own.

## Executors

Fluke's answer is the executor model. An executor is a named, typed block in a manifest that declares desired state in terms of a specific tool or capability. Each executor:

- Defines its own HCL schema (validated at parse time, not runtime)
- Implements a **check** operation — is desired state already satisfied?
- Implements an **apply** operation — bring the system to desired state

The agent core only knows about the executor interface. It calls `Check`, and if the result indicates drift, calls `Apply`. It doesn't know or care what the executor does internally.

This model is loosely analogous to Terraform's provider model: Fluke core is the engine, executors are providers, and HCL blocks in manifests are resource declarations validated against each executor's schema.

## The mise Executor and the Handoff Pattern

The `mise` executor deserves special attention because it inverts the usual pattern. Rather than Fluke encoding knowledge about how to deploy an application, the user defines `check` and `apply` tasks in a `.mise.toml` file and the executor simply calls them.

```
fluke agent
    │  calls
    ▼
mise run check    ← user-defined: "is the app running the right version?"
    │  if non-zero (drifted):
    ▼
mise run apply    ← user-defined: "pull, build, restart"
```

This means:

- Deployment logic lives in the application or infrastructure repository, not in Fluke manifests
- Tasks can be tested locally with `mise run` before Fluke ever calls them
- Fluke doesn't need to understand the deployment — it just needs to know whether to run it

The `working_dir` field is the handshake point. How that directory got there — manually maintained, git-cloned by another tool, or git-cloned by Fluke's optional `git` block — is entirely up to the user. Fluke makes no assumptions about repository ownership.

## Built-in Executors

Fluke ships with three executors:

| Executor | Purpose |
|----------|---------|
| `mise` | Delegates to user-defined mise tasks; preferred for most deployments |
| `systemd` | Manages systemd unit state and enablement |
| `shell` | Runs arbitrary shell commands; escape hatch for everything else |

`shell` is intentionally available but should be a last resort. It offers no schema, no local testability, and a broader security surface than purpose-built executors.

## Future: External Executors

The executor interface is designed to support external plugins in a future version. An external executor would be a separate binary that Fluke discovers and communicates with over a defined protocol — similar to how Terraform providers work as separate binaries communicating over gRPC.

For now, all executors are compiled into the Fluke binary. The interface is defined clearly in `internal/executor/executor.go` and should be kept stable so the eventual plugin boundary doesn't require breaking changes to existing executors.

When external executors are introduced, agents will report their available executors to the server during registration, enabling the server to make dispatch decisions based on what each host actually supports. This is not necessary while all executors are built-in, since the server already knows every agent theoretically supports all executors.

## The Check Requirement

Every executor — including `shell` — requires a check. Unconditional execution is not supported by design.

Without a check, the reconciliation loop becomes a periodic scheduler: Fluke would re-run the apply on every cycle, potentially every 60 seconds. For expensive operations (database migrations, full deployments), this would be harmful. The check is what makes the reconciliation loop safe to run continuously.

A missing check is always a configuration error.
