# Enable Redis Event Store

By default Fluke keeps event history in memory. Events are lost when the server restarts, which is acceptable for most single-server deployments where logs and metrics are the long-term record.

If you need event history to survive server restarts, Redis is available as an opt-in backend.

## Configuration

```hcl
server {
  event_store {
    backend = "redis"

    redis {
      url    = "redis://localhost:6379"
      prefix = "fluke"     # namespace for all Fluke keys
      ttl    = "168h"      # events expire after 7 days
    }
  }
}
```

Events are expired by Redis TTL rather than by Fluke. Adjust `ttl` to match your operational needs. Since event history is lossy operational data rather than a system of record, a TTL of 7–30 days is typical.

## When to Use Redis

Redis is most useful when:

- You want event history to survive server restarts
- You are planning a future move to multiple server instances (HA)

Redis does not change what Fluke considers authoritative. Git remains the source of truth for desired state. Agent labels and registration state are always rebuilt from live connections. Redis stores only the event log.

## Default: In-Memory

If no `event_store` block is configured, Fluke uses the in-memory backend with a default ring buffer of 100 events per agent:

```hcl
server {
  event_store {
    backend = "memory"

    memory {
      max_events_per_agent = 100
    }
  }
}
```
