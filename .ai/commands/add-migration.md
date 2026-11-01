# Playbook: Add Migration

## 1. Determine Next Version Number
```bash
ls migrations/ | sort | tail -5
```

## 2. Create Files
```bash
touch migrations/<NNNNNN>_<description>.up.sql
touch migrations/<NNNNNN>_<description>.down.sql
```

## 3. Write `up.sql`
Rules:
- `NUMERIC(10,2)` for monetary columns — never `FLOAT`.
- `VARCHAR(36)` UUID for task and work ids; `VARCHAR(26)` ULID for `service_order_id` and history rows.
- `TIMESTAMPTZ DEFAULT NOW()` for timestamps.
- Add `CHECK` constraints for status/enum columns (list all valid values).
- Add all required foreign key constraints.
- Add indexes on: status, FK columns, and any column used in filters.

## 4. Write `down.sql`
- Must fully reverse the `up.sql`.
- Drop in reverse order: indexes → FKs → columns → tables.

## 5. Apply and Verify
```bash
make migrate-up
migrate -path migrations -database "$DATABASE_URL" version
```

## Never
- Modify an existing migration file after it has been applied.
- Drop or alter existing columns (add new ones, leave old ones in place as deprecated).
- Change the database schema manually.
