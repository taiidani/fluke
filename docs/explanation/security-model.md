# Security Model

## Trust Boundaries

Fluke has two trust boundaries:

1. **Between agent and server** — authenticated by pre-shared token over TLS
2. **Between agent and host** — the agent runs as a Unix user and inherits that user's permissions

## Agent Authentication

Agents authenticate with the server using a pre-shared token presented in gRPC metadata on every request. The server validates the token against its `agent_tokens` list.

Tokens are opaque strings with no built-in identity or permission model — any valid token grants the same access level. To differentiate token sources (e.g. production vs staging agents), use separate tokens and manage them accordingly.

**Token rotation:** add the new token to `agent_tokens`, update the agent config, restart the agent, then remove the old token.

**Generation:** `openssl rand -hex 32`

Never commit tokens to Git. Use environment variable interpolation in config files:

```hcl
agent {
  token = "$FLUKE_AGENT_TOKEN"
}
```

## The Real Threat Model

The most important security consideration for Fluke is not the agent-to-server channel — it's this:

> **Anyone with write access to the manifest repository can execute arbitrary commands on enrolled hosts.**

This is inherent to any GitOps tool. The manifest repository is a privileged system. Protect it accordingly:

- Use branch protection on the tracked branch
- Require pull request reviews before merging
- Limit who can approve and merge to the tracked branch
- Audit repository access regularly

The executor model provides some mitigation: purpose-built executors (`mise`, `systemd`) have bounded schemas and can't run arbitrary code. The `shell` executor can, and its use should be reviewed carefully in manifest PRs.

## Agent Execution Permissions

The agent executes commands as the Unix user running the agent process. Principle of least privilege applies:

- Run the agent as a dedicated non-root user with only the permissions required by your tasks
- Use `run_as` in `shell` executor blocks to scope specific commands to specific users
- Use targeted sudo rules rather than broad sudo access

Example sudo configuration for an agent user that needs to manage a service:

```
# /etc/sudoers.d/fluke-agent
fluke-agent ALL=(ALL) NOPASSWD: /bin/systemctl restart app
fluke-agent ALL=(ALL) NOPASSWD: /bin/systemctl start app
fluke-agent ALL=(ALL) NOPASSWD: /bin/systemctl stop app
```

## Transport Security

TLS is required for any non-local deployment. Without TLS, tokens and task output traverse the network in plaintext.

The gRPC server does not need to be internet-facing — agents initiate outbound connections to the server, so the server only needs to be reachable from agent hosts.

The HTTP server (web UI) should be restricted to trusted networks or placed behind a reverse proxy with authentication, as it exposes operational information about your infrastructure.

See [Configure TLS](../how-to/configure-tls.md).

## Secrets

Fluke does not manage secrets. Do not put secrets in HCL manifests — they will be committed to your Git repository and distributed to agents.

Secrets that tasks need at runtime should be pre-placed on target hosts by a dedicated secrets tool (Vault, SOPS, cloud provider secret managers) and referenced via environment variables or files already present on the host.
