# Architecture

Execution is a single-bounded-context service: it owns the task queue of each service order and the
progress of each work. It has no synchronous dependency on other services.

## Packages
| Package | Responsibility |
|---|---|
| `internal/work` | Work status state machine (no I/O) |
| `internal/task` | Task aggregate, command handlers (`EnqueueExecution`, `CancelExecution`), REST operations, controller, message payloads, `Repository` port |
| `internal/task/repository` | `Postgres` and `InMemory` implementations of the port |
| `internal/app` | HTTP server, bootstrap (API + outbox relay + SQS consumer), routing, middlewares |
| `internal/pkg/messaging` | Envelope, outbox `Enqueue`, relay, consumer (dedupe), SNS and SQS adapters |

## Transactions
- A command handler runs inside the consumer transaction (dedupe row + state + history + outbox rows, one commit).
- A REST mutation opens its own transaction in the service: find the task, change the aggregate, `Save`,
  enqueue the events, commit. A domain error rolls everything back.
- Repositories take `db.Executor`, so they run on a `*sql.Tx` or a `*sql.DB`.

## Domain purity
`task` and `work` do not import Gin, `lib/pq` or the messaging package. The aggregate records the
`work.Change` list; the service turns it into events.
