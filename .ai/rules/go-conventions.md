# Go conventions

Go 1.27 (`go.mod`; CI `GO_VERSION`) · Gin · PostgreSQL 15 · `lib/pq` (raw SQL, no ORM) · `golang-migrate` · `golang-jwt/jwt/v5` · Zap · Testify · `google/uuid` · `oklog/ulid/v2` · `aws-sdk-go-v2` (SNS, SQS).

- Repositories take `db.Executor` (implemented by `*sql.DB` and `*sql.Tx`).
- Mocks are not generated: the service tests use `sqlmock` and the in-memory repository.
- Errors: sentinel values with `errors.New`, wrapped with `%w`; mapping to HTTP is in `internal/pkg/web/errors.go`.
- Configuration: `internal/pkg/env` (`GetString`); `Makefile` loads `.env` and exports it.
- IDs: `internal/pkg/id` (`NewUUID` for tasks and works, `NewULID` for history rows).
