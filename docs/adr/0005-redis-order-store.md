# ADR 0005: Shared Redis order store for multi-replica correctness

- Status: Accepted
- Date: 2026-11-15

## Context

The first implementation kept orders and idempotency records in a process-local
map. That is correct for a single process and for the unit tests, but it is
wrong as soon as the Deployment or Rollout runs more than one replica:

- a replay can land on a different pod and create a second order (HTTP 201
  instead of 200);
- `GET /v1/orders/{id}` can return 404 for an order created on another pod;
- customer listings only show orders written by the pod that serves the request.

The staging smoke test exposed this directly: staging runs two replicas and the
idempotent replay intermittently received 201. This is a data-consistency bug,
not a platform-layer problem, so weakening the test or pinning traffic to one pod
would hide it rather than fix it.

## Decision

Keep the `OrderStore` seam and add a Redis implementation used by the chart:

- `Create` runs a Lua script that atomically checks the idempotency record and
  writes the idempotency key, order JSON and customer sorted-set entry. A replay
  returns the original order ID from the same script; a different request hash
  returns `ErrIdempotencyConflict`.
- `Get`, `ListByCustomer` and a readiness `PING` read the shared instance.
- `REDIS_ADDR` selects Redis. When it is unset, the binary falls back to the
  in-memory store; unit tests and standalone runs stay dependency-free.
- The Helm chart deploys a single Redis instance per environment, pinned to a
  multi-arch image digest, with a read-only root filesystem, dropped
  capabilities, probes, resource limits, an emptyDir, and NetworkPolicies that
  only allow the checkout pods to reach port 6379.

## Consequences

- Reads, listings and idempotent replays are consistent across replicas, so the
  HPA, PDB and canary configuration can be used as designed.
- Redis is a single instance with emptyDir storage and no authentication. App
  pods can restart without losing data, but Redis or node loss loses orders and
  idempotency records. This is an explicit demo boundary, not a production
  durability claim.
- A real deployment should use a managed or operator-managed Redis with
  persistence, authentication/TLS, backups, and a PodDisruptionBudget; the Go
  client already accepts `REDIS_PASSWORD` and `REDIS_DB`.
- The in-memory store remains covered by unit tests including concurrent
  creates; Redis behavior is covered by the live staging smoke test and would
  merit a container-backed integration test in CI when a datastore fixture is
  acceptable.
