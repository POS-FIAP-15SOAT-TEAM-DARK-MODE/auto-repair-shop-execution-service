# Database

PostgreSQL 15, database `execution`, role `execution_app`. Schema in `migrations/000001_init_schema.*`
(DDL in `docs/fase4/contracts/ddl.md`, section execution).

## Conventions
- Ids: `VARCHAR(36)` UUID for tasks and works; `VARCHAR(26)` ULID for `service_order_id` and history rows.
- Timestamps: `TIMESTAMP WITH TIME ZONE`, UTC.
- Status columns: `VARCHAR` with a `CHECK` on the valid values.
- No foreign key to other databases; `work_id` and `service_order_id` are only references.

## Required constraints
- `execution_task.service_order_id` UNIQUE (one task per order).
- `execution_work (task_id, work_id)` UNIQUE.
- Indexes: `execution_task (status, created_at)` for the queue; `execution_work_history (task_id, work_id)`.
- `processed_message` (dedupe) and `outbox` (`published_at IS NULL` partial index) exist in this database.

## Migrations
Versioned files only: `migrations/NNNNNN_<name>.up.sql` and `.down.sql`. Never edit an applied migration.
`make migrate-up` / `make migrate-down`.
