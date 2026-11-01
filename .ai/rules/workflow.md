# Workflow

## Branches and PRs
- Feature branches from `develop`: `feat/<topic>`. PRs target `develop`.
- Commit messages: Conventional Commits; one theme per commit.
- CI jobs: `pre-commit`, `build`, `unit-tests`, `integration-tests`, `coverage`, `sonarqube` (needs `SONAR_TOKEN`).

## Scopes
`messaging` · `work` · `task` · `api` · `db` · `k8s` · `infra` · `docs` · `ci` · `bootstrap`

## Local commands
```bash
make run               # start the server (PORT from .env, default 8084)
make test              # unit tests with the race detector
make coverage          # coverage profile
make up / make down    # docker compose: app, PostgreSQL, migrations, MiniStack
make migrate-up        # apply pending migrations
```
