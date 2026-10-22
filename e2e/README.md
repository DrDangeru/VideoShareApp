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
Go modules. `modernc.org/sqlite` v1.60.1 is paired with `modernc.org/libc`
v1.77.1; keep that exact module pairing when updating dependencies. The fixture
test binary also has a 10-minute hard timeout.

The normal application server defaults to `127.0.0.1:8080`; `VIDEO_SHARE_ADDR`
can explicitly select another bind address. Its header/read/idle timeouts are
configured, with writes left unbounded for media streaming. The disposable
fixture remains restricted to `127.0.0.1:18765`. Application sessions expire
after 24 hours, and login/registration share a 10-attempt-per-5-minute
in-process IP limit; the fixture server bypasses that limiter because its
tests perform many loopback logins within seconds. Ordinary/edit/upload POST
bodies are capped at 1/10/512 MiB, respectively.

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
owner/other-user/admin authorization, comment privacy and ownership, avatar
selection/save/reload/removal, and registration. The avatar browser case uses a
synthetic JPEG fixture. Go regression tests separately cover pooled
foreign-key enforcement, request limits, session expiry, password and
rate-limit policies, server configuration, startup, discovery channel locks,
comment ownership/privacy, shared media URLs, thumbnail validation, database
failures, and path aliases.

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
