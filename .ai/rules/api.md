# API

All routes are under `/v1/`. Swagger source: `docs/swagger.yaml`.

| Route | Roles | Success | Errors |
|---|---|---|---|
| `GET /v1/execution/queue` | ATTENDANT, MECHANIC, ADMIN | 200 `{"items":[{taskId,serviceOrderId,status,createdAt,works:[{workId,name,status}]}]}` | 401, 403 |
| `PUT /v1/service-order/:id/work/:workId/next` | MECHANIC, ADMIN | 204 | 401, 403, 404 (no task / work not in task), 409 |
| `PUT /v1/service-order/:id/work/:workId/cancel` | MECHANIC, ADMIN | 204 | 401, 403, 404, 409 |
| `PUT /v1/service-order/:id/finish` | MECHANIC, ADMIN | 204 | 401, 403, 404, 409 (works not finished, or task not active) |

- `X-Request-Id` is propagated to the events raised by the request; generated when absent.
- Errors use `{"code": <status>, "error": "...", "errors": [...]}` (see `internal/pkg/web/errors.go`).
- 5xx errors show the innermost message, as the monolith did.
- Routes are protected by `middleware.Auth(roles...)`; the role groups are in `internal/pkg/auth/roles.go`.
