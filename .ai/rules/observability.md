# Observability

- Logger: Zap (`internal/pkg/logger`). Levels: `INFO` for business decisions (task created, duplicate ignored,
  cancel ignored), `WARN` for rejected commands (invalid works), `ERROR` for infrastructure failures.
- Prefer explicit fields: `zap.String("service_order_id", id)`, `zap.String("type", env.Type)`.
- Metrics: `http_request_duration_seconds` (labels `method`, `route`, `status`) at `/metrics`.
- Health: `/ping`.
- The logger redacts sensitive keys (password, token, dsn).
