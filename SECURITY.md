# Security notes

Startup seeds a default administrator intended for local development and testing. Set
`VIDEO_SHARE_ADMIN_PASSWORD` before the first launch to choose a different
password; once the account exists, seeding never modifies, deletes, or resets
it or any other account or session.

Before exposing a deployment, rotate or delete the default administrator and
revoke its active sessions using a secured operational process. This project
does not provide a credential-rotation flow.

Media and uploaded files are served only when their exact URL is registered in
the database. When a URL is referenced by multiple videos or channels, access
is allowed only if every matching record permits it; a private or locked
reference denies access unless the requester owns that restricted record or is
an administrator. Database lookup failures deny access.

Thumbnails accept JPEG or PNG data URLs only, with decoded/normalized data
limited to 2 MiB and dimensions limited to 4096 by 4096 and 16 million pixels.
Accepted images are fully decoded and normalized to JPEG before storage. New
uploads and thumbnail updates reject invalid thumbnail data before creating a
channel/video or changing video metadata.

Channel avatars use the same validation and normalization rules. An edit without
a new avatar preserves the current one; removing an avatar clears its database
URL but does not delete the file. Comment authors are displayed as `Member N`,
not by email address.

Non-administrative discovery excludes admin-locked channels, including their
videos, even for the channel owner. Direct channel pages return 404 for every
non-administrator when the channel is locked; owners can still manage their
channel from the edit page and dashboard. Non-administrators cannot subscribe
to or toggle notifications for locked channels. Comment creation follows watch
visibility, is limited to 4000 Unicode characters, and deletion is limited to
the comment author, channel owner, or an administrator.

Avatar files are available publicly for unlocked channels and restricted to the
channel owner or administrators for locked channels.

Registration accepts valid UTF-8 passwords with at least 8 Unicode characters,
at most 72 bytes, and at least one non-whitespace character. Login has no
minimum length so existing short-password accounts remain usable; passwords
over 72 bytes are rejected as invalid credentials.

Login and registration share an in-process limit of 10 credential attempts
per 5-minute window per normalized remote IP. Source ports are ignored and
forwarded-IP headers are not trusted. The limiter retains at most 4096 active
IP buckets, resets on process restart, and is not distributed; clients behind
the same NAT share a budget. IP addresses are kept only in process memory for
the lifetime of a window and are never written to the database or logs.

Sessions are valid for at most 24 hours from `sessions.created_at`. Invalid,
malformed, or future timestamps fail closed. Expired session rows are purged
when a new session is created; there is no background cleanup or global
session reset.

POST bodies are limited to 1 MiB for ordinary forms, 10 MiB for video/channel
edits, and 512 MiB for uploads including multipart overhead. Multipart parsing
uses an 8 MiB in-memory threshold for file parts before spooling them to
temporary files. Text fields and metadata have a separate parser budget; the
total body limits still apply, and handlers remove temporary files after parsing.

SQLite foreign-key enforcement is configured in the connection DSN so every
pooled connection enables it. The HTTP server binds to `127.0.0.1:8080` by
default. Set `VIDEO_SHARE_ADDR` explicitly to bind elsewhere; for example,
`:8080` listens on all interfaces. Header/read/idle timeouts are configured,
while the write timeout remains disabled so streamed media is not cut off.
