# AI Rules — Master Index

Single source of truth for all AI agent rules in this project.
**Never duplicate content** in agent pointer files — those files only contain a routing table and 3 always-apply rules.

## Folder Map

```
.ai/
  rules/               # topic-scoped rule files — open on demand
    architecture.md        packages, transactions, domain purity, messaging flow
    domain.md              ubiquitous language, task and work lifecycles, finish rule, saga correlation
    go-conventions.md      Go idioms, db.Executor, errors, IDs, sqlmock
    database.md            PostgreSQL conventions, constraints, migrations
    api.md                 REST routes with roles, error format
    testing.md             coverage thresholds, unit/integration patterns
    security.md            JWT, roles, queue policy, secrets
    observability.md       Zap, log levels, metrics, redaction
    workflow.md            Conventional Commits, CI jobs, dev commands
  commands/            # reusable playbooks (also Claude Code slash commands via symlinks)
    add-migration.md       create and apply a versioned migration
  hooks/               # pre-commit hook scripts
    check-ai-rules.sh      enforces .ai/ as the only rule source
```

## Topic Index

| Topic | File | Key contents |
|---|---|---|
| Architecture | `rules/architecture.md` | Packages, transactions, domain purity |
| Domain & Business Rules | `rules/domain.md` | Task and work lifecycles, finish rule, saga correlation |
| Go Conventions | `rules/go-conventions.md` | Tech stack, db.Executor, errors, IDs |
| Database | `rules/database.md` | Constraints, indexes, migrations |
| API | `rules/api.md` | Endpoints with roles, error format |
| Testing | `rules/testing.md` | 90%/80% coverage, unit vs integration patterns |
| Security | `rules/security.md` | JWT, roles, queue policy, secrets |
| Observability | `rules/observability.md` | Zap, log levels, metrics, redaction |
| Workflow | `rules/workflow.md` | Conventional Commits, CI jobs, dev commands |

## Task Routing — Required Reading by Task Type

| Task type | Files to read |
|---|---|
| New feature or domain change | `architecture.md` · `domain.md` · `go-conventions.md` |
| Database schema or migration | `database.md` · `go-conventions.md` |
| API endpoint | `api.md` · `architecture.md` · `security.md` |
| Auth, roles, or security | `security.md` · `domain.md` |
| Tests | `testing.md` · `architecture.md` |
| Logging or observability | `observability.md` · `security.md` |
| Git workflow | `workflow.md` |
| Add migration (command) | `commands/add-migration.md` |
