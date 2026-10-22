# Project notes for agents

## Verification policy

- Do NOT run the full Playwright E2E suite on every change. Run it only when
  the change touches UI behavior: templates, `static/` CSS/JS, E2E specs, the
  fixture server (`test_playwright_server_test.go`, `e2e/`), or anything that
  alters rendered pages or browser-visible behavior.
- For backend-only changes (Go handlers, DB, auth, limits), verify with the Go
  suite and focused tests instead:
  - `go test -count=1 ./...`
  - `go vet ./...`
  - `go build ./...`
- Focused Go runs: see `TESTING.md` for `-run` patterns.
- Screenshots, video, traces, and any screen/frame capture always require
  explicit per-capture user approval (see global rules); the Playwright config
  keeps them off by default.
- Do NOT run `go run ./cmd/mutator` (full mutation testing) as part of routine
  verification: a full run takes ~15-30 minutes and is reserved for the user,
  on demand and/or before deploy. The fast curated check is
  `scripts/mutation-check.sh` (~2 minutes).

## Commands

- Go tests: `go test -count=1 ./...` (full suite is ~3s)
- E2E (UI changes only): `npm run test:e2e`
- E2E runner lifecycle tests only: `node --test e2e/server-process.test.mjs`
- Pre-commit hook runs gofmt/vet/build/tests on every commit; enable per clone
  with `git config core.hooksPath hooks` (see `TESTING.md`).

## Environment notes

- Go modules must be updated as a paired set: `modernc.org/sqlite` v1.60.1
  pins `modernc.org/libc` v1.77.1 exactly; do not bump one without the other.
- E2E fixture runs are offline (`GOPROXY=off`, `GOTOOLCHAIN=local`,
  `GOSUMDB=off`) and require a warm Go module cache.
- The disposable fixture server bypasses the login rate limiter
  (`VIDEO_SHARE_E2E=1`); production keeps the 10-attempts-per-5-minute IP
  limit.
- Default HTTP bind is `127.0.0.1:8080`; override with `VIDEO_SHARE_ADDR`.
- The development admin account `admin@admin.com` is seeded intentionally for
  local testing; password defaults to `admin12345`, overridable via
  `VIDEO_SHARE_ADMIN_PASSWORD` before first creation.
