# Architecture

## Overview

Fluke is a server/agent system. A central server tracks desired state from a Git repository and communicates reconciliation work to agents running on target hosts. Both roles are compiled into a single Go binary, selected at runtime by subcommand.

## Components

### Server

The server is the control plane. It:

- Polls a Git repository and parses `.fluke.hcl` manifests into a desired state model
- Maintains a registry of connected agents and their declared labels
- Matches agents to tasks using label selectors
- Coordinates the reconciliation cycle: request check → diff → dispatch apply if drifted
- Stores recent reconciliation events (in-memory by default, Redis optionally)
- Serves the web UI and responds to CLI commands

The server holds no durable state beyond the event store. If it restarts, desired state is reloaded from Git and agents rebuild their registrations on reconnect. See [Statelessness](statelessness.md).

### Agent

The agent runs persistently on each target host. It:

- Registers with the server on startup, providing its labels and name
- Receives check and apply instructions over a long-lived gRPC stream
- Runs executor checks to determine current state
- Executes apply operations when instructed
- Streams results back to the server

Agents do not read Git or parse HCL. All desired state arrives from the server.

### Single Binary

Mode is selected by subcommand:

```bash
fluke server --config /etc/fluke/server.hcl
fluke agent  --config /etc/fluke/agent.hcl
```

## Communication

Agents connect to the server over **gRPC** (HTTP/2) and hold a long-lived stream for receiving work and sending results. Authentication uses a **pre-shared token** presented in gRPC metadata on every call. See [Security model](security-model.md).

## Reconciliation Loop

```
Git repo
   │  poll / webhook
   ▼
Server
   │  parse HCL → desired state model
   │  match agents by label selector
   │  for each matched agent:
   │    → send check instruction
   │    ← receive check results
   │    → diff desired vs actual
   │    → if drifted: apply drift policy
   │      (remediate | remediate_and_alert | alert_only)
   │
   ▼  gRPC stream
Agent (on host)
   │  run executor checks
   │  run executor applies (if instructed)
   └  stream results back to server
```

1. The server detects a new commit (via polling or webhook) or a reconciliation interval elapses.
2. Manifests are parsed and the desired state model is updated.
3. For each connected agent, the server evaluates which tasks match by label selector.
4. The server sends check instructions to the agent; the agent runs each executor's check and returns results.
5. The server diffs the results against desired state.
6. Drifted executors are handled according to the configured drift policy.
7. If remediation is warranted, apply instructions are sent to the agent.
8. The agent executes applies in declaration order and streams results back.
9. The outcome is recorded in the event store.

## Label Targeting

Hosts are not targeted by hostname. Each agent declares key/value labels at startup. Manifests use `selector` blocks that match against those labels. This allows a single task to apply to any number of hosts without naming them explicitly, and makes it straightforward to add or remove hosts from a task by changing their labels.

## Design Constraints

- **No Kubernetes dependency.** Fluke has no K8s-specific code.
- **No secrets management.** Secrets are out of scope; inject them via environment variables or a dedicated secrets tool.
- **No host provisioning.** Fluke assumes the agent is already running. Initial bootstrapping is the user's responsibility.
- **Git is the only source of truth for desired state.** The server derives everything else at runtime.
