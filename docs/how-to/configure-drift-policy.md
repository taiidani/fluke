# Configure Drift Policy

Drift occurs when a host's actual state no longer matches the desired state in Git. Fluke detects drift by running each executor's check on the target host and offers three policies for handling it.

## Policies

| Policy | What happens |
|--------|-------------|
| `remediate` | Apply silently. No external notification. |
| `remediate_and_alert` | Apply and send a webhook alert. |
| `alert_only` | Send a webhook alert. Do not apply. Requires manual remediation. |

## Setting the Server Default

```hcl
server {
  drift {
    policy        = "remediate_and_alert"
    alert_webhook = "https://hooks.slack.com/services/xxx/yyy/zzz"
  }
}
```

`alert_webhook` is required for `remediate_and_alert` and `alert_only`. It is unused for `remediate`.

## Overriding Per Task

Individual tasks can override the server default:

```hcl
task "rotate_certificates" {
  description = "Certificate rotation — requires manual approval before applying"

  drift {
    policy = "alert_only"
  }

  selector {
    match_labels = { role = "web" }
  }

  shell "renew" {
    check   = "openssl x509 -in /etc/app/tls/cert.pem -checkend 2592000 -noout"
    command = "certbot renew --cert-name app.example.com"
  }
}
```

## Triggering Manual Remediation

When a task is configured with `alert_only`, or when you want to force a re-run regardless of policy:

```bash
# Remediate a specific task across all matched hosts
fluke reconcile task <task-name>

# Remediate all tasks on a specific agent
fluke reconcile agent <agent-name>

# Full reconciliation pass across all agents
fluke reconcile all
```

Manual remediation is also available from the agent detail page in the web UI.

## Webhook Payload

Fluke POSTs JSON to the configured webhook URL when drift is detected:

```json
{
  "event": "drift_detected",
  "timestamp": "2025-03-13T14:22:01Z",
  "agent": {
    "name": "web-01",
    "labels": { "role": "web", "env": "production" }
  },
  "task": "rotate_certificates",
  "policy": "alert_only",
  "remediation_status": "pending_manual"
}
```

For `remediate_and_alert`, a second event is sent once remediation completes:

```json
{
  "event": "drift_remediated",
  "timestamp": "2025-03-13T14:22:08Z",
  "agent": { "name": "web-01" },
  "task": "rotate_certificates",
  "outcome": "success"
}
```
