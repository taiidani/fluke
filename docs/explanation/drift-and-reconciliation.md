# Drift and Reconciliation

## What Is Drift?

Drift is the condition where a host's actual state no longer matches the desired state defined in Git. It can happen because:

- A task was never applied to a host (new task, new host joining the fleet)
- A check that previously passed now fails (a runtime was removed, a service crashed, a config file was overwritten)
- A previous apply failed partway through
- The manifest was updated with new desired state

Drift is detected by running each executor's check on the target host and comparing the result against what the manifest expects.

## The Reconciliation Cycle

Reconciliation runs on a continuous loop, triggered by:

- A new commit detected in the manifest repository (via polling or webhook)
- The passage of the configured `poll_interval` (default 60s)
- A manual `fluke reconcile` command

On each cycle, for each agent, the server:

1. Determines which tasks match the agent's labels
2. Sends check instructions to the agent
3. Receives check results
4. Identifies which executors are drifted
5. Applies the drift policy for each drifted task

## Drift Policies

Three policies are available, configurable as a server default and overridable per task:

**`remediate`** — apply silently. The most common default for stable infrastructure tasks where drift is expected and self-healing is the right behaviour.

**`remediate_and_alert`** — apply and send a webhook notification. Useful when you want an external record of what changed and when, without requiring human intervention before every change.

**`alert_only`** — notify but do not apply. For sensitive operations where a human should approve the change before it runs — certificate rotations, database migrations, anything where an automated apply could cause harm.

## Idempotency

The check/apply model assumes executors are idempotent: running apply multiple times should produce the same result as running it once. This is the user's responsibility for the `shell` executor and the `mise` executor's task implementations. Built-in executors (`systemd`) are idempotent by construction.

When writing mise tasks for Fluke:

- **Check should be fast and read-only.** It runs on every reconciliation cycle.
- **Apply should be safe to run more than once.** If it runs twice due to a transient failure, the result should be the same as running it once.
- **A successful apply should cause the next check to pass.** If your apply runs successfully but the check still fails, you have a bug in one of the two tasks.

## Manual Reconciliation

Manual reconciliation bypasses the drift policy — it always applies, even for `alert_only` tasks. Use it when you've reviewed a drift alert and want to trigger remediation explicitly.

```bash
fluke reconcile task <task-name>
fluke reconcile agent <agent-name>
fluke reconcile all
```
