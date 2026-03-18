---
layout: home

hero:
  name: "Fluke"
  text: "GitOps-based continuous delivery for VMs and bare metal"
  tagline: A single-binary reconciliation-loop CD tool for long-lived servers. No Kubernetes required.
  actions:
    - theme: brand
      text: Getting Started
      link: /tutorials/getting-started
    - theme: alt
      text: Reference
      link: /reference/

features:
  - icon: 🎓
    title: New to Fluke?
    details: Follow the tutorial to get a server, agent, and your first manifest running.
    link: /tutorials/getting-started
    linkText: Getting Started
  - icon: 📖
    title: Looking for something specific?
    details: Step-by-step guides for common tasks.
    link: /how-to/
    linkText: How-to Guides
  - icon: 📄
    title: Need exact syntax?
    details: Complete reference for manifests, config, executors, and CLI.
    link: /reference/
    linkText: Reference
  - icon: 💡
    title: Want to understand the design?
    details: Architecture, executor model, and the reasoning behind key decisions.
    link: /explanation/
    linkText: Explanation
---

## How It Works

```
Git repo (desired state)
       ↓  poll / webhook
    fluke server
       ↓  gRPC
    fluke agent   ←→   mise, systemd, shell...
   (on each host)
```

## Key Features

- **Single binary** — server and agent modes in one binary, selected by flag
- **HCL manifests** — expressive desired state with per-executor schemas
- **Pluggable executors** — mise, systemd, shell built-in; designed for external plugins later
- **Label-based targeting** — match hosts by labels, not hostnames
- **Configurable drift handling** — remediate silently, alert-and-remediate, or alert-only
- **Intentionally stateless** — in-memory event history by default, Redis opt-in
