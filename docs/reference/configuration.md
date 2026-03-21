# Configuration Reference

Both server and agent are configured via HCL files passed with `--config`.

Environment variables are used only as token fallbacks:
- `agent.token`: if empty or whitespace in HCL, falls back to `FLUKE_TOKEN`.
- `server.agent_tokens`: if the HCL list has no non-empty entries after trimming, falls back to `FLUKE_AGENT_TOKENS` (comma-separated, trimmed, empty entries ignored).

There is no generic `$VAR` / `${VAR}` interpolation in other config fields.

---

## Server

```hcl
server {
  listen_grpc  = ":7070"
  listen_http  = ":7071"
  agent_tokens = ["token-a", "token-b"]

  git { ... }
  tls { ... }
  drift { ... }
  event_store { ... }
  log { ... }
}
```

### `server`

| Field | Default | Description |
|-------|---------|-------------|
| `listen_grpc` | `":7070"` | Address for the gRPC server |
| `listen_http` | `":7071"` | Address for the web UI and HTTP API |
| `agent_tokens` | — | **Required.** List of accepted agent tokens. If all entries are empty/whitespace, uses `FLUKE_AGENT_TOKENS` CSV fallback |

### `server.git`

| Field | Default | Description |
|-------|---------|-------------|
| `url` | — | **Required.** Repository URL |
| `branch` | `"main"` | Branch to track |
| `poll_interval` | `"60s"` | How often to poll for changes |
| `manifest_glob` | `"**/*.fluke.hcl"` | Glob to find manifest files |
| `ssh_key_file` | — | SSH private key for private repos |
| `basic_auth_user` | — | Username for HTTPS basic auth |
| `basic_auth_password` | — | Password for HTTPS basic auth |

### `server.tls`

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `true` | Set `false` for local development only |
| `cert_file` | — | Path to TLS certificate |
| `key_file` | — | Path to TLS private key |

### `server.drift`

| Field | Default | Description |
|-------|---------|-------------|
| `policy` | `"remediate"` | Default drift policy: `remediate`, `remediate_and_alert`, or `alert_only` |
| `alert_webhook` | — | Webhook URL; required for alert policies |

### `server.event_store`

| Field | Default | Description |
|-------|---------|-------------|
| `backend` | `"memory"` | `memory` or `redis` |

**`memory` sub-block:**

| Field | Default | Description |
|-------|---------|-------------|
| `max_events_per_agent` | `100` | Ring buffer size per agent |

**`redis` sub-block:**

| Field | Default | Description |
|-------|---------|-------------|
| `url` | — | **Required.** Redis connection URL |
| `prefix` | `"fluke"` | Key namespace prefix |
| `ttl` | `"168h"` | Event expiry duration |

### `server.log`

| Field | Default | Description |
|-------|---------|-------------|
| `level` | `"info"` | `debug`, `info`, `warn`, or `error` |
| `format` | `"text"` | `text` or `json` |

---

## Agent

```hcl
agent {
  server_url = "grpcs://fluke.internal:7070"
  token      = "agent-token"
  name       = "web-01"

  labels = {
    role = "web"
    env  = "production"
  }

  tls { ... }
  execution { ... }
  log { ... }
}
```

### `agent`

| Field | Default | Description |
|-------|---------|-------------|
| `server_url` | — | **Required.** `grpcs://` for TLS, `grpc://` for plaintext |
| `token` | — | **Required.** Pre-shared token matching a server `agent_tokens` entry. If empty/whitespace, uses `FLUKE_TOKEN` fallback |
| `name` | system hostname | Display name in UI and CLI |
| `labels` | `{}` | Key/value labels used to match this host against task selectors |

### `agent.tls`

| Field | Default | Description |
|-------|---------|-------------|
| `ca_file` | — | CA certificate for verifying the server; required for self-signed certs |
| `insecure_skip_verify` | `false` | Disable cert verification. Development only. |

### `agent.execution`

| Field | Default | Description |
|-------|---------|-------------|
| `default_shell` | `"/bin/bash"` | Shell used for `shell` executor commands and checks |
| `command_timeout` | `"5m"` | Maximum duration for a single command before it is killed |

### `agent.log`

Same fields as `server.log`.
