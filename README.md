# Auto Repair Shop — Execution Service

Service that owns the mechanic work of a service order: the execution queue, the status of each work and the
finish of the task. Go, Gin, PostgreSQL (database `execution`), SNS/SQS messaging with the outbox pattern.

- Port: `8084` · Namespace `auto-repair-shop` · Kubernetes name `execution-service`
- Swagger: `docs/swagger.yaml` (served at `/swagger/index.html`)
- Decisions: `docs/adr/` (0001 branching strategy, 0002 execution owns the work progress)

## REST API

| Route | Roles | Result |
|---|---|---|
| `GET /v1/execution/queue` | ATTENDANT, MECHANIC, ADMIN | 200 `{"items":[...]}` with QUEUED and IN_PROGRESS tasks, oldest first |
| `PUT /v1/service-order/:id/work/:workId/next` | MECHANIC, ADMIN | 204; 404 without task or work; 409 when not active or the work cannot move |
| `PUT /v1/service-order/:id/work/:workId/cancel` | MECHANIC, ADMIN | 204; allowed from AWAITING_START or IN_PROGRESS |
| `PUT /v1/service-order/:id/finish` | MECHANIC, ADMIN | 204; 409 while a work is not COMPLETED or CANCELLED |

Work status: `AWAITING_START → IN_PROGRESS → COMPLETED`; `CANCELLED` is terminal and reachable from the first two.
Task status: `QUEUED → IN_PROGRESS → COMPLETED`, or `CANCELLED` (cancelled by the saga, or when every work was
cancelled at finish).

**Behaviour change:** the monolith did not require the works to be finished before closing the order. The
execution service does (`finish` returns 409 `ErrWorksNotFinished`).

## Messaging

| Direction | Message | Topic / queue |
|---|---|---|
| consumes | `EnqueueExecution`, `CancelExecution` | `saga-commands` (filter `target=execution`) → `execution-queue` |
| publishes | `ExecutionQueued`, `ExecutionWorkStatusChanged`, `ExecutionCompleted`, `ExecutionFailed` | `execution-events` |

- Commands are deduplicated by `message_id` in `processed_message`; events go through the `outbox` table and the
  relay, in the same transaction as the state change.
- Every event of a task carries the `saga_id` received in `EnqueueExecution`, including the REST-driven ones.
- `CancelExecution` cancels the non-terminal works and the task, and publishes the work changes only.
- `ExecutionFailed.reason`: `INVALID_REQUEST` (invalid command, or a command for a task already completed or
  cancelled, where `task_id` is the existing task) and `ALL_WORKS_CANCELLED` (finish with every work cancelled).

## Layout

```
cmd/service                    entrypoint
internal/work                  work status state machine (NextStatus, Cancel)
internal/task                  task aggregate, command handlers, REST operations, controller, messages
internal/task/repository       PostgreSQL and in-memory repositories
internal/app                   HTTP server, bootstrap (API + relay + consumer), routing, middlewares
internal/pkg                   shared packages: auth (JWT, roles), messaging (outbox, relay, consumer), web, ...
migrations                     golang-migrate schema (execution_task, execution_work, history, outbox, dedupe)
testdata/contracts             message fixtures (spec: auto-repair-shop/docs/fase4/contracts/messaging.md)
```

## Run

```bash
cp .env.example .env
make up            # docker compose: app, Postgres, migrations, MiniStack, queues
make test          # unit tests with the race detector
make coverage      # coverage profile
make test-integration   # needs PostgreSQL and MiniStack (see docker-compose.yml)
```

## Configuration

`PORT`, `LOG_LEVEL`, `JWT_SECRET`, `POSTGRES_HOST|PORT|USER|PASSWORD|DB|SSLMODE`, `AWS_REGION`,
`AWS_ENDPOINT_URL` (local only), `EXECUTION_QUEUE_URL`, `EXECUTION_EVENTS_TOPIC_ARN`. Without the two queue/topic
variables the HTTP API still starts, and the relay and consumer are disabled.

## Quality gates

- Coverage: total ≥ 80 % and ≥ 90 % for `internal/task` and `internal/work`. CI job `coverage`.
- Measured on 2026-11-01 with `LOG_LEVEL=PANIC go test ./... --race -count=1` (the repo has no generated mocks):

| Package | Coverage |
|---|---|
| **total** | **90.8 %** |
| `internal/task` | 93.7 % |
| `internal/task/repository` | 99.2 % |
| `internal/work` | 100 % |
| `internal/app` | 36.4 % |
| `internal/app/bootstrap` | 17.6 % |
| `internal/app/routing` | 84.0 % |
| `internal/app/middleware` | 100 % |
| `internal/pkg/messaging` | 97.2 % |
| `internal/pkg/logger` | 94.3 % |
| `internal/pkg/web` | 97.3 % |
| other `internal/pkg/*` | 100 % |

- Integration tests (`-tags=integration`) run in CI against PostgreSQL and MiniStack; they are not in the table.
