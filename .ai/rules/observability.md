# Observability

## Logs
- Logger: Zap, JSON (`internal/pkg/logger`). Levels: `INFO` for business decisions (execution queued, duplicate ignored,
  cancel ignored), `WARN` for rejected commands (invalid works), `ERROR` for infrastructure failures.
- Every line has `service` = `execution-service`. The request or message context adds the correlation fields:
  `request_id` (always), `service_order_id`, `saga_id` and `message_id` when known.
  - HTTP: the Logger middleware takes `X-Request-Id` or generates one, and writes it back to the request header, so the
    envelopes published by the handler carry the same `request_id`. The route param `id` becomes `service_order_id`.
  - Messages: `Consumer.Handle` adds `message_id`, `saga_id`, `service_order_id` and `request_id` from the envelope.
  - Add fields with `logger.With(ctx, zap.String(...))`; do not build loggers by hand.
- Event names are `<entity>.<action>` (`execution.enqueue_ignored`, `message.duplicate_ignored`, `outbox.enqueued`,
  `request.completed`). The contract types (`ExecutionQueued`, ...) are the envelope `type`, not log messages.
- Prefer explicit fields: `zap.String("service_order_id", id)`, `zap.String("type", env.Type)`.
- Never log payloads, JWTs, `Authorization`, CPF or CNPJ. The logger redacts keys password, token, jwt, authorization,
  cpf, cnpj and documento, and the password of a DSN.

## Metrics (`/metrics`)
- `http_request_duration_seconds` (labels `method`, `route`, `status`).
- `outbox_pending` (gauge): outbox rows with `published_at IS NULL`, read on each scrape. It is NaN when the query fails.
- Not applicable here: `saga_started_total`, `saga_completed_total`, `saga_compensated_total`, `saga_step_duration_seconds`
  belong to the service-order service (the orchestrator).
- Not implemented: `dlq_messages`. It needs `EXECUTION_DLQ_URL`, a poller and `sqs:GetQueueAttributes` on the DLQ,
  which belong to infra-k8s.

## Scrape and health
- `ServiceMonitor`: `k8s/manifests/components/observability` (overlays lab, stg and prd; not local).
- Health: `/ping`.
