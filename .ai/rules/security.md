# Security

- Authentication: JWT HS256 (`JWT_SECRET`). Claims: `user_id`, `roles`. The middleware validates the token and checks roles;
  it makes no database call.
- Authorization: the route's roles, from `internal/pkg/auth/roles.go` (`MechanicRoles`, `AttendantAndMechanicRoles`, ...).
- Internal messages: the queue is only writable by the `saga-commands` topic (queue policy in the infra scripts).
- Logs and errors never contain JWT tokens, passwords, or the body of a command beyond the ids needed for correlation.
- Validate payloads in the command handler: invalid works produce `ExecutionFailed(INVALID_REQUEST)`; they never create a task.
- Secrets come from Secrets Manager (`auto-repair-shop-<env>/execution`); never write them to files or the repository.
