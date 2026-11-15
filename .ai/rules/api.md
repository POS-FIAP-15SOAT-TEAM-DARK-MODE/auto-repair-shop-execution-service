# API

All routes are under `/v1/`. Swagger source: `docs/swagger.yaml`.

| Route | Roles | Success | Errors |
|---|---|---|---|
| `GET /v1/execution/queue` | ATTENDANT, MECHANIC, ADMIN | 200 `{"items":[{taskId,serviceOrderId,status,createdAt,works:[{workId,name,status}]}]}` | 401, 403 |
| `PUT /v1/service-order/:id/work/:workId/next` | MECHANIC, ADMIN | 204 | 401, 403, 404 (no task / work not in task), 409 |
| `PUT /v1/service-order/:id/work/:workId/cancel` | MECHANIC, ADMIN | 204 | 401, 403, 404, 409 |
| `PUT /v1/service-order/:id/finish` | MECHANIC, ADMIN | 204 | 401, 403, 404, 409 (works not finished, or task not active) |

- `X-Request-Id` is propagated to the events raised by the request; generated when absent.
- Errors use `{"code": <status>, "error": "...", "errors": [...]}` (see `internal/pkg/web/errors.go`). `error` and
  `errors` are omitted when empty. `errors` lists the validation messages (400); the REST routes have no body, so
  they do not answer 400 today.
- 404 and 409 answer the message of the domain sentinel. Infrastructure 409/422 get generic messages (`conflict`,
  `invalid data`). 5xx answers `{"code":500,"error":"internal server error"}`; the cause is only logged.
- Also registered outside `/v1/` (no token): `GET /ping` and `GET /metrics`. Swagger UI: `/swagger/index.html`.
- Routes are protected by `middleware.Auth(roles...)`; the role groups are in `internal/pkg/auth/roles.go`.
