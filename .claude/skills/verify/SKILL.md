---
name: verify
description: Run this repo's checks before a commit, push or PR (there is no CI, so this is the only gate) — Go build, vet, tests and gofmt on changed files; frontend typecheck, tests, lint and build. Use after any code change, when asked to verify, test or check, and always before committing.
---

# Verify

The repo has no CI. Run every step that applies to what changed, from the repo root, and report each result.

## 1. What changed

```bash
git status --short
git diff --name-only origin/main...HEAD; git diff --name-only HEAD
```

Backend steps when anything under `backend/` changed; frontend steps when anything under `frontend/` changed. Both when unsure.

## 2. Backend (Go)

```bash
cd backend
go build ./...
go vet ./...
go test ./...
```

gofmt only the Go files you changed (a few older files are not gofmt-clean: `internal/api/h_analytics.go`, `internal/api/shared.go`, `internal/sso/oauth.go`; don't reformat them unless you changed them):

```bash
gofmt -l <changed .go files>   # must print nothing; fix with gofmt -w <file>
```

Tests that need a database skip without `TEST_DATABASE_URL` (PostgreSQL with TimescaleDB). When it is not set, say which tests were skipped; don't report them as passed.

A new or changed migration in `backend/internal/store/migrations/`: when a PostgreSQL server is available, run its Up, a quick insert/select, then its Down against a throwaway database. Without one, say the migration was not run.

## 3. Frontend

```bash
cd frontend
[ -d node_modules ] || npm ci --no-audit --no-fund
npm run typecheck
npm test
npm run lint
npm run build
```

## 4. Report

List each step as passed, failed (with the error output) or not run (and why). Don't commit or push when a step failed, and don't call the work verified while a step that applies was not run. The app itself (UI flows) is not covered by these checks; say so when the change is user-facing.
