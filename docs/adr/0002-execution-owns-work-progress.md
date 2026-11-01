# ADR 0002 — Execution owns the work progress of a service order

- Status: Accepted
- Date: 2026-11-01

## Context

In the monolith the service order held the work status (`work_service_order_history`) and decided when the
order could move on. The saga of the service order now coordinates the steps through SNS/SQS, and the
execution (mechanic) work must not depend on the service order database.

## Decision

- The execution service owns the task of each service order (`execution_task`) and the status of each work
  (`execution_work`, history in `execution_work_history`), in its own PostgreSQL database `execution`.
- The saga starts the work with `EnqueueExecution`. The execution service answers with `ExecutionQueued`,
  `ExecutionCompleted` or `ExecutionFailed`, and publishes `ExecutionWorkStatusChanged` for every work change.
- The task keeps the `saga_id` of `EnqueueExecution` and uses it on every later event, including the ones
  raised by REST calls (`next`, `cancel`, `finish`), so the saga can correlate them.
- `finish` requires every work to be COMPLETED or CANCELLED. This is a new rule: the monolith allowed the
  order to move on with works still pending.
- An invalid `EnqueueExecution` (no works, empty or repeated `work_id`) creates no task and is answered with
  `ExecutionFailed{reason: INVALID_REQUEST}`.

## Consequences

- Service order and History only see events; they never read execution tables.
- Each change, its history row and its outbox event are written in one transaction, and the consumer dedupes by
  `message_id`, so a redelivered command has no extra effect.
- Work-level history (with start time per work) is consumed by History to build the work report.
