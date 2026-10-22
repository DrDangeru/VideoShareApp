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

Run the security-hardening regressions:

```powershell
go test ./internal/app -run 'Test(ChannelAvatar|CommentAuthorPrivacy|CommentLengthIsLimited|HTTPServerConfiguration|LoginLimiter|LoginAndRegistrationRateLimit|NewEnablesForeignKeys|ParseRequestForm|POSTRoutesEnforce|RegistrationPassword|SessionLifetime|UnsupportedFormBody|LockedChannelPage)' -count=1 -v
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

## Mutation check

`scripts/mutation-check.sh` verifies the Go suite actually catches
security-relevant regressions. It copies the working tree (source only, never
the real database, media, or uploads) to a temporary directory, seeds one small
bug at a time — a disabled limit, a flipped ownership check, a removed
visibility clause — and expects the suite to fail for every mutant. Any
`SURVIVED` line names a coverage gap.

```bash
bash scripts/mutation-check.sh            # all mutants (run via bash; invoking
                                          # the .sh from PowerShell directly
                                          # does not execute it)
scripts/mutation-check.sh 'session'       # subset by description
WORKERS=4 scripts/mutation-check.sh       # override worker count (default 6)
```

The script first requires a clean, unmodified build and test baseline. Each
of the six workers uses its own isolated source copy, with only one mutation
active at a time, so concurrent mutants cannot contaminate each other's results.
It prints timestamped START/result lines with worker and mutant numbers,
source files, failing test names, and elapsed time, plus a "still running,
please wait" ticker every 30 seconds. Full test output for every mutant,
`baseline.log`, and a replayable `console.log` are saved under
`test-results/mutation-check/<timestamp>-<unique suffix>/`. Build failures,
no-op mutations, runner errors, and missing results fail the run rather than
counting as successful kills.

The script is a curated list, not exhaustive mutation testing; add an entry
when a new security-relevant check lands. Run it after significant test or
security changes.

## Full mutation testing (user-run, on demand)

`cmd/mutator` performs complete AST-based mutation testing: it generates a
mutant at every mutable site in `internal/app` production files (swapped
comparisons, flipped boolean operators, removed negations, bumped integer
literals), applies each to a throwaway copy of the working tree, and expects
the Go suite to fail. Survivors name potential coverage gaps; constant and
sub-nanosecond boundary mutants are expected noise.

Because a full run executes the test suite hundreds of times (~591 sites,
roughly 15-30 minutes), this tool is **run only by the user, on demand and/or
before a deploy**. It is never part of the pre-commit hook, routine
verification, or agent test loops.

```powershell
go run ./cmd/mutator -dry-run        # list mutation sites only
go run ./cmd/mutator -only handlers  # restrict to matching files
go run ./cmd/mutator                 # full run (long)
```

The fast curated check in `scripts/mutation-check.sh` remains the routine
option; the full mutator is the deep pre-deploy option.

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
  uploads, discovery and direct locked-channel privacy, shared media
  references, comment ownership/privacy/length, avatar validation, request
  limits, session expiry, password rules, rate limits, and HTTP configuration
  using in-memory databases and temporary working directories.
- The pooled foreign-key regression invokes `New()` only after changing into
  a fresh temporary directory; it checks three simultaneous SQLite pool
  connections and never opens the repository's `data/app.db`.
- Thumbnail unit and handler tests cover strict JPEG/PNG validation and
  normalization; E2E preview tests simulate decoded video metadata and mock
  canvas output, without decoding real video or capturing real frames.
- Runner shutdown/cancellation unit tests can run independently with
  `node --test e2e/server-process.test.mjs`; they do not start the fixture
  server.
- Media security tests cover unregistered URLs, database failures, and
  registered synthetic fixtures. No tests use production DB/media or perform
  real media processing.
