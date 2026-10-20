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

Non-administrative discovery excludes admin-locked channels, including their
videos, even for the channel owner. Comment creation follows watch visibility;
deletion is limited to the comment author, channel owner, or an administrator.

Avatar files are available publicly for unlocked channels and restricted to the
channel owner or administrators for locked channels.
