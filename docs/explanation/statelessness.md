# Statelessness

## Design Intent

Fluke is intentionally designed to minimize the state it owns. Every piece of data Fluke holds is either derived from an authoritative external source at runtime, or is explicitly lossy operational data with a short horizon.

This is a deliberate tradeoff: less state means simpler operations, easier restarts, and no database to maintain.

## What Each Piece of State Is and Where It Lives

**Desired state** — lives in Git. Loaded on startup and on every poll cycle. If the server restarts, the full desired state is restored within one poll interval. Git is the only source of truth.

**Agent registry** — built from live connections. When an agent connects, it registers its name and labels. When the server restarts, agents reconnect and re-register within one heartbeat interval (typically seconds). There is nothing to restore.

**Label-to-task matching** — derived at runtime by crossing agent labels against manifest selectors. Fully recomputable from the agent registry and desired state.

**Event history** — the only genuinely lossy data. Recent reconciliation events (what ran, on which host, with what outcome) are held in a ring buffer or Redis. This is operational data, not system-of-record data. The long-term history of what Fluke has done belongs in your log aggregator and metrics stack, not in Fluke itself.

## What You Lose on Restart

When the server restarts with the default in-memory event store:

- Event history is cleared
- Drift status is unknown until the next reconciliation cycle completes (typically within `poll_interval`)
- The web UI shows no agents until they reconnect (typically within seconds)

For most deployments this is acceptable. The system converges to a correct state quickly and the history that matters is preserved in external logs.

## Extending Event History

If event history surviving restarts is important for your deployment, the Redis event store extends this without introducing a relational database dependency:

```hcl
server {
  event_store {
    backend = "redis"
    redis {
      url = "redis://localhost:6379"
      ttl = "168h"
    }
  }
}
```

Events expire via Redis TTL rather than explicit eviction. A TTL of 7–30 days covers typical operational review windows.

A Postgres backend is not in scope for v1 but the `EventStore` interface is designed to support additional backends. If you need Postgres, the interface is the right extension point.

## What Fluke Deliberately Does Not Own

- **Source of truth for application versions or deployment history** — use your deployment pipeline's records or your observability stack
- **Secrets** — use a dedicated secrets tool; Fluke does not encrypt or store secrets
- **Provisioning state** — Fluke assumes the agent is already running; initial host setup is out of scope
- **Container or image state** — Fluke is not a container scheduler
