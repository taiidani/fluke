# Use the mise Executor

The mise executor is the recommended way to define deployment tasks in Fluke. Rather than encoding deployment logic in the manifest, you define `check` and `apply` tasks in a `.mise.toml` file and point Fluke at them. This keeps your deployment logic in your application or infrastructure repository, testable locally with `mise run`, and independent of Fluke's manifest schema.

For a full explanation of why this model was chosen, see [Executor model](../explanation/executor-model.md).

## The Convention

The mise executor calls two mise tasks:

- **check task** — must exit 0 if desired state is already satisfied, non-zero if not
- **apply task** — brings the system to desired state; only called when the check exits non-zero

Both tasks must exist. A missing task is a configuration error and the executor will fail with a clear message indicating which task name it attempted to call.

## Basic Usage

```hcl
mise "deploy" {
  working_dir = "/opt/app"
}
```

With no further configuration, Fluke calls `mise run check` and `mise run apply` in `/opt/app`.

The corresponding `.mise.toml`:

```toml
[tasks.check]
run = "systemctl is-active app && curl -sf http://localhost:3000/health"

[tasks.apply]
run = """
git pull origin main
npm ci --production
systemctl restart app
"""
```

## Custom Task Names

If your project uses a different naming convention, override the task names:

```hcl
mise "deploy" {
  working_dir = "/opt/app"
  check_task  = "fluke-check"
  apply_task  = "fluke-apply"
}
```

## Automatically Syncing a Git Repository

If the working directory is managed by Fluke rather than manually or by another tool, add a `git` block. When present, Fluke pulls the repository before running the check task on every reconciliation cycle.

```hcl
mise "deploy" {
  working_dir = "/opt/app"

  git {
    url    = "https://github.com/taiidani/fluke.git"
    branch = "main"
  }
}
```

The `git` block is entirely optional. If your working directory is managed manually, by another process, or already kept up to date some other way, omit it and Fluke will not touch the directory's git state.

## Using Environment Variables in Tasks

Environment variables available to the agent process are inherited by mise tasks. You can also set task-specific variables in `.mise.toml`:

```toml
[tasks.apply]
env = { NODE_ENV = "production" }
run = "npm ci && systemctl restart app"
```

## Testing Locally

Because the executor just calls `mise run`, you can test your tasks locally before connecting Fluke:

```bash
cd /opt/app
mise run check   # Should exit 0 if already deployed, non-zero if not
mise run apply   # Should bring the system to desired state
mise run check   # Should exit 0 after a successful apply
```

If this sequence works locally, it will work under Fluke.
