# Executor Reference

Executors are the built-in units of work in Fluke. Each executor defines its own HCL block schema and implements two operations: **check** (is desired state already satisfied?) and **apply** (bring the system to desired state).

For the design rationale behind the executor model, see [Executor model](../explanation/executor-model.md).

---

## `mise`

Delegates check and apply to user-defined mise tasks. The recommended executor for most use cases.

```hcl
mise "<n>" {
  working_dir = "/opt/app"
  check_task  = "check"
  apply_task  = "apply"

  git {
    url    = "https://github.com/taiidani/fluke.git"
    branch = "main"
  }
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `working_dir` | Yes | — | Directory where mise tasks are run; must contain a `.mise.toml` |
| `check_task` | No | `"check"` | Name of the mise task to run as the check |
| `apply_task` | No | `"apply"` | Name of the mise task to run as the apply |
| `git` | No | — | If present, the repository is pulled before every check |

**`git` sub-block:**

| Field | Required | Description |
|-------|----------|-------------|
| `url` | Yes | Repository URL |
| `branch` | No | Branch to track; defaults to `"main"` |

**Behaviour:**

- If a `git` block is present, the repository is pulled (or cloned if absent) before the check task runs.
- Both `check_task` and `apply_task` must exist in `.mise.toml`. A missing task is a runtime error with a message indicating the task name that was attempted.
- The check task must exit 0 to indicate satisfied state, non-zero to indicate drift.
- The apply task is only called when the check exits non-zero.

---

## `systemd`

Manages systemd units on the target host.

```hcl
systemd "<n>" {
  unit    = "app.service"
  state   = "running"
  enabled = true
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `unit` | Yes | — | Systemd unit name, including suffix (e.g. `app.service`) |
| `state` | No | `"running"` | Desired active state: `running` or `stopped` |
| `enabled` | No | `true` | Whether the unit should be enabled (start on boot) |

**Behaviour:**

- Check queries the unit's active state and enabled state via `systemctl is-active` and `systemctl is-enabled`.
- Apply calls `systemctl start`/`stop` and `systemctl enable`/`disable` as needed.
- Requires the agent to have permission to manage systemd units, either by running as root or via sudo configuration.

---

## `shell`

Runs arbitrary shell commands. The escape hatch executor for cases where no purpose-built executor fits. Prefer `mise` or `systemd` when possible.

```hcl
shell "<n>" {
  check       = "test -f /opt/app/.deployed"
  command     = "/opt/app/deploy.sh"
  working_dir = "/opt/app"
  run_as      = "deploy"
  env = {
    APP_ENV = "production"
  }
  on_failure = "abort"
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `check` | Yes | — | Shell expression; exit 0 = satisfied, non-zero = needs to run |
| `command` | Yes | — | Shell command to execute when check exits non-zero |
| `working_dir` | No | agent home dir | Working directory for check and command |
| `run_as` | No | — | Unix user to run as; requires appropriate sudo permissions |
| `env` | No | — | Additional environment variables |
| `on_failure` | No | `"abort"` | `abort` stops the task; `continue` proceeds to the next executor block |

**Behaviour:**

- Both `check` and `command` are run via `default_shell` (configured in the agent; defaults to `/bin/bash`).
- Environment variables set on the agent process are inherited unless overridden by `env`.
- A missing `check` field is a configuration error. Unconditional execution is not supported; see [Statelessness](../explanation/statelessness.md).
