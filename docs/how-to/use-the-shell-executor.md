# Use the Shell Executor

The shell executor runs arbitrary shell commands directly on the target host. It is the escape hatch for situations where no purpose-built executor fits.

Prefer the mise executor when possible. The shell executor offers no schema validation, no local testability guarantees, and a broader execution surface. It is intentionally available — some tasks genuinely require it — but its use should be considered carefully.

## Basic Usage

```hcl
shell "migrate_db" {
  check   = "psql $DATABASE_URL -tAc 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1' | grep -qx '20250313001'"
  command = "psql $DATABASE_URL < /opt/app/migrations/20250313001_add_users.sql"
}
```

`check` is required. A shell executor block without a check will fail at runtime. See [Statelessness](../explanation/statelessness.md) for why unconditional execution is not supported.

## Options

```hcl
shell "example" {
  check       = "test -f /opt/app/.deployed"
  command     = "/opt/app/deploy.sh"
  working_dir = "/opt/app"
  run_as      = "deploy"
  env = {
    APP_ENV = "production"
  }
  on_failure  = "abort"   # abort (default) | continue
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `check` | Yes | Shell expression; exit 0 = satisfied, non-zero = needs to run |
| `command` | Yes | Shell command to execute when check exits non-zero |
| `working_dir` | No | Working directory for both check and command |
| `run_as` | No | Unix user to run as; requires appropriate sudo permissions for the agent user |
| `env` | No | Additional environment variables |
| `on_failure` | No | `abort` stops the task; `continue` proceeds to the next executor block |

## Ordering Within a Task

Multiple executor blocks within a task run in declaration order. A shell block can be combined with other executors:

```hcl
task "deploy" {
  selector {
    match_labels = { role = "web" }
  }

  mise "runtimes" {
    working_dir = "/opt/app"
  }

  shell "seed_config" {
    check   = "test -f /etc/app/config.json"
    command = "cp /opt/app/config.example.json /etc/app/config.json"
    run_as  = "root"
  }
}
```
