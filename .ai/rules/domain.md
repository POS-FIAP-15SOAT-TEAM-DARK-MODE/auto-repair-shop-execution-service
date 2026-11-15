# Domain

## Ubiquitous language

| Name | Meaning |
|---|---|
| Task (`execution_task`) | The mechanic work of one service order. One per service order (`UNIQUE service_order_id`). Carries the `saga_id` of its `EnqueueExecution`. |
| Work (`execution_work`) | One catalog work inside the task. `work_id` is the catalog id; `id` is the execution row id. |
| Work history (`execution_work_history`) | One row per work status change. `previous_status` is NULL on creation. |

## Task status
```
QUEUED ──(first work IN_PROGRESS)──▶ IN_PROGRESS ──(finish, not all cancelled)──▶ COMPLETED
   │                                       │
   └──────────(CancelExecution / finish with all works cancelled)──────────▶ CANCELLED
```
Active = `QUEUED` or `IN_PROGRESS`. Only active tasks accept `next`, `cancel` and `finish`.

## Work status
```
AWAITING_START ──next──▶ IN_PROGRESS ──next──▶ COMPLETED
      │                       │
      └─────── cancel ────────┴──────────▶ CANCELLED
```
`COMPLETED` and `CANCELLED` are terminal. `next` on a terminal work → `ErrWorkServiceOrderAlreadyCompleted` /
`ErrWorkServiceOrderAlreadyCancelled`. `cancel` on `COMPLETED` / `CANCELLED` → the same errors.

## Commands and their rules
- `EnqueueExecution`: payload `works[]` with unique non-empty `work_id`. Invalid → `ExecutionFailed(INVALID_REQUEST, task_id "")`,
  no task. Active task for the service order → nothing (deduped). Terminal task → `ExecutionFailed(INVALID_REQUEST, task_id = existing)`.
- `CancelExecution`: cancels every non-terminal work and the task. No task or terminal task → nothing.

## Finish rule
Every work must be COMPLETED or CANCELLED, otherwise `ErrWorksNotFinished` (409). All CANCELLED → task CANCELLED and
`ExecutionFailed(ALL_WORKS_CANCELLED)`. Otherwise task COMPLETED (+`completed_at`) and `ExecutionCompleted`.
This rule is new compared with the monolith (see ADR 0002).

## Saga correlation
The task keeps the `saga_id` of `EnqueueExecution`. Every event about the task uses it, including REST-driven events.
The failure reply to an invalid or duplicate command uses that command's `saga_id`.
