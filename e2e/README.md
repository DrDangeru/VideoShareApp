# Playwright E2E tests

The E2E suite uses `@playwright/test` with Chromium. The Go fixture server is
built offline into a fresh temporary directory, uses in-memory SQLite plus
temporary synthetic templates/assets/media, and binds only to
`127.0.0.1:18765`. It never reuses an existing server or accesses production
data. The runner removes its temporary directory on graceful exit. Forced
termination on Windows can leave a `videoshare-e2e-*` directory in the OS temp
folder, although the server process is stopped.

Prerequisites are Node.js 20 or later, Go 1.26 or later with module dependencies
already cached, and Chromium from the pinned Playwright version. The fixture
runner sets `GOPROXY=off`, `GOTOOLCHAIN=local`, and `GOSUMDB=off`.
If Go modules are not cached in a new environment, run `go mod download` only
after registry access is explicitly approved; this E2E runner never downloads
Go modules. The fixture test binary also has a 10-minute hard timeout.

The development dependency is pinned to `@playwright/test` 1.64.1. Matching
Chromium v1248 is installed locally. Run the suite with:

```powershell
npm run test:e2e
```

To provision the locked package in another checkout, use:

```powershell
npm ci
```

Do not run dependency installs or browser downloads until required registry or
CDN access has been approved.

If the matching browser binary is missing, install Chromium with:

```powershell
npx playwright install chromium
```

Browser downloads require explicit network approval.

Opening the HTML report manually:

```powershell
npm run test:e2e:report
```

The browser cases cover dashboard multipart uploads and simulated thumbnail
preview state, public/private/locked watch and media access,
owner/other-user/admin authorization, comments, avatars, and registration. Go
regression tests separately cover startup, discovery channel locks, comment
ownership, shared media URLs, thumbnail validation, database failures, and
path aliases.

Run the runner lifecycle unit tests without starting the fixture server:

```powershell
node --test e2e/server-process.test.mjs
```

Automatic screenshots, video recordings, and traces remain disabled, and this
suite does not capture screenshots, video frames, or screen pixels. A future
manual Playwright screenshot requires explicit per-screenshot approval
immediately beforehand under the general rules in the global `AGENTS.md`;
approval to install packages or run tests is not screenshot approval. Browser
requests to non-localhost hosts are aborted. Fixture credentials and media
bytes are synthetic test data only. The preview test supplies mocked video
metadata and canvas methods with a fixed synthetic JPEG data URL; it never
reads video frames or browser pixels.
