# Manifest Reference

Desired state is expressed in HCL files stored in a Git repository. Any file matching `**/*.fluke.hcl` (configurable via `manifest_glob`) is loaded and merged into a single desired state model. Task names must be unique across all files.

## File Layout

Organize manifests however suits your team:

```
infra/
  web.fluke.hcl
  database.fluke.hcl
  shared.fluke.hcl
```

---

## `task`

The primary unit of desired state. A task describes one or more executor blocks that should be satisfied on all hosts matching its selector.

```hcl
task "<name>" {
  description = "optional human-readable description"

  drift {
    policy = "remediate"   # optional; overrides server default
  }

  selector { ... }

  # One or more executor blocks in declaration order:
  mise "<name>" { ... }
  shell "<name>" { ... }
  systemd "<name>" { ... }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `description` | No | Shown in web UI and CLI output |
| `drift` | No | Per-task drift policy override; see [Configure drift policy](../how-to/configure-drift-policy.md) |
| `selector` | Yes | Label selector to match target hosts |

---

## `selector`

Matches hosts by their declared labels. A host matches if **all** key/value pairs in `match_labels` are present in the host's label set.

```hcl
selector {
  match_labels = {
    role   = "web"
    env    = "production"
  }
}
```

---

## Executor Blocks

Executor blocks declare the desired state for a specific capability. Each executor defines its own HCL schema. Blocks are executed in declaration order within a task.

See [Executor reference](executors.md) for full schemas.

### `mise`

```hcl
mise "<name>" {
  working_dir = "/opt/app"       # required
  check_task  = "check"          # default: "check"
  apply_task  = "apply"          # default: "apply"

  git {                          # optional
    url    = "https://github.com/taiidani/fluke.git"
    branch = "main"
  }
}
```

### `systemd`

```hcl
systemd "<name>" {
  unit    = "app.service"   # required
  state   = "running"       # running | stopped
  enabled = true            # whether the unit should be enabled
}
```

### `shell`

```hcl
shell "<name>" {
  check       = "test -f /opt/app/.deployed"   # required
  command     = "/opt/app/deploy.sh"            # required
  working_dir = "/opt/app"
  run_as      = "deploy"
  env         = { APP_ENV = "production" }
  on_failure  = "abort"                         # abort | continue
}
```

---

## HCL Features

Manifests support the full HCL expression syntax, including locals and variable interpolation:

```hcl
locals {
  node_version = "22"
  app_dir      = "/opt/app"
}

task "runtimes" {
  selector {
    match_labels = { role = "web" }
  }

  mise "node" {
    working_dir = local.app_dir
    check_task  = "check-node-${local.node_version}"
    apply_task  = "install-node-${local.node_version}"
  }
}
```
