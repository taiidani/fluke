# Getting Started

This tutorial walks you through installing Fluke, running a server and agent, and applying your first manifest. By the end you'll have a working reconciliation loop on a single host.

## Prerequisites

- Two machines: one to run the Fluke server (your control node), one to run the agent (your target host). These can be the same machine for local testing.
- A Git repository to store your manifests. A local bare repo is fine for this tutorial.
- Go 1.22+ if building from source.
- `mise` installed on the target host if you want to follow the mise executor example.

## Step 1: Install Fluke

On both machines:

```bash
git clone https://github.com/taiidani/fluke.git
cd fluke
go build -o fluke ./cmd/fluke
sudo mv fluke /usr/local/bin/fluke
fluke version
```

## Step 2: Configure the Server

Create a server config. For this tutorial we'll disable TLS to keep things simple — see [Configure TLS](../how-to/configure-tls.md) before deploying to production.

```hcl
# /etc/fluke/server.hcl

server {
  listen_grpc = ":7070"
  listen_http = ":7071"

  agent_tokens = ["dev-token-change-me"]

  git {
    url           = "/path/to/your/manifest-repo.git"
    branch        = "main"
    poll_interval = "30s"
  }

  tls {
    enabled = false   # development only
  }

  event_store {
    backend = "memory"
  }
}
```

Start the server:

```bash
fluke server --config /etc/fluke/server.hcl
```

Navigate to `http://localhost:7071` — you should see the dashboard with no agents connected.

## Step 3: Configure the Agent

On your target host, create an agent config:

```hcl
# /etc/fluke/agent.hcl

agent {
  server_url = "grpc://your-control-node:7070"
  token      = "dev-token-change-me"

  labels = {
    role = "web"
    env  = "dev"
  }
}
```

The `labels` block is how this host identifies itself. Manifests use label selectors to target hosts — you'll see this in the next step.

Start the agent:

```bash
fluke agent --config /etc/fluke/agent.hcl
```

The agent will register with the server. Refresh the web UI — your host should appear.

## Step 4: Write a Manifest

In your manifest repository, create a file named `web.fluke.hcl`:

```hcl
# web.fluke.hcl

task "install_runtimes" {
  description = "Install Node.js 22 via mise"

  selector {
    match_labels = {
      role = "web"
    }
  }

  mise "node" {
    working_dir = "/home/deploy/app"
    check_task  = "check"
    apply_task  = "apply"
  }
}
```

This task targets any host with the label `role = "web"` — which matches the agent you just configured.

On the target host, create a minimal `.mise.toml` at `/home/deploy/app/.mise.toml`:

```toml
[tasks.check]
run = "node --version 2>/dev/null | grep -q 'v22'"

[tasks.apply]
run = "mise use --global node@22"
```

Commit and push the manifest to your repository. Within one poll interval the server will pick it up, match it to your agent, and execute the task.

## Step 5: Verify

```bash
# Check agent status and recent events
fluke get agents
fluke get events

# Or check the web UI at http://localhost:7071
```

You should see the task listed as either satisfied (if Node 22 was already installed) or successfully applied.

## Next Steps

- [Use the mise executor](../how-to/use-the-mise-executor.md) — full options including git-managed working directories
- [Configure drift policy](../how-to/configure-drift-policy.md) — control what happens when state drifts
- [Configure TLS](../how-to/configure-tls.md) — required before any non-local deployment
- [Manifest reference](../reference/manifest.md) — full HCL syntax
