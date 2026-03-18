# CLI Reference

## Global Flags

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to config file (required for `server` and `agent` commands) |
| `--help` | Show help |
| `--version` | Show version |

---

## `fluke server`

Start the Fluke server.

```bash
fluke server --config /etc/fluke/server.hcl
```

---

## `fluke agent`

Start the Fluke agent.

```bash
fluke agent --config /etc/fluke/agent.hcl
```

---

## `fluke get`

List resources.

### `fluke get agents`

List all connected agents, their labels, and current status.

```bash
fluke get agents
```

### `fluke get drift`

Show current drift status across all agents.

```bash
fluke get drift
```

### `fluke get events`

Show recent reconciliation events.

```bash
fluke get events [--agent <name>] [--limit <n>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--agent` | all | Filter events by agent name |
| `--limit` | `20` | Number of events to show |

---

## `fluke describe`

Show detailed information about a specific resource.

### `fluke describe agent <name>`

```bash
fluke describe agent web-01
```

Shows labels, connected tasks, last reconciliation result per task, and recent events.

### `fluke describe task <name>`

```bash
fluke describe task deploy_app
```

Shows the task definition, matched agents, and per-agent reconciliation status.

---

## `fluke reconcile`

Trigger manual reconciliation.

```bash
# All agents
fluke reconcile all

# Specific agent
fluke reconcile agent <name>

# Specific task across all matched agents
fluke reconcile task <name>
```

Manual reconciliation runs regardless of the configured drift policy — it will apply even if the task policy is `alert_only`.

---

## `fluke version`

Print the Fluke version and build info.

```bash
fluke version
```
