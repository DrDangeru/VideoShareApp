# Testing

The regression suite lives in `internal/app` (`package app`) and uses only the
standard library (`testing`, `net/http/httptest`) plus `bcrypt`. Tests run
against in-memory SQLite databases and temporary working directories; they
never touch the real `data/app.db`, `Videos/`, `static/uploads/`, or any other
real user content (temporary directories may recreate those relative paths with
fixture files). The default Go suite does not start a server, browser, or
ffmpeg. All Go HTTP checks go through the app's mux with `httptest` recorders.
The `playwright` build-tag test server is started only by the disposable E2E
runner described in `e2e/README.md`.

Run all commands from the repo root in PowerShell.

## Run the suite

```powershell
go test ./...
```

Verbose output:

```powershell
go test -v ./...
```

Skip cached results (always re-run):

```powershell
go test -count=1 ./...
```

## Run a single test

```powershell
go test ./internal/app -run '^TestLogin$' -v
```

Use `-run` with any test name, e.g. `'^TestLoadFeed$'` or a subtest pattern like
`'^TestWatchPermissions/channel'` (matches the channel-locked watch subtests).

Run seed, upload, and security regression tests:

```powershell
go test ./internal/app -run 'Test(Security|Seed|CreateChannel|UploadNewChannel)' -count=1 -v
```

Run discovery, comment ownership, shared-media, and thumbnail regressions:

```powershell
go test ./internal/app -run 'Test(DiscoveryChannelLocks|CommentDeleteOwnership|SecuritySharedMediaURLs|DecodeThumbnail|Upload.*Thumbnail|EditThumbnail)' -count=1 -v
```

For browser E2E prerequisites, fixture scope, and safety settings, see
[Playwright E2E tests](e2e/README.md).

## Pre-commit hook

`hooks/pre-commit` runs gofmt, `go vet`, `go build`, and the full Go test
suite on every commit. Git hooks are not cloned automatically, so enable it
once per clone (so setup if cloning the repo on a new machine via the 1 liner below):

```powershell
git config core.hooksPath hooks
```

Then every `git commit` runs the checks and fails on any problem. To skip the
checks for a single commit, use `git commit --no-verify`. On Linux/macOS, make
the script executable once (`chmod +x hooks/pre-commit`); on Windows, Git Bash
runs it as-is.

## Coverage

Statement coverage summary:

```powershell
go test -cover ./...
```

Coverage profile for `internal/app`, then open the HTML report:

```powershell
go test -coverprofile=coverage.out ./internal/app
go tool cover -html=coverage.out
```

## Scope and limits

- `internal/app` is the only package with tests today. `main.go` and
  `cmd/thumbgen` have no test coverage yet.
- Tests cover seed behavior, fresh disposable startup, synthetic multipart
  uploads, discovery locks, shared media references, and comment ownership
  using in-memory databases and temporary working directories.
- Thumbnail unit and handler tests cover strict JPEG/PNG validation and
  normalization; E2E preview tests simulate decoded video metadata and mock
  canvas output, without decoding real video or capturing real frames.
- Runner shutdown/cancellation unit tests can run independently with
  `node --test e2e/server-process.test.mjs`; they do not start the fixture
  server.
- Media security tests cover unregistered URLs, database failures, and
  registered synthetic fixtures. No tests use production DB/media or perform
  real media processing.
