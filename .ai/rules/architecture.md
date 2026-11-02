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
The domain files (`internal/work/work.go`, `internal/task/task.go`, `internal/task/messages.go`,
`internal/task/repository.go`) import no Gin, `lib/pq` or messaging package. The aggregate records the `work.Change`
list; `internal/task/service.go` (application layer) turns it into events through `messaging`. `lib/pq` is imported
only by `internal/task/repository/postgres.go`; `net/http` only by `internal/task/controller.go`.
