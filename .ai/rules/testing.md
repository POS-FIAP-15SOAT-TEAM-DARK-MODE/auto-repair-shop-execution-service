# Testing

## Coverage thresholds
- Total: 80 % minimum (CI gate `coverage`; the gate drops `/mocks/` paths, but the repo has no generated mocks).
- `internal/task` and `internal/work`: 90 % minimum (CI gate `coverage`).
- Measured on 2026-11-01 (`LOG_LEVEL=PANIC go test ./... --race -count=1`): total 90.8 %; `internal/task` 93.7 %,
  `internal/work` 100 %. See the README for the per-package table.

## Commands
```bash
make test                                   # all unit tests with the race detector (LOG_LEVEL=PANIC)
make coverage                               # coverage.out and the total (no HTML report)
go test ./internal/task/... -run TestEnqueue -v
go vet -tags=integration ./...              # integration tests must compile
make test-integration                       # needs PostgreSQL and MiniStack (CI)
```

## Unit tests
- Domain: table tests for every transition, including invalid ones (`internal/work`, `internal/task`).
- Services: `sqlmock` for the outbox and transactions, the in-memory repository for the aggregate state.
  Outbox expectations match the envelope type, `saga_id` and payload fields.
- Repositories: `sqlmock` for the Postgres implementation, including each error branch.
- Contracts: `testdata/contracts/<Type>.json` is decoded by the consumer side and rebuilt by the publisher side.
- REST: route tests cover 401 (no token), 403 (wrong role), 404 and the success path with real JWTs.

## Integration tests (`-tags=integration`)
- `internal/integration`: Postgres flows (dedupe, invalid command, REST flow keeps the saga, cancel) run without MiniStack.
- The SNS/SQS test runs only with `AWS_ENDPOINT_URL` set (CI).

## Rules
- Arrange / Act / Assert; independent tests; sub-tests with `t.Run`.
- Name: `Test<Unit>_<Condition>_<Outcome>` for new tests or table cases with clear names. Existing names are mixed
  (`TestEnqueue`, `TestCancelWork_Errors`); follow the neighbouring file.
